package db

import (
	"context"
	"reflect"
	"testing"
)

func TestSourceStatsCountsDocumentsAndChunksPerSource(t *testing.T) {
	database := newTestDatabase(t)
	ctx := context.Background()

	empty, err := SourceStats(ctx, database)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty index = %v, err = %v, want an empty non-nil list", empty, err)
	}

	for _, seed := range []struct{ id, source, text string }{
		{"b/1", "b", "banana from b"},
		{"a/1", "a", "banana from a"},
		{"a/2", "a", "banana again from a"},
	} {
		doc, chunk := seedSourceDoc(t, seed.id, seed.source, seed.text)
		seedSearchDocument(t, database, doc, chunk)
	}

	// One document with three chunks and one with none keep the two counters independent:
	// COUNT(d.id) would inflate documents, and an inner join would drop the chunkless one.
	multi, first := seedSourceDoc(t, "a/3", "a", "banana one")
	chunks := []ChunkRecord{
		first,
		{ID: "a/3:000001", ChunkIndex: 1, Text: "banana two", ChunkHash: "c-a/3-1"},
		{ID: "a/3:000002", ChunkIndex: 2, Text: "banana three", ChunkHash: "c-a/3-2"},
	}
	if _, _, err := UpsertDocumentWithChunks(ctx, database, multi, chunks, ""); err != nil {
		t.Fatalf("seed multi-chunk document: %v", err)
	}
	chunkless, _ := seedSourceDoc(t, "b/2", "b", "")
	if _, _, err := UpsertDocumentWithChunks(ctx, database, chunkless, nil, ""); err != nil {
		t.Fatalf("seed chunkless document: %v", err)
	}

	got, err := SourceStats(ctx, database)
	if err != nil {
		t.Fatalf("SourceStats: %v", err)
	}
	want := []SourceStat{
		{ID: "a", Type: "confluence", Documents: 3, Chunks: 5},
		{ID: "b", Type: "confluence", Documents: 2, Chunks: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sources = %+v, want %+v", got, want)
	}

	stats, err := GetStats(ctx, database)
	if err != nil || !reflect.DeepEqual(stats.Sources, want) {
		t.Fatalf("GetStats sources = %+v, err = %v, want %+v", stats.Sources, err, want)
	}
}
