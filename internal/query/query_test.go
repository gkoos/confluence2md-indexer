package query

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/embedding"
)

func TestRunHybridWeightedPrefersLexicalWithHighAlpha(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	results, total, err := Run(context.Background(), database, embedding.NewHashProvider(8), Request{
		Text:       "banana",
		Mode:       "hybrid",
		Fusion:     "weighted",
		Alpha:      0.95,
		TopK:       5,
		CandidateK: 10,
	})
	if err != nil {
		t.Fatalf("run query: %v", err)
	}
	if total == 0 {
		t.Fatalf("expected total > 0")
	}
	if len(results) == 0 {
		t.Fatalf("expected non-empty results")
	}
	if results[0].ChunkID != "p2:000000" {
		t.Fatalf("expected lexical top result p2:000000 got %s", results[0].ChunkID)
	}
}

func TestRunHybridRRFCombinesBothChannels(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	results, _, err := Run(context.Background(), database, embedding.NewHashProvider(8), Request{
		Text:       "banana",
		Mode:       "hybrid",
		Fusion:     "rrf",
		RRFK:       60,
		TopK:       5,
		CandidateK: 10,
	})
	if err != nil {
		t.Fatalf("run query: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results got %d", len(results))
	}
	if results[0].Fusion != "rrf" {
		t.Fatalf("expected rrf fusion got %s", results[0].Fusion)
	}
	if results[0].Fused < results[1].Fused {
		t.Fatalf("expected sorted fused scores desc")
	}
}

func TestRunTopKLimit(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	results, _, err := Run(context.Background(), database, embedding.NewHashProvider(8), Request{
		Text:       "banana",
		Mode:       "lexical",
		TopK:       1,
		CandidateK: 10,
	})
	if err != nil {
		t.Fatalf("run query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected top-k to limit to 1 result, got %d", len(results))
	}
}

func TestRunExpandStitchesNeighborChunks(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	results, _, err := Run(context.Background(), database, embedding.NewHashProvider(8), Request{
		Text:       "middleterm",
		Mode:       "lexical",
		TopK:       1,
		CandidateK: 10,
		Expand:     1,
	})
	if err != nil {
		t.Fatalf("run query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result got %d", len(results))
	}
	if results[0].ChunkID != "p3:000001" {
		t.Fatalf("expected center chunk p3:000001 got %s", results[0].ChunkID)
	}
	if results[0].ContextStartIndex != 0 || results[0].ContextEndIndex != 2 {
		t.Fatalf("expected context range 0..2 got %d..%d", results[0].ContextStartIndex, results[0].ContextEndIndex)
	}
	if results[0].ContextChunkCount != 3 {
		t.Fatalf("expected context chunk count 3 got %d", results[0].ContextChunkCount)
	}
	if results[0].BaseChunkText == "" {
		t.Fatalf("expected base chunk text to be preserved")
	}
	if !strings.Contains(results[0].ChunkText, "left neighbor") || !strings.Contains(results[0].ChunkText, "right neighbor") {
		t.Fatalf("expected expanded chunk text to include neighboring chunks")
	}
}

func TestRunPaginationOffsetLimit(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	results, total, err := Run(context.Background(), database, embedding.NewHashProvider(8), Request{
		Text:       "neighbor",
		Mode:       "lexical",
		TopK:       5,
		Offset:     1,
		Limit:      1,
		CandidateK: 10,
	})
	if err != nil {
		t.Fatalf("run query: %v", err)
	}
	if total < 2 {
		t.Fatalf("expected total >= 2 got %d", total)
	}
	if len(results) != 1 {
		t.Fatalf("expected paged result length 1 got %d", len(results))
	}
	if results[0].Rank != 2 {
		t.Fatalf("expected paged rank to be global rank 2 got %d", results[0].Rank)
	}
}

func TestFuseLexicalFiltersZeroScoreTail(t *testing.T) {
	results := fuse(Request{Mode: "lexical"}, []db.Candidate{
		{ChunkID: "c-keep", LexicalScoreRaw: 10},
		{ChunkID: "c-drop", LexicalScoreRaw: 0},
	}, nil)

	if len(results) != 1 {
		t.Fatalf("expected one positive lexical result after filtering, got %d", len(results))
	}
	if results[0].ChunkID != "c-keep" {
		t.Fatalf("expected c-keep to remain, got %s", results[0].ChunkID)
	}
}

func setupQueryTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "query-test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return database
}

func seedQueryDocs(t *testing.T, database *sql.DB) {
	t.Helper()

	docs := []struct {
		doc    db.DocumentRecord
		chunks []db.ChunkRecord
	}{
		{
			doc: db.DocumentRecord{
				ID:             "p1",
				PageID:         "p1",
				Title:          "Alpha",
				LocalPath:      "p1.md",
				SpaceKey:       "ENG",
				SourceURL:      "https://example.test/p1",
				LastModifiedAt: "2026-01-01T00:00:00Z",
				ContentHash:    "h1",
			},
			chunks: []db.ChunkRecord{{ID: "p1:000000", ChunkIndex: 0, Text: "apple and pear", ChunkHash: "c1"}},
		},
		{
			doc: db.DocumentRecord{
				ID:             "p2",
				PageID:         "p2",
				Title:          "Banana",
				LocalPath:      "p2.md",
				SpaceKey:       "ENG",
				SourceURL:      "https://example.test/p2",
				LastModifiedAt: "2026-01-01T00:00:00Z",
				ContentHash:    "h2",
			},
			chunks: []db.ChunkRecord{{ID: "p2:000000", ChunkIndex: 0, Text: "banana banana yellow", ChunkHash: "c2"}},
		},
		{
			doc: db.DocumentRecord{
				ID:             "p3",
				PageID:         "p3",
				Title:          "Middle",
				LocalPath:      "p3.md",
				SpaceKey:       "ENG",
				SourceURL:      "https://example.test/p3",
				LastModifiedAt: "2026-01-01T00:00:00Z",
				ContentHash:    "h3",
			},
			chunks: []db.ChunkRecord{
				{ID: "p3:000000", ChunkIndex: 0, Text: "left neighbor", ChunkHash: "c3a"},
				{ID: "p3:000001", ChunkIndex: 1, Text: "middleterm center", ChunkHash: "c3b"},
				{ID: "p3:000002", ChunkIndex: 2, Text: "right neighbor", ChunkHash: "c3c"},
			},
		},
	}

	for _, item := range docs {
		if _, _, err := db.UpsertDocumentWithChunks(context.Background(), database, item.doc, item.chunks); err != nil {
			t.Fatalf("upsert doc %s: %v", item.doc.ID, err)
		}
	}

	provider := embedding.NewHashProvider(8)
	texts := []string{"apple and pear", "banana banana yellow", "left neighbor", "middleterm center", "right neighbor"}
	vectors, err := provider.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	_, err = db.UpsertEmbeddings(context.Background(), database, []db.EmbeddingRecord{
		{ChunkID: "p1:000000", Model: provider.Name(), Dimension: len(vectors[0]), Vector: vectors[0]},
		{ChunkID: "p2:000000", Model: provider.Name(), Dimension: len(vectors[1]), Vector: vectors[1]},
		{ChunkID: "p3:000000", Model: provider.Name(), Dimension: len(vectors[2]), Vector: vectors[2]},
		{ChunkID: "p3:000001", Model: provider.Name(), Dimension: len(vectors[3]), Vector: vectors[3]},
		{ChunkID: "p3:000002", Model: provider.Name(), Dimension: len(vectors[4]), Vector: vectors[4]},
	})
	if err != nil {
		t.Fatalf("upsert embeddings: %v", err)
	}
}
