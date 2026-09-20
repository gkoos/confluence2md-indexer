package db

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"
)

func TestBuildMatchExpression(t *testing.T) {
	cases := map[string]struct {
		query string
		want  string
	}{
		"single term is quoted":        {"apple", `"apple"`},
		"terms are combined with or":   {"apple banana", `"apple" OR "banana"`},
		"quoted phrase stays a phrase": {`"apple banana"`, `"apple banana"`},
		"trailing star is a prefix":    {"appl*", `"appl"*`},
		"hyphen becomes a phrase":      {"release-train", `"release train"`},
		"punctuation is dropped":       {"c++", `"c"`},
		"colon cannot filter a column": {"apple:banana", `"apple banana"`},
		"reserved words are terms":     {"apple OR", `"apple" OR "OR"`},
		"unbalanced quote is a term":   {`"apple`, `"apple"`},
		"whitespace only":              {"   ", ""},
		"punctuation only":             {"!!! ???", ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := BuildMatchExpression(tc.query); got != tc.want {
				t.Fatalf("BuildMatchExpression(%q) = %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}

func TestBuildMatchExpressionBoundsTermCount(t *testing.T) {
	words := make([]string, 0, 40)
	for i := range 40 {
		words = append(words, "term"+strconv.Itoa(i))
	}

	expression := BuildMatchExpression(strings.Join(words, " "))
	if got := strings.Count(expression, " OR ") + 1; got != maxMatchTerms {
		t.Fatalf("term count = %d, want %d", got, maxMatchTerms)
	}
}

func TestSearchLexicalAcceptsPunctuationAndReservedWords(t *testing.T) {
	database := newTestDatabase(t)
	ctx := context.Background()

	seedSearchDocument(t, database, DocumentRecord{
		ID:             "1",
		PageID:         "1",
		Title:          "Release train",
		LocalPath:      "a.md",
		SpaceKey:       "ENG",
		SourceURL:      "https://example.test/1",
		LastModifiedAt: "2026-01-10T00:00:00Z",
		ContentHash:    "h1",
	}, ChunkRecord{
		ID:         "1:000000",
		ChunkIndex: 0,
		Text:       "apple banana ordering",
		ChunkHash:  "c1",
		Section:    "Deployment > Rollback",
	})

	// Every one of these used to reach SQLite as FTS5 syntax and end in an error
	// such as `no such column: banana` or a parse failure.
	for _, query := range []string{"apple-banana", "apple:", "c++ query", "apple OR", "NEAR(apple banana)"} {
		if _, err := SearchLexical(ctx, database, query, SearchFilters{Candidate: 5}); err != nil {
			t.Fatalf("SearchLexical(%q): %v", query, err)
		}
	}

	// Space keys used to be indexed, so searching for one returned every page in
	// that space whatever its text said.
	hits, err := SearchLexical(ctx, database, "ENG", SearchFilters{Candidate: 5})
	if err != nil {
		t.Fatalf("SearchLexical(ENG): %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("a space key matched %d chunks, want 0", len(hits))
	}

	// The heading breadcrumb is indexed, so a section title finds its chunks.
	sectionHits, err := SearchLexical(ctx, database, "rollback", SearchFilters{Candidate: 5})
	if err != nil {
		t.Fatalf("SearchLexical(rollback): %v", err)
	}
	if len(sectionHits) != 1 {
		t.Fatalf("a section match returned %d chunks, want 1", len(sectionHits))
	}
}

func TestSearchLexicalRanksTitleMatchesFirst(t *testing.T) {
	database := newTestDatabase(t)
	ctx := context.Background()

	// The first document carries the query in its title, the second only in its
	// body text; the title match has to come first.
	seedSearchDocument(t, database, DocumentRecord{
		ID: "1", PageID: "1", Title: "Rollback runbook", LocalPath: "a.md", SpaceKey: "ENG",
		SourceURL: "https://example.test/1", LastModifiedAt: "2026-01-10T00:00:00Z", ContentHash: "h1",
	}, ChunkRecord{ID: "1:000000", ChunkIndex: 0, Text: "steps to undo a deployment", ChunkHash: "c1"})

	seedSearchDocument(t, database, DocumentRecord{
		ID: "2", PageID: "2", Title: "Deployment notes", LocalPath: "b.md", SpaceKey: "ENG",
		SourceURL: "https://example.test/2", LastModifiedAt: "2026-01-11T00:00:00Z", ContentHash: "h2",
	}, ChunkRecord{ID: "2:000000", ChunkIndex: 0, Text: "how to perform a rollback", ChunkHash: "c2"})

	hits, err := SearchLexical(ctx, database, "rollback", SearchFilters{Candidate: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].DocumentID != "1" {
		t.Fatalf("first hit = %s, want the title match 1", hits[0].DocumentID)
	}
}

// seedSearchDocument stores one document with one chunk and fails the test when the
// write does not succeed.
func seedSearchDocument(t *testing.T, database *sql.DB, doc DocumentRecord, chunk ChunkRecord) {
	t.Helper()

	if _, _, err := UpsertDocumentWithChunks(context.Background(), database, doc, []ChunkRecord{chunk}, ""); err != nil {
		t.Fatalf("seed document %s: %v", doc.ID, err)
	}
}
