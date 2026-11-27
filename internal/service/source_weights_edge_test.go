package service

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/gkoos/confluence2md-indexer/internal/query"
)

func TestLargestAllowedWeightStaysFiniteInEveryMode(t *testing.T) {
	dbPath := indexStrongAndWeak(t)

	for name, base := range map[string]query.Request{
		"lexical":      {Text: "banana", Mode: "lexical", TopK: 5, CandidateK: 10},
		"vector":       {Text: "banana", Mode: "vector", TopK: 5, CandidateK: 10},
		"hybrid":       {Text: "banana", Mode: "hybrid", TopK: 5, CandidateK: 10},
		"hybrid + rrf": {Text: "banana", Mode: "hybrid", Fusion: "rrf", TopK: 5, CandidateK: 10},
	} {
		t.Run(name, func(t *testing.T) {
			for _, weight := range []float64{query.MaxSourceWeight, query.MinSourceWeight} {
				req := base
				req.SourceWeights = map[string]float64{"weak": weight}

				_, results := topSource(t, dbPath, req)
				for _, result := range results {
					for _, score := range []float64{result.Lexical, result.Vector, result.Fused} {
						if math.IsNaN(score) || math.IsInf(score, 0) {
							t.Fatalf("weight %g produced a non-finite score: %+v", weight, result)
						}
					}
				}
				if _, err := json.Marshal(results); err != nil {
					t.Fatalf("weight %g: results cannot be marshalled: %v", weight, err)
				}
			}
		})
	}
}

func TestQueryRejectsWeightsOutsideTheSupportedRange(t *testing.T) {
	dbPath := indexStrongAndWeak(t)

	// A weight of 1e308 on a raw score of 4 overflows to infinity, and normalising that
	// gives NaN; the range is what keeps it from reaching the ranking.
	for _, weight := range []float64{1e308, query.MaxSourceWeight * 10, query.MinSourceWeight / 10} {
		_, err := Query(t.Context(), dbPath, query.Request{
			Text: "banana", Mode: "lexical", TopK: 5, CandidateK: 10,
			SourceWeights: map[string]float64{"weak": weight},
		})
		if err == nil {
			t.Fatalf("weight %g must be rejected", weight)
		}
	}
}

func TestRankFusionWeightScalesTheMetadataBoostWithTheScore(t *testing.T) {
	dbPath := indexStrongAndWeak(t)

	priors, err := query.ParsePriors("recency")
	if err != nil {
		t.Fatalf("parse priors: %v", err)
	}
	base := query.Request{
		Text: "banana", Mode: "hybrid", Fusion: "rrf", TopK: 5, CandidateK: 10,
		Priors: query.PriorConfig{Priors: priors, Strength: 0.5, Now: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	}

	const weight = 2.0
	byChunk := func(req query.Request) map[string]query.Result {
		_, results := topSource(t, dbPath, req)
		out := map[string]query.Result{}
		for _, result := range results {
			out[result.ChunkID] = result
		}
		return out
	}

	plain := byChunk(base)
	weighted := base
	weighted.SourceWeights = map[string]float64{"weak": weight}
	got := byChunk(weighted)

	checked := false
	for chunkID, result := range got {
		unweighted := plain[chunkID]
		if result.SourceID != "weak" {
			continue
		}
		if unweighted.MetadataBoost == 0 {
			t.Fatal("the recency prior added nothing, so this test would prove nothing")
		}

		if math.Abs(result.Fused-unweighted.Fused*weight) > 1e-12 || math.Abs(result.MetadataBoost-unweighted.MetadataBoost*weight) > 1e-12 {
			t.Fatalf("fused = %v and boost = %v, want the unweighted %v and %v both times %v",
				result.Fused, result.MetadataBoost, unweighted.Fused, unweighted.MetadataBoost, weight)
		}
		checked = true
	}
	if !checked {
		t.Fatal("the weighted source never appeared in the results")
	}
}
