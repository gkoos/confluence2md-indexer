package db

import (
	"context"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/embedding"
	"github.com/gkoos/confluence2md-indexer/internal/embedding/embeddingtest"
)

func TestUpsertRefreshesMetadataWithoutRewritingChunks(t *testing.T) {
	database := newTestDatabase(t)
	ctx := context.Background()

	doc := DocumentRecord{
		ID: "1", PageID: "1", Title: "Deployment", LocalPath: "a.md", SpaceKey: "OPS",
		LastModifiedAt: "2026-05-20T10:00:00Z", ContentHash: "h1", MetadataHash: "m1", Depth: 1,
	}
	chunk := ChunkRecord{ID: "1:000000", ChunkIndex: 0, Text: "rollback steps", ChunkHash: "c1"}

	provider := embeddingtest.New(8)
	vectors, err := provider.Embed(ctx, embedding.KindDocument, []string{chunk.Text})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	status, _, err := UpsertDocumentWithChunks(ctx, database, doc, []ChunkRecord{chunk}, provider.Name())
	if err != nil || status != "inserted" {
		t.Fatalf("insert status = %q, err = %v, want inserted", status, err)
	}
	if _, err := UpsertEmbeddings(ctx, database, []EmbeddingRecord{
		{ChunkID: chunk.ID, Name: provider.Name(), Dimension: len(vectors[0]), Vector: vectors[0]},
	}); err != nil {
		t.Fatalf("upsert embeddings: %v", err)
	}

	// A re-crawl that changed metadata only.
	updated := doc
	updated.Title = "Deployment renamed"
	updated.Depth = 4
	updated.IsSeed = true
	updated.CreatedByName = "Ada Lovelace"
	updated.MetadataHash = "m2"

	status, written, err := UpsertDocumentWithChunks(ctx, database, updated, []ChunkRecord{chunk}, provider.Name())
	if err != nil {
		t.Fatalf("metadata update: %v", err)
	}
	if status != "metadata" || written != 0 {
		t.Fatalf("status = %q, written = %d, want metadata with no chunk writes", status, written)
	}

	var depth int
	var createdBy string
	if err := database.QueryRowContext(ctx, `SELECT depth, created_by_name FROM documents WHERE id = '1'`).Scan(&depth, &createdBy); err != nil {
		t.Fatalf("read document: %v", err)
	}
	if depth != 4 || createdBy != "Ada Lovelace" {
		t.Fatalf("depth = %d, created_by_name = %q, want the refreshed metadata", depth, createdBy)
	}

	var embeddings int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM embeddings`).Scan(&embeddings); err != nil {
		t.Fatalf("count embeddings: %v", err)
	}
	if embeddings != 1 {
		t.Fatalf("embeddings = %d, want the stored vector untouched", embeddings)
	}

	// The title copy in the full-text index follows a rename that arrives through
	// metadata alone.
	hits, err := SearchLexical(ctx, database, "renamed", SearchFilters{Candidate: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want the document found by its new title", len(hits))
	}
}

func TestUpsertSkipsAnUnchangedDocument(t *testing.T) {
	database := newTestDatabase(t)
	ctx := context.Background()

	doc := DocumentRecord{
		ID: "1", PageID: "1", Title: "Deployment", LocalPath: "a.md", SpaceKey: "OPS",
		LastModifiedAt: "2026-05-20T10:00:00Z", ContentHash: "h1", MetadataHash: "m1",
	}
	chunks := []ChunkRecord{{ID: "1:000000", ChunkIndex: 0, Text: "body", ChunkHash: "c1"}}

	if _, _, err := UpsertDocumentWithChunks(ctx, database, doc, chunks, ""); err != nil {
		t.Fatalf("insert: %v", err)
	}

	status, written, err := UpsertDocumentWithChunks(ctx, database, doc, chunks, "")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if status != "skipped" || written != 0 {
		t.Fatalf("status = %q, written = %d, want skipped/0", status, written)
	}
}

func TestUpsertRewritesWhenTheTextChanges(t *testing.T) {
	database := newTestDatabase(t)
	ctx := context.Background()

	doc := DocumentRecord{
		ID: "1", PageID: "1", Title: "Deployment", LocalPath: "a.md", SpaceKey: "OPS",
		LastModifiedAt: "2026-05-20T10:00:00Z", ContentHash: "h1", MetadataHash: "m1",
	}
	if _, _, err := UpsertDocumentWithChunks(ctx, database, doc, []ChunkRecord{
		{ID: "1:000000", ChunkIndex: 0, Text: "old body", ChunkHash: "c1"},
	}, ""); err != nil {
		t.Fatalf("insert: %v", err)
	}

	doc.ContentHash = "h2"
	status, written, err := UpsertDocumentWithChunks(ctx, database, doc, []ChunkRecord{
		{ID: "1:000000", ChunkIndex: 0, Text: "new body", ChunkHash: "c2"},
		{ID: "1:000001", ChunkIndex: 1, Text: "second chunk", ChunkHash: "c3"},
	}, "")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if status != "updated" || written != 2 {
		t.Fatalf("status = %q, written = %d, want updated/2", status, written)
	}
}

func TestRecordCorpusSnapshotUpsertsPerRun(t *testing.T) {
	database := newTestDatabase(t)
	ctx := context.Background()

	run, err := BeginRun(ctx, database, "rebuild")
	if err != nil {
		t.Fatalf("begin run: %v", err)
	}

	first := CorpusSnapshot{StartedAt: "2026-05-22T10:00:00Z", PageCount: 3, SeedCount: 1, Mode: "full"}
	if err := RecordCorpusSnapshot(ctx, database, run.ID, first); err != nil {
		t.Fatalf("record snapshot: %v", err)
	}

	second := CorpusSnapshot{StartedAt: "2026-06-01T10:00:00Z", CompletedAt: "2026-06-01T10:05:00Z", PageCount: 5, SeedCount: 2, Mode: "updates"}
	if err := RecordCorpusSnapshot(ctx, database, run.ID, second); err != nil {
		t.Fatalf("record second snapshot: %v", err)
	}

	var rows, pageCount int
	var mode string
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*), page_count, crawl_mode FROM corpus_snapshot WHERE run_id = ?`, run.ID).Scan(&rows, &pageCount, &mode); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if rows != 1 || pageCount != 5 || mode != "updates" {
		t.Fatalf("rows = %d, page count = %d, mode = %q, want one updated row", rows, pageCount, mode)
	}
}
