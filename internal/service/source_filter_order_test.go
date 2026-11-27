package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/indexer"
	"github.com/gkoos/confluence2md-indexer/internal/query"
)

// TestSourceFilterRunsBeforeCandidatesAreTruncated gives the excluded source the
// stronger match for every channel and keeps one candidate per channel. A filter applied
// after truncation would see only the excluded source's chunk and return nothing.
func TestSourceFilterRunsBeforeCandidatesAreTruncated(t *testing.T) {
	ctx := context.Background()
	strong := writeSourceFolder(t, "banana banana banana banana banana")
	weak := writeSourceFolder(t, "banana apple pear plum grape cherry melon")
	dbPath := filepath.Join(t.TempDir(), "index.db")

	if _, err := Index(ctx, IndexRequest{
		Sources: []indexer.Source{{ID: "strong", Folder: strong}, {ID: "weak", Folder: weak}},
		DBPath:  dbPath,
	}); err != nil {
		t.Fatalf("index: %v", err)
	}

	for _, mode := range []string{"lexical", "vector", "hybrid"} {
		t.Run(mode, func(t *testing.T) {
			base := query.Request{Text: "banana", Mode: mode, TopK: 5, CandidateK: 1}

			// The setup must put the excluded source first, or the test proves nothing.
			unfiltered, err := Query(ctx, dbPath, base)
			if err != nil {
				t.Fatalf("unfiltered query: %v", err)
			}
			if len(unfiltered.Results) != 1 || unfiltered.Results[0].SourceID != "strong" {
				t.Fatalf("unfiltered top result = %+v, want the strong source alone", unfiltered.Results)
			}

			filtered := base
			filtered.Filters = db.SearchFilters{Sources: []string{"weak"}}
			resp, err := Query(ctx, dbPath, filtered)
			if err != nil {
				t.Fatalf("filtered query: %v", err)
			}
			if len(resp.Results) != 1 {
				t.Fatalf("filtered query returned %d results, want the weak source's chunk", len(resp.Results))
			}

			got := resp.Results[0]
			if got.SourceID != "weak" || got.SourceType != indexer.TypeConfluence {
				t.Fatalf("source = %q (%q), want weak (%s)", got.SourceID, got.SourceType, indexer.TypeConfluence)
			}
		})
	}
}
