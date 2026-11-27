package db

import (
	"context"
	"errors"
	"testing"
)

func seedSourceDoc(t *testing.T, id, source, text string) (DocumentRecord, ChunkRecord) {
	t.Helper()

	return DocumentRecord{
			ID: id, PageID: "1", SourceID: source, SourceType: "confluence", Title: "Page", LocalPath: id + ".md",
			SourceURL: "https://example.test/" + id, LastModifiedAt: "2026-01-10T00:00:00Z", ContentHash: "h-" + id,
		}, ChunkRecord{
			ID: id + ":000000", ChunkIndex: 0, Text: text, ChunkHash: "c-" + id,
		}
}

func TestRemoveSourceDocumentsRemovesOnlyThatSource(t *testing.T) {
	database := newTestDatabase(t)
	ctx := context.Background()

	for _, seed := range []struct{ id, source, text string }{
		{"a/1", "a", "banana from a"},
		{"a/2", "a", "banana again from a"},
		{"b/1", "b", "banana from b"},
	} {
		doc, chunk := seedSourceDoc(t, seed.id, seed.source, seed.text)
		seedSearchDocument(t, database, doc, chunk)
	}

	removal, err := RemoveSourceDocuments(ctx, database, "a")
	if err != nil || removal.Deleted != 2 || removal.Remaining != 1 {
		t.Fatalf("removal = %+v, err = %v, want 2 deleted and 1 remaining", removal, err)
	}

	counts, err := SourceDocumentCounts(ctx, database)
	if err != nil || len(counts) != 1 || counts["b"] != 1 {
		t.Fatalf("counts = %v, err = %v, want only b with one document", counts, err)
	}

	// Chunks cascade, and the full-text rows, which no foreign key covers, are gone too.
	var chunks, fts int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&chunks); err != nil || chunks != 1 {
		t.Fatalf("chunks = %d, err = %v, want 1", chunks, err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks_fts`).Scan(&fts); err != nil || fts != 1 {
		t.Fatalf("full-text rows = %d, err = %v, want 1", fts, err)
	}
}

func TestDeletedDocumentsDoNotDuplicateTheirHitsWhenIndexedAgain(t *testing.T) {
	database := newTestDatabase(t)
	ctx := context.Background()

	doc, chunk := seedSourceDoc(t, "a/1", "a", "banana text")
	seedSearchDocument(t, database, doc, chunk)

	for name, remove := range map[string]func() error{
		"by source": func() error { _, err := RemoveSourceDocuments(ctx, database, "a"); return err },
		"not in":    func() error { _, err := DeleteDocumentsNotIn(ctx, database, []string{"other"}); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := remove(); err != nil {
				t.Fatalf("delete: %v", err)
			}

			// The same chunk id comes back, as it does when a removed source is indexed again.
			seedSearchDocument(t, database, doc, chunk)

			hits, err := SearchLexical(ctx, database, "banana", SearchFilters{Candidate: 10})
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(hits) != 1 {
				t.Fatalf("got %d lexical hits for one chunk, want 1 (orphaned full-text rows duplicate it)", len(hits))
			}
		})
	}
}

func TestRemoveSourceDocumentsRejectsAnEmptyID(t *testing.T) {
	if _, err := RemoveSourceDocuments(context.Background(), newTestDatabase(t), "  "); err == nil {
		t.Fatal("an empty source id must be rejected, not treated as every document")
	}
}

func TestRemoveSourceDocumentsReportsUnknownSourcesAndChangesNothing(t *testing.T) {
	database := newTestDatabase(t)
	ctx := context.Background()

	doc, chunk := seedSourceDoc(t, "a/1", "a", "banana text")
	seedSearchDocument(t, database, doc, chunk)

	removal, err := RemoveSourceDocuments(ctx, database, "typo")
	if !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("err = %v, want ErrSourceNotFound", err)
	}
	if removal.Known["a"] != 1 || len(removal.Known) != 1 {
		t.Fatalf("known sources = %v, want a with one document", removal.Known)
	}

	counts, err := SourceDocumentCounts(ctx, database)
	if err != nil || counts["a"] != 1 {
		t.Fatalf("a rejected removal must change nothing: %v, %v", counts, err)
	}
}
