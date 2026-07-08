package indexerapi

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/embedding"
)

func TestQueryReturnsResults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "indexerapi-query.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	_, _, err = db.UpsertDocumentWithChunks(ctx, database, db.DocumentRecord{
		ID:             "p1",
		PageID:         "p1",
		Title:          "Alpha",
		LocalPath:      "p1.md",
		SpaceKey:       "ENG",
		SourceURL:      "https://example.test/p1",
		LastModifiedAt: "2026-01-01T00:00:00Z",
		ContentHash:    "h1",
	}, []db.ChunkRecord{{ID: "p1:000000", ChunkIndex: 0, Text: "banana text", ChunkHash: "c1"}})
	if err != nil {
		t.Fatalf("seed doc: %v", err)
	}

	vectors, err := embedding.NewHashProvider(8).Embed(ctx, []string{"banana text"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	_, err = db.UpsertEmbeddings(ctx, database, []db.EmbeddingRecord{{
		ChunkID:   "p1:000000",
		Model:     "hash-local",
		Dimension: len(vectors[0]),
		Vector:    vectors[0],
	}})
	if err != nil {
		t.Fatalf("upsert embeddings: %v", err)
	}

	resp, err := Query(ctx, dbPath, QueryRequest{Text: "banana", Mode: "hybrid", TopK: 5, CandidateK: 10})
	if err != nil {
		t.Fatalf("indexerapi query failed: %v", err)
	}
	if resp.Total == 0 || len(resp.Results) == 0 {
		t.Fatalf("expected results, got total=%d count=%d", resp.Total, len(resp.Results))
	}
}

func TestQueryValidatesDBPath(t *testing.T) {
	_, err := Query(context.Background(), "", QueryRequest{Text: "banana"})
	if err == nil {
		t.Fatalf("expected db path validation error")
	}
}
