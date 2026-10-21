package db

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
)

const testVectorName = "test:unit@4"

// seedVectorEntry stores one chunk with an embedding, so a vector query has something
// to scan.
func seedVectorEntry(t *testing.T, database *sql.DB, chunkID string, vector []float32) {
	t.Helper()

	records := []EmbeddingRecord{{
		ChunkID:   chunkID,
		Name:      testVectorName,
		Dimension: len(vector),
		Vector:    vector,
	}}
	if _, err := UpsertEmbeddings(context.Background(), database, records); err != nil {
		t.Fatalf("upsert embedding %s: %v", chunkID, err)
	}
}

func vectorFilters() SearchFilters {
	return SearchFilters{Candidate: 10, EmbeddingName: testVectorName}
}

// TestSearchVectorHydratesEveryCandidateField guards the second phase: priors and the
// fused output read these fields off the vector channel's candidates, so a missing one
// would silently change ranking rather than fail.
func TestSearchVectorHydratesEveryCandidateField(t *testing.T) {
	database := newTestDatabase(t)

	doc := DocumentRecord{
		ID: "1", PageID: "page-1", Title: "Seed page", LocalPath: "1.md", SpaceKey: "OPS",
		SourceURL: "https://example.test/1", CanonicalURL: "https://example.test/wiki/1",
		Host: "a.test", LastModifiedAt: "2026-05-20T10:00:00Z", Depth: 0, IsSeed: true,
		CreatedByName: "Ada Lovelace", ModifiedByName: "Grace Hopper",
		LinkIn: 3, LinkOut: 2, Attachments: 2, Comments: 1,
		ContentHash: "h-1", MetadataHash: "m-1",
	}
	chunk := ChunkRecord{ID: "1:000000", ChunkIndex: 0, Text: "vector candidate text", ChunkHash: "c-1", Section: "Deployment"}
	seedSearchDocument(t, database, doc, chunk)
	seedVectorEntry(t, database, chunk.ID, []float32{1, 0, 0, 0})

	hits, err := SearchVector(context.Background(), database, []float32{1, 0, 0, 0}, vectorFilters())
	if err != nil {
		t.Fatalf("search vector: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}

	want := Candidate{
		ChunkID: "1:000000", DocumentID: "1", PageID: "page-1", Title: "Seed page",
		LocalPath: "1.md", SpaceKey: "OPS", SourceURL: "https://example.test/1",
		LastModifiedAt: "2026-05-20T10:00:00Z", ChunkText: "vector candidate text",
		ChunkIndex: 0, Host: "a.test", CanonicalURL: "https://example.test/wiki/1",
		Depth: 0, IsSeed: true, CreatedByName: "Ada Lovelace", ModifiedByName: "Grace Hopper",
		LinkIn: 3, Attachments: 2, Comments: 1,
		VectorScoreRaw: 1,
	}
	if !reflect.DeepEqual(hits[0], want) {
		t.Fatalf("candidate = %+v, want %+v", hits[0], want)
	}
}

func TestSearchVectorKeepsTieBreakOrder(t *testing.T) {
	database := newTestDatabase(t)

	doc := DocumentRecord{
		ID: "1", PageID: "1", Title: "Ties", LocalPath: "1.md", SpaceKey: "OPS",
		ContentHash: "h-1", MetadataHash: "m-1", Host: "a.test",
	}
	chunks := []ChunkRecord{
		{ID: "1:000001", ChunkIndex: 1, Text: "second", ChunkHash: "c-2"},
		{ID: "1:000000", ChunkIndex: 0, Text: "first", ChunkHash: "c-1"},
	}
	if _, _, err := UpsertDocumentWithChunks(context.Background(), database, doc, chunks, ""); err != nil {
		t.Fatalf("seed document: %v", err)
	}
	// Identical vectors mean identical scores, so only the chunk id breaks the tie.
	seedVectorEntry(t, database, "1:000000", []float32{1, 0, 0, 0})
	seedVectorEntry(t, database, "1:000001", []float32{1, 0, 0, 0})

	hits, err := SearchVector(context.Background(), database, []float32{1, 0, 0, 0}, vectorFilters())
	if err != nil {
		t.Fatalf("search vector: %v", err)
	}
	if got := []string{hits[0].ChunkID, hits[1].ChunkID}; !reflect.DeepEqual(got, []string{"1:000000", "1:000001"}) {
		t.Fatalf("chunk order = %v, want ascending chunk ids on equal scores", got)
	}
}

func TestSearchVectorSkipsUndecodableVectors(t *testing.T) {
	database := newTestDatabase(t)

	doc := DocumentRecord{
		ID: "1", PageID: "1", Title: "Broken vector", LocalPath: "1.md", SpaceKey: "OPS",
		ContentHash: "h-1", MetadataHash: "m-1", Host: "a.test",
	}
	chunks := []ChunkRecord{
		{ID: "1:000000", ChunkIndex: 0, Text: "good", ChunkHash: "c-1"},
		{ID: "1:000001", ChunkIndex: 1, Text: "bad", ChunkHash: "c-2"},
	}
	if _, _, err := UpsertDocumentWithChunks(context.Background(), database, doc, chunks, ""); err != nil {
		t.Fatalf("seed document: %v", err)
	}
	seedVectorEntry(t, database, "1:000000", []float32{1, 0, 0, 0})

	// A dimension that does not match the blob length is how a corrupt row looks to
	// decodeFloat32Vector.
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO embeddings(chunk_id, name, dimension, vector, updated_at) VALUES(?, ?, ?, ?, ?)`,
		"1:000001", testVectorName, 4, encodeFloat32Vector([]float32{1, 0, 0}), "2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("insert corrupt embedding: %v", err)
	}

	hits, err := SearchVector(context.Background(), database, []float32{1, 0, 0, 0}, vectorFilters())
	if err != nil {
		t.Fatalf("search vector: %v", err)
	}
	if len(hits) != 1 || hits[0].ChunkID != "1:000000" {
		t.Fatalf("hits = %+v, want only the decodable chunk", hits)
	}
}

func TestSearchVectorEmptyResult(t *testing.T) {
	database := newTestDatabase(t)

	hits, err := SearchVector(context.Background(), database, []float32{1, 0, 0, 0}, vectorFilters())
	if err != nil {
		t.Fatalf("search vector: %v", err)
	}
	if hits == nil || len(hits) != 0 {
		t.Fatalf("hits = %#v, want an empty non-nil slice", hits)
	}
}

func TestHydrateCandidatesDropsMissingRows(t *testing.T) {
	database := newTestDatabase(t)

	hydrated, err := hydrateCandidates(context.Background(), database, []string{"missing:000000"})
	if err != nil {
		t.Fatalf("hydrate candidates: %v", err)
	}
	if len(hydrated) != 0 {
		t.Fatalf("hydrated = %v, want nothing for a missing chunk", hydrated)
	}
}
