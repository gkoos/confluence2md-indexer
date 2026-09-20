package service

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/embedding"
	"github.com/gkoos/confluence2md-indexer/internal/indexer"
)

type IndexRequest struct {
	Folder  string
	DBPath  string
	Rebuild bool
	// Provider overrides provider resolution. Tests inject a deterministic
	// provider here; command paths leave it nil and resolve from flags,
	// environment and defaults.
	Provider embedding.Provider
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
	InputFolder         string
	MetadataPath        string
	PageCount           int
	CheckedFiles        int
	Inserted            int
	Updated             int
	Skipped             int
	Deleted             int
	ChunkWrites         int
	RunID               string
	DBStats             *db.Stats
}

func Index(ctx context.Context, req IndexRequest) (*IndexResponse, error) {
	folder := strings.TrimSpace(req.Folder)
	if folder == "" {
		return nil, fmt.Errorf("index requires a non-empty folder")
	}
	dbPath := strings.TrimSpace(req.DBPath)
	if dbPath == "" {
		return nil, fmt.Errorf("index requires a non-empty --db path")
	}

	summary, err := indexer.Preflight(folder)
	if err != nil {
		return nil, fmt.Errorf("index preflight failed: %w", err)
	}

	if req.Rebuild {
		if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("index rebuild failed to recreate db file: %w", err)
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

	docs, err := indexer.LoadDocuments(folder, indexer.DefaultChunkSize, indexer.DefaultChunkOverlap)
	if err != nil {
		return nil, fmt.Errorf("index ingestion failed: %w", err)
	}

	embProvider, embSource, err := resolveIndexProvider(req.Provider)
	if err != nil {
		return nil, err
	}

	// An empty identity tells the store that no vectors are expected, which keeps
	// the skip decision correct when embeddings are disabled.
	embeddingName := ""
	if embProvider.Caps().Enabled() {
		embeddingName = embProvider.Name()
	}

	var inserted, updated, skipped, chunkWrites, embeddingWrites int
	keepIDs := make([]string, 0, len(docs))
	for _, doc := range docs {
		keepIDs = append(keepIDs, doc.ID)
		docRecord := db.DocumentRecord{
			ID:             doc.ID,
			PageID:         doc.PageID,
			Title:          doc.Title,
			LocalPath:      doc.LocalPath,
			SpaceKey:       doc.SpaceKey,
			SourceURL:      doc.SourceURL,
			LastModifiedAt: doc.ModifiedAt,
			ContentHash:    doc.ContentHash,
		}
		chunkRecords := make([]db.ChunkRecord, 0, len(doc.Chunks))
		chunkTexts := make([]string, 0, len(doc.Chunks))
		for _, ch := range doc.Chunks {
			chunkRecords = append(chunkRecords, db.ChunkRecord{
				ID:         ch.ID,
				ChunkIndex: ch.ChunkIndex,
				Text:       ch.Text,
				ChunkHash:  ch.ChunkHash,
			})
			chunkTexts = append(chunkTexts, ch.Text)
		}

		status, written, err := db.UpsertDocumentWithChunks(ctx, database, docRecord, chunkRecords, embeddingName)
		if err != nil {
			return nil, fmt.Errorf("index persistence failed for page %s: %w", doc.PageID, err)
		}
		switch status {
		case "inserted":
			inserted++
		case "updated":
			updated++
		case "skipped":
			skipped++
		}
		chunkWrites += written

		if written > 0 && len(chunkRecords) > 0 && embProvider.Caps().Enabled() {
			vectors, err := embProvider.Embed(ctx, embedding.KindDocument, chunkTexts)
			if err != nil {
				return nil, fmt.Errorf("index embedding failed for page %s: %w", doc.PageID, err)
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
				return nil, fmt.Errorf("index embedding persistence failed for page %s: %w", doc.PageID, err)
			}
			embeddingWrites += writtenEmbeddings
		}
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

	dbStats, err := db.GetStats(ctx, database)
	if err != nil {
		return nil, fmt.Errorf("index db stats failed: %w", err)
	}

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
		InputFolder:         summary.FolderPath,
		MetadataPath:        summary.MetadataPath,
		PageCount:           summary.PageCount,
		CheckedFiles:        summary.MarkdownChecked,
		Inserted:            inserted,
		Updated:             updated,
		Skipped:             skipped,
		Deleted:             int(deleted),
		ChunkWrites:         chunkWrites,
		RunID:               run.ID,
		DBStats:             dbStats,
	}, nil
}

// resolveIndexProvider prefers an injected provider so tests never touch the
// network, and otherwise resolves one from flags, environment and defaults.
func resolveIndexProvider(injected embedding.Provider) (embedding.Provider, string, error) {
	if injected != nil {
		return injected, "injected", nil
	}

	resolution, err := embedding.Resolve(embedding.Options{})
	if err != nil {
		return nil, "", fmt.Errorf("index embedding provider setup failed: %w", err)
	}
	return resolution.Provider, resolution.Source, nil
}
