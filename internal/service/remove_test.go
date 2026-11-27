package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/indexer"
	"github.com/gkoos/confluence2md-indexer/internal/query"
)

func TestRemoveSourceDropsOnlyThatSource(t *testing.T) {
	ctx := context.Background()
	first := writeSourceFolder(t, "banana text")
	second := writeSourceFolder(t, "cherry text")
	dbPath := filepath.Join(t.TempDir(), "index.db")
	sources := []indexer.Source{{ID: "first", Folder: first}, {ID: "second", Folder: second}}

	if _, err := Index(ctx, IndexRequest{Sources: sources, DBPath: dbPath}); err != nil {
		t.Fatalf("index: %v", err)
	}

	// The folder is not read, so the source can go after its files have.
	if err := os.RemoveAll(first); err != nil {
		t.Fatalf("remove folder: %v", err)
	}

	resp, err := RemoveSource(ctx, dbPath, "first")
	if err != nil {
		t.Fatalf("remove source: %v", err)
	}
	if resp.Deleted != 1 || resp.RemainingDocuments != 1 {
		t.Fatalf("deleted = %d, remaining = %d, want 1 and 1", resp.Deleted, resp.RemainingDocuments)
	}

	gone, err := Query(ctx, dbPath, query.Request{Text: "banana", Mode: "lexical", TopK: 5, CandidateK: 10})
	if err != nil || len(gone.Results) != 0 {
		t.Fatalf("the removed source still answers: %+v, %v", gone, err)
	}
	kept, err := Query(ctx, dbPath, query.Request{Text: "cherry", Mode: "lexical", TopK: 5, CandidateK: 10})
	if err != nil || len(kept.Results) != 1 || kept.Results[0].SourceID != "second" {
		t.Fatalf("the other source must still answer: %+v, %v", kept, err)
	}

	// Indexing the source again brings it back, once.
	first = writeSourceFolder(t, "banana text")
	if _, err := Index(ctx, IndexRequest{Sources: []indexer.Source{{ID: "first", Folder: first}, sources[1]}, DBPath: dbPath}); err != nil {
		t.Fatalf("re-index: %v", err)
	}
	back, err := Query(ctx, dbPath, query.Request{Text: "banana", Mode: "lexical", TopK: 5, CandidateK: 10})
	if err != nil || len(back.Results) != 1 {
		t.Fatalf("re-added source returned %+v, %v, want exactly one hit", back, err)
	}
}

func TestRemoveSourceRejectsUnknownSourcesAndNamesTheKnownOnes(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	folder := writeSourceFolder(t, "text")
	if _, err := Index(ctx, IndexRequest{Sources: []indexer.Source{{ID: "known", Folder: folder}}, DBPath: dbPath}); err != nil {
		t.Fatalf("index: %v", err)
	}

	_, err := RemoveSource(ctx, dbPath, "typo")
	if err == nil || !strings.Contains(err.Error(), `"typo"`) || !strings.Contains(err.Error(), "known") {
		t.Fatalf("err = %v, want it to name the typo and the known source", err)
	}

	counts := mustSourceCounts(t, dbPath)
	if counts["known"] != 1 {
		t.Fatalf("a rejected removal must change nothing: %v", counts)
	}
}

func TestRemoveSourceDoesNotCreateAMissingIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "missing.db")

	if _, err := RemoveSource(context.Background(), dbPath, "any"); err == nil {
		t.Fatal("expected an error for a missing index")
	}
	if _, err := os.Stat(dbPath); err == nil {
		t.Fatal("RemoveSource created an empty index file")
	}
}

func TestRemoveSourceValidatesInput(t *testing.T) {
	if _, err := RemoveSource(context.Background(), "", "a"); err == nil {
		t.Fatal("an empty db path must be rejected")
	}
	if _, err := RemoveSource(context.Background(), "x.db", "  "); err == nil {
		t.Fatal("an empty source id must be rejected")
	}
}

func mustSourceCounts(t *testing.T, dbPath string) map[string]int {
	t.Helper()

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = database.Close() }()

	counts, err := db.SourceDocumentCounts(context.Background(), database)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	return counts
}
