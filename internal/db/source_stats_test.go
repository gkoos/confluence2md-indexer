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

	got, err := SourceStats(ctx, database)
	if err != nil {
		t.Fatalf("SourceStats: %v", err)
	}
	want := []SourceStat{
		{ID: "a", Type: "confluence", Documents: 2, Chunks: 2},
		{ID: "b", Type: "confluence", Documents: 1, Chunks: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sources = %+v, want %+v", got, want)
	}

	stats, err := GetStats(ctx, database)
	if err != nil || !reflect.DeepEqual(stats.Sources, want) {
		t.Fatalf("GetStats sources = %+v, err = %v, want %+v", stats.Sources, err, want)
	}
}
