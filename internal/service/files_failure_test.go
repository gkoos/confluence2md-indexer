package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/indexer"
	"github.com/gkoos/confluence2md-indexer/internal/query"
)

// TestFilesSourceThatBecomesUnindexableKeepsItsStoredDocuments covers a folder whose files
// all turn empty or non-text after an index run: reading it finds no documents, and the
// run must fail instead of treating every stored document as deleted.
func TestFilesSourceThatBecomesUnindexableKeepsItsStoredDocuments(t *testing.T) {
	ctx := context.Background()
	notes := writeNotesFolder(t, map[string]string{"a.md": "# A\n\nbanana", "b.md": "# B\n\ncherry"})
	dbPath := filepath.Join(t.TempDir(), "index.db")
	sources := []indexer.Source{{ID: "notes", Folder: notes}}

	if _, err := Index(ctx, IndexRequest{Sources: sources, DBPath: dbPath}); err != nil {
		t.Fatalf("index: %v", err)
	}

	for _, name := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(notes, name), []byte("\x00binary now"), 0o644); err != nil {
			t.Fatalf("overwrite %s: %v", name, err)
		}
	}

	if _, err := Index(ctx, IndexRequest{Sources: sources, DBPath: dbPath}); err == nil {
		t.Fatal("a source with nothing indexable must fail the run")
	}

	for _, text := range []string{"banana", "cherry"} {
		found, err := Query(ctx, dbPath, query.Request{Text: text, Mode: "lexical", TopK: 5, CandidateK: 10})
		if err != nil || len(found.Results) != 1 {
			t.Fatalf("query %q returned %d results (%v): the stored document must survive a failed run", text, len(found.Results), err)
		}
	}
}
