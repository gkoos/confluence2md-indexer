package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/indexer"
	"github.com/gkoos/confluence2md-indexer/internal/query"
)

// indexTwoSources builds one index from two sources whose pages both mention "shared".
func indexTwoSources(t *testing.T) string {
	t.Helper()

	first := writeSourceFolder(t, "shared banana text")
	second := writeSourceFolder(t, "shared cherry text")
	dbPath := filepath.Join(t.TempDir(), "index.db")

	_, err := Index(context.Background(), IndexRequest{
		Sources: []indexer.Source{{ID: "first", Folder: first}, {ID: "second", Folder: second}},
		DBPath:  dbPath,
	})
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	return dbPath
}

func TestQueryFiltersBySource(t *testing.T) {
	dbPath := indexTwoSources(t)
	ctx := context.Background()

	all, err := Query(ctx, dbPath, query.Request{Text: "shared", Mode: "lexical", TopK: 10, CandidateK: 10})
	if err != nil || len(all.Results) != 2 {
		t.Fatalf("unfiltered query returned %+v, %v, want 2 results", all, err)
	}

	for _, tc := range []struct {
		name    string
		sources []string
		want    []string
	}{
		{"one source", []string{"second"}, []string{"second"}},
		{"several sources", []string{"first", "second"}, []string{"first", "second"}},
		{"blank values are ignored", []string{" ", "first"}, []string{"first"}},
		{"unknown source matches nothing", []string{"nope"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := Query(ctx, dbPath, query.Request{
				Text: "shared", Mode: "lexical", TopK: 10, CandidateK: 10,
				Filters: db.SearchFilters{Sources: tc.sources},
			})
			if err != nil {
				t.Fatalf("query: %v", err)
			}

			got := map[string]bool{}
			for _, result := range resp.Results {
				got[result.SourceID] = true
				if result.SourceType != indexer.TypeConfluence {
					t.Fatalf("source type = %q, want %q", result.SourceType, indexer.TypeConfluence)
				}
			}
			if len(got) != len(tc.want) {
				t.Fatalf("sources in results = %v, want %v", got, tc.want)
			}
			for _, source := range tc.want {
				if !got[source] {
					t.Fatalf("sources in results = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestQueryCountsDocumentsPerSource(t *testing.T) {
	dbPath := indexTwoSources(t)

	resp, err := Query(context.Background(), dbPath, query.Request{
		Text: "shared", Mode: "lexical", TopK: 10, CandidateK: 10,
		Facets: []string{query.FacetSource},
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	counts := resp.Facets[query.FacetSource]
	if counts["first"] != 1 || counts["second"] != 1 || len(counts) != 2 {
		t.Fatalf("source facet = %v, want one document each", counts)
	}
}

func TestParseFacetsAcceptsSource(t *testing.T) {
	dimensions, err := query.ParseFacets("space, source")
	if err != nil || len(dimensions) != 2 || dimensions[1] != query.FacetSource {
		t.Fatalf("dimensions = %v, err = %v", dimensions, err)
	}
}
