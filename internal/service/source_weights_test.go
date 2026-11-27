package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/indexer"
	"github.com/gkoos/confluence2md-indexer/internal/query"
)

// indexStrongAndWeak builds an index where the "strong" source is the better text match
// for "banana", so it ranks first until a weight says otherwise.
func indexStrongAndWeak(t *testing.T) string {
	t.Helper()

	strong := writeSourceFolder(t, "banana banana banana banana banana")
	weak := writeSourceFolder(t, "banana apple pear plum grape cherry melon")
	dbPath := filepath.Join(t.TempDir(), "index.db")

	if _, err := Index(context.Background(), IndexRequest{
		Sources: []indexer.Source{{ID: "strong", Folder: strong}, {ID: "weak", Folder: weak}},
		DBPath:  dbPath,
	}); err != nil {
		t.Fatalf("index: %v", err)
	}
	return dbPath
}

func topSource(t *testing.T, dbPath string, req query.Request) (query.Result, []query.Result) {
	t.Helper()

	resp, err := Query(context.Background(), dbPath, req)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Fatal("query returned nothing")
	}
	return resp.Results[0], resp.Results
}

func TestSourceWeightsReorderResultsInEveryMode(t *testing.T) {
	dbPath := indexStrongAndWeak(t)

	for name, base := range map[string]query.Request{
		"lexical":      {Text: "banana", Mode: "lexical", TopK: 5, CandidateK: 10},
		"vector":       {Text: "banana", Mode: "vector", TopK: 5, CandidateK: 10},
		"hybrid":       {Text: "banana", Mode: "hybrid", TopK: 5, CandidateK: 10},
		"hybrid + rrf": {Text: "banana", Mode: "hybrid", Fusion: "rrf", TopK: 5, CandidateK: 10},
	} {
		t.Run(name, func(t *testing.T) {
			if top, _ := topSource(t, dbPath, base); top.SourceID != "strong" {
				t.Fatalf("without weights the top source = %q, want strong", top.SourceID)
			}

			weighted := base
			weighted.SourceWeights = map[string]float64{"weak": 100}
			top, all := topSource(t, dbPath, weighted)
			if top.SourceID != "weak" || top.SourceWeight != 100 {
				t.Fatalf("with a heavy weight the top result = %q (weight %v), want weak (100)", top.SourceID, top.SourceWeight)
			}
			if len(all) != 2 {
				t.Fatalf("a weight must not drop results: got %d, want 2", len(all))
			}
		})
	}
}

func TestWeightOfOneAndUnknownSourcesChangeNothing(t *testing.T) {
	dbPath := indexStrongAndWeak(t)

	for _, mode := range []string{"lexical", "hybrid"} {
		t.Run(mode, func(t *testing.T) {
			base := query.Request{Text: "banana", Mode: mode, TopK: 5, CandidateK: 10}
			_, plain := topSource(t, dbPath, base)

			neutral := base
			neutral.SourceWeights = map[string]float64{"strong": 1, "unknown-source": 9}
			_, got := topSource(t, dbPath, neutral)

			if len(got) != len(plain) {
				t.Fatalf("got %d results, want %d", len(got), len(plain))
			}
			for i := range plain {
				if got[i].ChunkID != plain[i].ChunkID || got[i].Fused != plain[i].Fused || got[i].SourceWeight != 0 {
					t.Fatalf("result %d = %+v, want the unweighted %+v with no reported weight", i, got[i], plain[i])
				}
			}
		})
	}
}

func TestAWeightBelowOneDemotesButKeepsTheResult(t *testing.T) {
	dbPath := indexStrongAndWeak(t)

	weighted := query.Request{
		Text: "banana", Mode: "lexical", TopK: 5, CandidateK: 10,
		SourceWeights: map[string]float64{"strong": 0.001},
	}
	top, all := topSource(t, dbPath, weighted)

	if top.SourceID != "weak" || len(all) != 2 {
		t.Fatalf("top = %q of %d results, want weak first and both kept", top.SourceID, len(all))
	}
	if all[1].SourceID != "strong" || all[1].SourceWeight != 0.001 {
		t.Fatalf("demoted result = %+v, want strong with its weight reported", all[1])
	}
}
func TestSourceWeightsComposeWithTheSourceFilter(t *testing.T) {
	dbPath := indexStrongAndWeak(t)

	_, results := topSource(t, dbPath, query.Request{
		Text: "banana", Mode: "lexical", TopK: 5, CandidateK: 10,
		SourceWeights: map[string]float64{"weak": 3},
		Filters:       db.SearchFilters{Sources: []string{"weak"}},
	})
	if len(results) != 1 || results[0].SourceID != "weak" {
		t.Fatalf("results = %+v, want the weak source alone", results)
	}
}

func TestQueryRejectsWeightsThatCannotScaleAScore(t *testing.T) {
	dbPath := indexStrongAndWeak(t)

	for name, weights := range map[string]map[string]float64{
		"zero":     {"weak": 0},
		"negative": {"weak": -1},
		"blank id": {"  ": 2},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Query(context.Background(), dbPath, query.Request{
				Text: "banana", Mode: "lexical", TopK: 5, CandidateK: 10, SourceWeights: weights,
			})
			if err == nil || !strings.Contains(err.Error(), "weight") {
				t.Fatalf("err = %v, want a weight error", err)
			}
		})
	}
}
