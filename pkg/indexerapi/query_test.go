package indexerapi

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
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
	}, []db.ChunkRecord{{ID: "p1:000000", ChunkIndex: 0, Text: "banana text", ChunkHash: "c1"}}, "")
	if err != nil {
		t.Fatalf("seed doc: %v", err)
	}

	resolution, err := embedding.Resolve(embedding.Options{})
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}
	vectors, err := resolution.Provider.Embed(ctx, embedding.KindDocument, []string{"banana text"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	_, err = db.UpsertEmbeddings(ctx, database, []db.EmbeddingRecord{{
		ChunkID:   "p1:000000",
		Name:      resolution.Provider.Name(),
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

func TestQueryExposesFacets(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "indexerapi-facets.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	docs := []db.DocumentRecord{
		{
			ID: "p1", PageID: "p1", Title: "Alpha", LocalPath: "p1.md", SpaceKey: "ENG",
			SourceURL: "https://example.test/p1", ContentHash: "h1",
			CreatedByName: "Ada Lovelace", ModifiedByName: "Ada Lovelace",
		},
		{
			ID: "p2", PageID: "p2", Title: "Beta", LocalPath: "p2.md", SpaceKey: "OPS",
			SourceURL: "https://example.test/p2", ContentHash: "h2",
			CreatedByName: "Grace Hopper", ModifiedByName: "Grace Hopper",
		},
	}
	for _, doc := range docs {
		chunk := db.ChunkRecord{ID: doc.ID + ":000000", ChunkIndex: 0, Text: "banana text", ChunkHash: "c-" + doc.ID}
		if _, _, err := db.UpsertDocumentWithChunks(ctx, database, doc, []db.ChunkRecord{chunk}, ""); err != nil {
			t.Fatalf("seed doc %s: %v", doc.ID, err)
		}
	}

	// A lexical query needs no provider, which keeps this test about the payload.
	resp, err := Query(ctx, dbPath, QueryRequest{
		Text:   "banana",
		Mode:   "lexical",
		TopK:   5,
		Facets: []string{"space", "author"},
	})
	if err != nil {
		t.Fatalf("indexerapi query failed: %v", err)
	}

	if got := resp.Facets["space"]; got["ENG"] != 1 || got["OPS"] != 1 {
		t.Fatalf("space facets = %v, want one page per space", got)
	}
	if got := resp.Facets["author"]; got["Ada Lovelace"] != 1 || got["Grace Hopper"] != 1 {
		t.Fatalf("author facets = %v, want one page per author", got)
	}

	// The exported response has to serialize the counts under the same key the CLI
	// uses, because the MCP server consumes this shape.
	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if !strings.Contains(string(encoded), `"facets":{`) {
		t.Fatalf("expected the facets block in the json response, got %s", encoded)
	}
	// encoding/json sorts map keys, so the bucket order is stable for consumers.
	if !strings.Contains(string(encoded), `"space":{"ENG":1,"OPS":1}`) {
		t.Fatalf("expected the space buckets in the json response, got %s", encoded)
	}
}

func TestQueryValidatesDBPath(t *testing.T) {
	_, err := Query(context.Background(), "", QueryRequest{Text: "banana"})
	if err == nil {
		t.Fatalf("expected db path validation error")
	}
}
