package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/indexer"
	"github.com/gkoos/confluence2md-indexer/internal/query"
)

// writeSourceFolder writes a crawler output folder with one page whose key is "1",
// the same key in every folder, so a collision between sources would show.
func writeSourceFolder(t *testing.T, text string) string {
	t.Helper()
	dir := t.TempDir()

	metadata := `{"pages": {"1": {"local_path": "a.md", "title": "Page", "space_key": "ENG", "last_modified_at": "2026-01-15T12:00:00Z", "source_url": "https://example.test/1"}}}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# heading\n\n"+text), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	return dir
}

func TestSourcesWithTheSamePageKeyDoNotCollide(t *testing.T) {
	ctx := context.Background()
	first := writeSourceFolder(t, "banana text")
	second := writeSourceFolder(t, "cherry text")
	dbPath := filepath.Join(t.TempDir(), "index.db")

	resp, err := Index(ctx, IndexRequest{
		Sources: []indexer.Source{{ID: "first", Folder: first}, {ID: "second", Folder: second}},
		DBPath:  dbPath,
	})
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if resp.Inserted != 2 || resp.DBStats.Documents != 2 {
		t.Fatalf("inserted = %d, documents = %d, want 2 and 2", resp.Inserted, resp.DBStats.Documents)
	}
	if len(resp.Sources) != 2 || resp.PageCount != 2 {
		t.Fatalf("per-source results missing: %+v (pages %d)", resp.Sources, resp.PageCount)
	}

	for text, wantDocument := range map[string]string{"banana": "first/1", "cherry": "second/1"} {
		found, err := Query(ctx, dbPath, query.Request{Text: text, Mode: "lexical", TopK: 5, CandidateK: 10})
		if err != nil {
			t.Fatalf("query %q: %v", text, err)
		}
		if len(found.Results) != 1 || found.Results[0].DocumentID != wantDocument {
			t.Fatalf("query %q returned %+v, want one result for %s", text, found.Results, wantDocument)
		}
		if found.Results[0].PageID != "1" {
			t.Fatalf("page id = %q, want the source's own key 1", found.Results[0].PageID)
		}
	}
}

func TestDroppingASourceRemovesOnlyItsDocuments(t *testing.T) {
	ctx := context.Background()
	first := writeSourceFolder(t, "banana text")
	second := writeSourceFolder(t, "cherry text")
	dbPath := filepath.Join(t.TempDir(), "index.db")
	both := []indexer.Source{{ID: "first", Folder: first}, {ID: "second", Folder: second}}

	if _, err := Index(ctx, IndexRequest{Sources: both, DBPath: dbPath}); err != nil {
		t.Fatalf("index both: %v", err)
	}

	resp, err := Index(ctx, IndexRequest{Sources: both[:1], DBPath: dbPath})
	if err != nil {
		t.Fatalf("index one: %v", err)
	}
	if resp.Deleted != 1 || resp.Skipped != 1 || resp.DBStats.Documents != 1 {
		t.Fatalf("deleted = %d, skipped = %d, documents = %d, want 1, 1, 1", resp.Deleted, resp.Skipped, resp.DBStats.Documents)
	}

	found, err := Query(ctx, dbPath, query.Request{Text: "banana", Mode: "lexical", TopK: 5, CandidateK: 10})
	if err != nil || len(found.Results) != 1 {
		t.Fatalf("the remaining source must still answer: %+v, %v", found, err)
	}
}

func TestIndexRejectsInvalidSources(t *testing.T) {
	dir := writeSourceFolder(t, "text")

	for name, sources := range map[string][]indexer.Source{
		"duplicate ids": {{ID: "a", Folder: dir}, {ID: "a", Folder: dir}},
		"bad id":        {{ID: "a/b", Folder: dir}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Index(context.Background(), IndexRequest{Sources: sources, DBPath: filepath.Join(t.TempDir(), "x.db")})
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestIndexNamesTheSourceThatFailsPreflight(t *testing.T) {
	good := writeSourceFolder(t, "text")
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	_, err := Index(context.Background(), IndexRequest{
		Sources: []indexer.Source{{ID: "good", Folder: good}, {ID: "broken", Folder: missing}},
		DBPath:  filepath.Join(t.TempDir(), "x.db"),
	})
	if err == nil || !strings.Contains(err.Error(), `source "broken"`) {
		t.Fatalf("error = %v, want it to name the broken source", err)
	}
}
