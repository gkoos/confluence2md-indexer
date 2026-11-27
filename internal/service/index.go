package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/embedding"
	"github.com/gkoos/confluence2md-indexer/internal/indexer"
)

type IndexRequest struct {
	// Sources are the folders the index is built from. Documents are namespaced by
	// source id, and anything the index holds that no source lists any more is removed.
	Sources []indexer.Source
	DBPath  string
	Rebuild bool
	// Progress, when set, is called synchronously as the run advances. It must not
	// block for long, because indexing waits for it.
	Progress func(ProgressEvent)
	// Provider overrides provider resolution. Tests inject a deterministic
	// provider here; command paths leave it nil and resolve from Embedding.
	Provider embedding.Provider
	// Embedding configures provider resolution when Provider is nil.
	Embedding embedding.Options
}

// SourceResult is what one source contributed to an index run.
type SourceResult struct {
	ID           string
	Type         string
	Folder       string
	MetadataPath string
	PageCount    int
	CheckedFiles int
	// SkippedFiles counts files a files source left out: links, files over the size limit,
	// and files that are empty or not text.
	SkippedFiles int
}

type IndexResponse struct {
	Status              string
	Incremental         bool
	Rebuild             bool
	DBPath              string
	EmbeddingName       string
	EmbeddingSource     string
	EmbeddingWrites     int
	EmbeddingPruned     int
	EmbeddingDimension  int
	EmbeddingCapability string
	// Sources lists what each source contributed, in the order they were given.
	Sources []SourceResult
	// PageCount and CheckedFiles total the sources.
	PageCount    int
	CheckedFiles int
	Inserted     int
	Updated      int
	// MetadataUpdates counts pages whose text was unchanged but whose crawler
	// metadata moved; they are refreshed without re-embedding anything.
	MetadataUpdates int
	Skipped         int
	Deleted         int
	ChunkWrites     int
	RunID           string
	DBStats         *db.Stats
	// Corpus is the crawl this run recorded, carrying the run's own start time as
	// IndexedAt, so a caller can tell whether the run read a corpus that was still moving.
	Corpus *db.CorpusSnapshot
}

