package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/embedding"
)

func TestMigrateAndRunLifecycle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	if err := Migrate(ctx, database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	run, err := BeginRun(ctx, database, "incremental")
	if err != nil {
		t.Fatalf("begin run: %v", err)
	}
	if run.ID == "" {
		t.Fatalf("expected non-empty run id")
	}

	if err := CompleteRun(ctx, database, run.ID); err != nil {
		t.Fatalf("complete run: %v", err)
	}

	stats, err := GetStats(ctx, database)
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("expected schema version %d got %d", CurrentSchemaVersion, stats.SchemaVersion)
	}
	if stats.Runs != 1 {
		t.Fatalf("expected runs=1 got %d", stats.Runs)
	}
}

func TestUpsertDocumentWithChunksLifecycle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	if err := Migrate(ctx, database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	doc := DocumentRecord{
		ID:          "1",
		PageID:      "1",
		Title:       "Doc",
		LocalPath:   "doc.md",
		ContentHash: "hash1",
	}
	chunks := []ChunkRecord{{ID: "1:000000", ChunkIndex: 0, Text: "hello", ChunkHash: "c1"}}

	status, written, err := UpsertDocumentWithChunks(ctx, database, doc, chunks)
	if err != nil {
		t.Fatalf("insert doc: %v", err)
	}
	if status != "inserted" || written != 1 {
		t.Fatalf("unexpected insert status=%s written=%d", status, written)
	}

	status, written, err = UpsertDocumentWithChunks(ctx, database, doc, chunks)
	if err != nil {
		t.Fatalf("skip doc: %v", err)
	}
	if status != "skipped" || written != 0 {
		t.Fatalf("unexpected skip status=%s written=%d", status, written)
	}

	doc.ContentHash = "hash2"
	chunks = []ChunkRecord{{ID: "1:000000", ChunkIndex: 0, Text: "updated", ChunkHash: "c2"}}
	status, written, err = UpsertDocumentWithChunks(ctx, database, doc, chunks)
	if err != nil {
		t.Fatalf("update doc: %v", err)
	}
	if status != "updated" || written != 1 {
		t.Fatalf("unexpected update status=%s written=%d", status, written)
	}
}

func TestDeleteDocumentsNotIn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	if err := Migrate(ctx, database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, id := range []string{"1", "2"} {
		_, _, err := UpsertDocumentWithChunks(ctx, database, DocumentRecord{
			ID:          id,
			PageID:      id,
			Title:       "Doc",
			LocalPath:   id + ".md",
			ContentHash: "h" + id,
		}, nil)
		if err != nil {
			t.Fatalf("seed doc %s: %v", id, err)
		}
	}

	deleted, err := DeleteDocumentsNotIn(ctx, database, []string{"1"})
	if err != nil {
		t.Fatalf("delete stale: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted got %d", deleted)
	}
}

func TestSearchLexicalAndVectorWithFilters(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	if err := Migrate(ctx, database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	_, _, err = UpsertDocumentWithChunks(ctx, database, DocumentRecord{
		ID:             "1",
		PageID:         "1",
		Title:          "Alpha",
		LocalPath:      "a.md",
		SpaceKey:       "ENG",
		SourceURL:      "https://example.test/1",
		LastModifiedAt: "2026-01-10T00:00:00Z",
		ContentHash:    "h1",
	}, []ChunkRecord{{ID: "1:000000", ChunkIndex: 0, Text: "banana apple", ChunkHash: "c1"}})
	if err != nil {
		t.Fatalf("seed doc 1: %v", err)
	}

	_, _, err = UpsertDocumentWithChunks(ctx, database, DocumentRecord{
		ID:             "2",
		PageID:         "2",
		Title:          "Beta",
		LocalPath:      "b.md",
		SpaceKey:       "OPS",
		SourceURL:      "https://example.test/2",
		LastModifiedAt: "2025-01-10T00:00:00Z",
		ContentHash:    "h2",
	}, []ChunkRecord{{ID: "2:000000", ChunkIndex: 0, Text: "grape orange", ChunkHash: "c2"}})
	if err != nil {
		t.Fatalf("seed doc 2: %v", err)
	}

	provider := embedding.NewHashProvider(8)
	vectors, err := provider.Embed(ctx, []string{"banana apple", "grape orange"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	_, err = UpsertEmbeddings(ctx, database, []EmbeddingRecord{
		{ChunkID: "1:000000", Model: provider.Name(), Dimension: len(vectors[0]), Vector: vectors[0]},
		{ChunkID: "2:000000", Model: provider.Name(), Dimension: len(vectors[1]), Vector: vectors[1]},
	})
	if err != nil {
		t.Fatalf("upsert embeddings: %v", err)
	}

	lexical, err := SearchLexical(ctx, database, "banana", SearchFilters{Candidate: 5})
	if err != nil {
		t.Fatalf("search lexical: %v", err)
	}
	if len(lexical) == 0 || lexical[0].ChunkID != "1:000000" {
		t.Fatalf("expected lexical top chunk 1:000000")
	}

	vector, err := SearchVector(ctx, database, vectors[0], SearchFilters{Candidate: 5})
	if err != nil {
		t.Fatalf("search vector: %v", err)
	}
	if len(vector) == 0 || vector[0].ChunkID != "1:000000" {
		t.Fatalf("expected vector top chunk 1:000000")
	}

	filtered, err := SearchLexical(ctx, database, "banana", SearchFilters{Candidate: 5, SpaceKey: "OPS"})
	if err != nil {
		t.Fatalf("search lexical filtered: %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("expected no lexical results for non-matching space filter")
	}
}

func TestFetchChunkWindow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	if err := Migrate(ctx, database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	_, _, err = UpsertDocumentWithChunks(ctx, database, DocumentRecord{
		ID:             "doc",
		PageID:         "doc",
		Title:          "Doc",
		LocalPath:      "doc.md",
		SpaceKey:       "ENG",
		SourceURL:      "https://example.test/doc",
		LastModifiedAt: "2026-01-01T00:00:00Z",
		ContentHash:    "h-doc",
	}, []ChunkRecord{
		{ID: "doc:000000", ChunkIndex: 0, Text: "zero", ChunkHash: "c0"},
		{ID: "doc:000001", ChunkIndex: 1, Text: "one", ChunkHash: "c1"},
		{ID: "doc:000002", ChunkIndex: 2, Text: "two", ChunkHash: "c2"},
	})
	if err != nil {
		t.Fatalf("seed doc: %v", err)
	}

	window, err := FetchChunkWindow(ctx, database, "doc", 1, 1)
	if err != nil {
		t.Fatalf("fetch chunk window: %v", err)
	}
	if len(window) != 3 {
		t.Fatalf("expected 3 chunks in window got %d", len(window))
	}
	if window[0].ChunkIndex != 0 || window[2].ChunkIndex != 2 {
		t.Fatalf("unexpected chunk index window: %+v", window)
	}
	if window[1].Text != "one" {
		t.Fatalf("unexpected center chunk text: %s", window[1].Text)
	}
}
