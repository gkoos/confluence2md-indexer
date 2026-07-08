package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/embedding"
	"github.com/gkoos/confluence2md-indexer/internal/query"
	"github.com/gkoos/confluence2md-indexer/pkg/indexerapi"
)

func TestQueryReturnsResults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "service-query.db")
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

	resp, err := Query(ctx, dbPath, query.Request{Text: "banana", Mode: "hybrid", TopK: 5, CandidateK: 10})
	if err != nil {
		t.Fatalf("service query failed: %v", err)
	}
	if resp.Total == 0 || len(resp.Results) == 0 {
		t.Fatalf("expected results, got total=%d count=%d", resp.Total, len(resp.Results))
	}
}

func TestQueryValidatesDBPath(t *testing.T) {
	_, err := Query(context.Background(), "", query.Request{Text: "banana"})
	if err == nil {
		t.Fatalf("expected db path validation error")
	}
}

func TestQueryParityWithPublicAPI(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "query-parity.db")
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

	req := query.Request{Text: "banana", Mode: "hybrid", TopK: 5, CandidateK: 10}

	serviceResp, err := Query(ctx, dbPath, req)
	if err != nil {
		t.Fatalf("service query failed: %v", err)
	}

	apiResp, err := indexerapi.Query(ctx, dbPath, req)
	if err != nil {
		t.Fatalf("public api query failed: %v", err)
	}

	if serviceResp.Total != apiResp.Total {
		t.Fatalf("expected equal totals, got service=%d api=%d", serviceResp.Total, apiResp.Total)
	}
	if len(serviceResp.Results) != len(apiResp.Results) {
		t.Fatalf("expected equal result counts, got service=%d api=%d", len(serviceResp.Results), len(apiResp.Results))
	}
	if len(serviceResp.Results) == 0 {
		t.Fatalf("expected non-empty results")
	}
	if serviceResp.Results[0].ChunkID != apiResp.Results[0].ChunkID {
		t.Fatalf("expected same top chunk, got service=%s api=%s", serviceResp.Results[0].ChunkID, apiResp.Results[0].ChunkID)
	}
}