func Index(ctx context.Context, req IndexRequest) (*IndexResponse, error) {
	sources, err := indexer.NormalizeSources(req.Sources)
	if err != nil {
		return nil, fmt.Errorf("index sources are invalid: %w", err)
	}
	dbPath := strings.TrimSpace(req.DBPath)
	if dbPath == "" {
		return nil, fmt.Errorf("index requires a non-empty --db path")
	}

	sourceResults := make([]SourceResult, 0, len(sources))
	for _, source := range sources {
		summary, err := indexer.PreflightSource(source)
		if err != nil {
			return nil, fmt.Errorf("index preflight failed for source %q: %w", source.ID, err)
		}
		sourceResults = append(sourceResults, SourceResult{
			ID:           source.ID,
			Type:         source.Type,
			Folder:       summary.FolderPath,
			MetadataPath: summary.MetadataPath,
			PageCount:    summary.PageCount,
			CheckedFiles: summary.MarkdownChecked,
		})
	}

	// A rebuild deletes the database files, so it must be the only user; every
	// other run shares the index with queries.
	lock, err := db.AcquireLock(dbPath, req.Rebuild)
	if err != nil {
		return nil, fmt.Errorf("index lock failed: %w", err)
	}
	defer func() { _ = lock.Release() }()

	if req.Rebuild {
		// The WAL and shared-memory sidecars belong to the file being replaced.
		for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("index rebuild failed to recreate db file: %w", err)
			}
		}
	}

	database, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("index db init failed: %w", err)
	}
	defer func() { _ = database.Close() }()

	if err := db.Migrate(ctx, database); err != nil {
		return nil, fmt.Errorf("index db migration failed: %w", err)
	}

	runMode := "incremental"
	if req.Rebuild {
		runMode = "rebuild"
	}

	run, err := db.BeginRun(ctx, database, runMode)
	if err != nil {
		return nil, fmt.Errorf("index run tracking failed: %w", err)
	}

	var documents []indexer.DocumentInput
	crawls := make([]indexer.CrawlInfo, 0, len(sources))
	var totalPages, totalChecked int
	for i, source := range sources {
		corpus, err := indexer.LoadSource(source, indexer.DefaultChunkSize, indexer.DefaultChunkOverlap)
		if err != nil {
			return nil, fmt.Errorf("index ingestion failed for source %q: %w", source.ID, err)
		}
		documents = append(documents, corpus.Documents...)

		// Only a crawl has crawl times, so a files source must not drag the combined
		// last-success time to "unknown".
		if source.Type == indexer.TypeConfluence {
			crawls = append(crawls, corpus.Crawl)
		}

		// What was read can differ from what the preflight counted, because a file that
		// is not text or is empty is only found when it is read.
		sourceResults[i].PageCount = len(corpus.Documents)
		sourceResults[i].SkippedFiles = corpus.Skipped
		totalPages += len(corpus.Documents)
		totalChecked += sourceResults[i].CheckedFiles
	}
	crawl := combineCrawls(crawls)
	crawl.PageCount = len(documents)

	embProvider, embOptions, embSource, err := resolveIndexProvider(req.Provider, req.Embedding)
	if err != nil {
		return nil, err
	}

	// An empty identity tells the store that no vectors are expected, which keeps
	// the skip decision correct when embeddings are disabled.
	embeddingName := ""
	if embProvider.Caps().Enabled() {
		embeddingName = embProvider.Name()
	}

	// The header puts page metadata into the embedding input, so the store has to know
	// which fields invalidate vectors when they move without the chunk text.
	headerFields := db.EmbeddingHeader{
		Title: embedding.HasContext(embOptions.Context, embedding.ContextTitle),
		Space: embedding.HasContext(embOptions.Context, embedding.ContextSpace),
	}

	var inserted, updated, metadataUpdates, skipped, chunkWrites, embeddingWrites int

	emit := func(event ProgressEvent) {
		if req.Progress != nil {
			req.Progress(event)
		}
	}
	emit(ProgressEvent{Event: "start", Total: len(documents)})
	keepIDs := make([]string, 0, len(documents))
	for i, doc := range documents {
		keepIDs = append(keepIDs, doc.ID)
		docRecord := db.DocumentRecord{
			ID:             doc.ID,
			PageID:         doc.PageID,
			SourceID:       doc.SourceID,
			SourceType:     doc.SourceType,
			Title:          doc.Title,
			LocalPath:      doc.LocalPath,
			SpaceKey:       doc.SpaceKey,
			SourceURL:      doc.SourceURL,
			LastModifiedAt: doc.ModifiedAt,
			ContentHash:    doc.ContentHash,
			MetadataHash:   doc.MetadataFingerprint(),
			Host:           doc.Metadata.Host,
			CanonicalURL:   doc.Metadata.CanonicalURL,
			Version:        doc.Metadata.Version,
			Depth:          doc.Metadata.Depth,
			ParentID:       doc.Metadata.ParentID,
			CreatedAt:      doc.Metadata.CreatedAt,
			CrawledAt:      doc.Metadata.CrawledAt,
			CreatedByName:  doc.Metadata.CreatedByName,
			ModifiedByName: doc.Metadata.ModifiedByName,
			IsSeed:         doc.Metadata.IsSeed,
			LinkIn:         doc.Metadata.LinkIn,
			LinkOut:        doc.Metadata.LinkOut,
			Attachments:    doc.Metadata.Attachments,
			Comments:       doc.Metadata.Comments,
		}
		chunkRecords := make([]db.ChunkRecord, 0, len(doc.Chunks))
		chunkTexts := make([]string, 0, len(doc.Chunks))
		for _, ch := range doc.Chunks {
			chunkRecords = append(chunkRecords, db.ChunkRecord{
				ID:         ch.ID,
				ChunkIndex: ch.ChunkIndex,
				Text:       ch.Text,
				ChunkHash:  ch.ChunkHash,
				// The breadcrumb is what the full-text index weights as the section
				// column; without it a chunk cannot be found by an ancestor heading.
				Section: ch.Section,
			})
			chunkTexts = append(chunkTexts, embedding.DocumentText(embOptions.Context, embedding.ContextFields{
				Title:   doc.Title,
				Space:   doc.SpaceKey,
				Section: ch.Section,
			}, ch.Text))
		}

		status, written, err := db.UpsertDocumentWithChunks(ctx, database, docRecord, chunkRecords, embeddingName, headerFields)
		if err != nil {
			return nil, fmt.Errorf("index persistence failed for page %s: %w", doc.ID, err)
		}
		switch status {
		case "inserted":
			inserted++
		case "updated":
			updated++
		case "metadata":
			metadataUpdates++
		case "skipped":
			skipped++
		}
		chunkWrites += written

		if written > 0 && len(chunkRecords) > 0 && embProvider.Caps().Enabled() {
			vectors, err := embProvider.Embed(ctx, embedding.KindDocument, chunkTexts)
			if err != nil {
				return nil, fmt.Errorf("index embedding failed for page %s: %w", doc.ID, err)
			}
			recs := make([]db.EmbeddingRecord, 0, len(vectors))
			for i, vec := range vectors {
				if i >= len(chunkRecords) {
					break
				}
				recs = append(recs, db.EmbeddingRecord{
					ChunkID:   chunkRecords[i].ID,
					Name:      embProvider.Name(),
					Dimension: len(vec),
					Vector:    vec,
				})
			}
			writtenEmbeddings, err := db.UpsertEmbeddings(ctx, database, recs)
			if err != nil {
				return nil, fmt.Errorf("index embedding persistence failed for page %s: %w", doc.ID, err)
			}
			embeddingWrites += writtenEmbeddings
		}

		emit(ProgressEvent{Event: "document", SourceID: doc.SourceID, DocumentID: doc.ID, Status: status, Done: i + 1, Total: len(documents)})
	}

	deleted, err := db.DeleteDocumentsNotIn(ctx, database, keepIDs)
	if err != nil {
		return nil, fmt.Errorf("index stale cleanup failed: %w", err)
	}

	// Keep a single vector space per index, and record which identity produced it
	// so query time can detect a mismatch instead of scoring zeros.
	pruned := 0
	if embProvider.Caps().Enabled() {
		removed, err := db.DeleteEmbeddingsNotNamed(ctx, database, embProvider.Name())
		if err != nil {
			return nil, fmt.Errorf("index stale embedding cleanup failed: %w", err)
		}
		pruned = int(removed)

		if err := db.RecordEmbeddingRun(ctx, database, run.ID, embProvider.Name(), embProvider.Dimension(), embProvider.Caps().Capability()); err != nil {
			return nil, fmt.Errorf("index embedding run recording failed: %w", err)
		}
	}

	if err := db.CompleteRun(ctx, database, run.ID); err != nil {
		return nil, fmt.Errorf("index run completion failed: %w", err)
	}

	// Record the crawl the index was built from, so a later report can show how
	// stale the index is relative to the crawler output.
	snapshot := db.CorpusSnapshot{
		RunID:       run.ID,
		StartedAt:   crawl.StartedAt,
		CompletedAt: crawl.CompletedAt,
		SucceededAt: crawl.SucceededAt,
		Mode:        crawl.Mode,
		SeedCount:   crawl.SeedCount,
		PageCount:   crawl.PageCount,
	}
	if err := db.RecordCorpusSnapshot(ctx, database, run.ID, snapshot); err != nil {
		return nil, fmt.Errorf("index corpus snapshot failed: %w", err)
	}
	// The recorded row derives IndexedAt from the run, so report the same value here.
	snapshot.IndexedAt = run.StartedAt.UTC().Format(time.RFC3339Nano)

	dbStats, err := db.GetStats(ctx, database)
	if err != nil {
		return nil, fmt.Errorf("index db stats failed: %w", err)
	}

	emit(ProgressEvent{Event: "finished", Done: len(documents), Total: len(documents), Summary: &ProgressSummary{
		Inserted:        inserted,
		Updated:         updated,
		MetadataUpdates: metadataUpdates,
		Skipped:         skipped,
		Deleted:         int(deleted),
		ChunkWrites:     chunkWrites,
		EmbeddingWrites: embeddingWrites,
	}})

	return &IndexResponse{
		Status:              "phase4-ready",
		Incremental:         !req.Rebuild,
		Rebuild:             req.Rebuild,
		DBPath:              dbPath,
		EmbeddingName:       embProvider.Name(),
		EmbeddingSource:     embSource,
		EmbeddingWrites:     embeddingWrites,
		EmbeddingPruned:     pruned,
		EmbeddingDimension:  embProvider.Dimension(),
		EmbeddingCapability: embProvider.Caps().Capability(),
		Sources:             sourceResults,
		PageCount:           totalPages,
		CheckedFiles:        totalChecked,
		Inserted:            inserted,
		Updated:             updated,
		MetadataUpdates:     metadataUpdates,
		Skipped:             skipped,
		Deleted:             int(deleted),
		ChunkWrites:         chunkWrites,
		RunID:               run.ID,
		DBStats:             dbStats,
		Corpus:              &snapshot,
	}, nil
}

// resolveIndexProvider prefers an injected provider so tests never touch the
// network, and otherwise resolves one from flags, environment and defaults. It returns
// the merged options too: the caller builds document text from the context selection,
// which the environment or the configuration file may have supplied.
func resolveIndexProvider(injected embedding.Provider, options embedding.Options) (embedding.Provider, embedding.Options, string, error) {
	if injected != nil {
		return injected, options, "injected", nil
	}

	resolution, err := embedding.Resolve(options)
	if err != nil {
		return nil, embedding.Options{}, "", err
	}
	return resolution.Provider, resolution.Options, resolution.Source, nil
}
