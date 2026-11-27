package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/indexer"
	"github.com/gkoos/confluence2md-indexer/internal/query"
)

func writeNotesFolder(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create folder: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func TestFilesAndCrawlerSourcesShareOneIndex(t *testing.T) {
	ctx := context.Background()
	crawled := writeSourceFolder(t, "shared banana from the crawl")
	notes := writeNotesFolder(t, map[string]string{
		"ideas/one.md": "# Ideas\n\nshared cherry from my notes",
		"todo.txt":     "shared melon on the list",
	})
	dbPath := filepath.Join(t.TempDir(), "index.db")

	resp, err := Index(ctx, IndexRequest{
		Sources: []indexer.Source{{ID: "wiki", Folder: crawled}, {ID: "notes", Folder: notes}},
		DBPath:  dbPath,
	})
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if resp.Inserted != 3 || resp.PageCount != 3 {
		t.Fatalf("inserted = %d, pages = %d, want 3 and 3", resp.Inserted, resp.PageCount)
	}
	if resp.Sources[0].Type != indexer.TypeConfluence || resp.Sources[1].Type != indexer.TypeFiles {
		t.Fatalf("types were not detected: %+v", resp.Sources)
	}

	all, err := Query(ctx, dbPath, query.Request{Text: "shared", Mode: "lexical", TopK: 10, CandidateK: 10})
	if err != nil || len(all.Results) != 3 {
		t.Fatalf("query returned %+v, %v, want the three documents", all, err)
	}
	types := map[string]string{}
	for _, result := range all.Results {
		types[result.DocumentID] = result.SourceType
	}
	if types["wiki/1"] != indexer.TypeConfluence || types["notes/ideas/one.md"] != indexer.TypeFiles || types["notes/todo.txt"] != indexer.TypeFiles {
		t.Fatalf("source types by document = %v", types)
	}

	cherry, err := Query(ctx, dbPath, query.Request{Text: "cherry", Mode: "lexical", TopK: 5, CandidateK: 10})
	if err != nil || len(cherry.Results) != 1 || cherry.Results[0].LocalPath != "ideas/one.md" || cherry.Results[0].Title != "Ideas" {
		t.Fatalf("a notes file must answer with its relative path and title: %+v, %v", cherry, err)
	}
}

func TestFilesSourceFollowsEditsAndDeletions(t *testing.T) {
	ctx := context.Background()
	notes := writeNotesFolder(t, map[string]string{"a.md": "# A\n\nbanana", "b.md": "# B\n\ncherry"})
	dbPath := filepath.Join(t.TempDir(), "index.db")
	sources := []indexer.Source{{ID: "notes", Folder: notes}}

	if _, err := Index(ctx, IndexRequest{Sources: sources, DBPath: dbPath}); err != nil {
		t.Fatalf("index: %v", err)
	}

	again, err := Index(ctx, IndexRequest{Sources: sources, DBPath: dbPath})
	if err != nil || again.Skipped != 2 || again.Inserted != 0 || again.Updated != 0 {
		t.Fatalf("an unchanged folder must change nothing: %+v, %v", again, err)
	}

	if err := os.WriteFile(filepath.Join(notes, "a.md"), []byte("# A\n\nmelon now"), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if err := os.Remove(filepath.Join(notes, "b.md")); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := os.WriteFile(filepath.Join(notes, "c.md"), []byte("# C\n\nplum"), 0o644); err != nil {
		t.Fatalf("add: %v", err)
	}

	resp, err := Index(ctx, IndexRequest{Sources: sources, DBPath: dbPath})
	if err != nil {
		t.Fatalf("re-index: %v", err)
	}
	if resp.Updated != 1 || resp.Inserted != 1 || resp.Deleted != 1 {
		t.Fatalf("updated = %d, inserted = %d, deleted = %d, want 1, 1, 1", resp.Updated, resp.Inserted, resp.Deleted)
	}

	for text, want := range map[string]int{"banana": 0, "cherry": 0, "melon": 1, "plum": 1} {
		found, err := Query(ctx, dbPath, query.Request{Text: text, Mode: "lexical", TopK: 5, CandidateK: 10})
		if err != nil || len(found.Results) != want {
			t.Fatalf("query %q returned %d results (%v), want %d", text, len(found.Results), err, want)
		}
	}
}

func TestFilesSourceDoesNotHideTheCrawlSuccessTime(t *testing.T) {
	ctx := context.Background()
	crawled := writeSourceFolder(t, "text")
	notes := writeNotesFolder(t, map[string]string{"a.md": "# A\n\nbody"})
	dbPath := filepath.Join(t.TempDir(), "index.db")

	metadata := `{"last_completed_crawl_completed_at": "2026-01-01T10:05:00Z", "last_successful_crawl_completed_at": "2026-01-01T10:05:00Z", "pages": {"1": {"local_path": "a.md", "title": "Page", "space_key": "ENG", "source_url": "https://example.test/1"}}}`
	if err := os.WriteFile(filepath.Join(crawled, "metadata.json"), []byte(metadata), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	resp, err := Index(ctx, IndexRequest{
		Sources: []indexer.Source{{ID: "wiki", Folder: crawled}, {ID: "notes", Folder: notes}},
		DBPath:  dbPath,
	})
	if err != nil {
		t.Fatalf("index: %v", err)
	}

	if resp.Corpus.SucceededAt != "2026-01-01T10:05:00Z" || resp.Corpus.CompletedAt != "2026-01-01T10:05:00Z" {
		t.Fatalf("the files source dragged the crawl times: %+v", resp.Corpus)
	}
	if resp.Corpus.PageCount != 2 {
		t.Fatalf("snapshot page count = %d, want every document counted", resp.Corpus.PageCount)
	}
}

func TestIndexRejectsAnEmptyFilesSource(t *testing.T) {
	_, err := Index(context.Background(), IndexRequest{
		Sources: []indexer.Source{{ID: "notes", Type: indexer.TypeFiles, Folder: t.TempDir()}},
		DBPath:  filepath.Join(t.TempDir(), "index.db"),
	})
	if err == nil {
		t.Fatal("an empty folder must not index as nothing")
	}
}
