package query

import (
	"fmt"
	"math"
	"strings"

	"github.com/gkoos/confluence2md-indexer/internal/db"
)

// ValidateSourceWeights rejects weights that cannot scale a score. A weight is a
// multiplier on the fused score, so it has to be a positive, finite number; excluding a
// source is what the --source filter is for, and a zero weight would only hide results
// without saying so. A source id the index does not hold is accepted, because the same
// weights can be configured for several indexes.
func ValidateSourceWeights(weights map[string]float64) error {
	for id, weight := range weights {
		switch {
		case strings.TrimSpace(id) == "":
			return fmt.Errorf("a source weight has no source id")
		case math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0:
			return fmt.Errorf("weight of source %q must be a positive number, got %v (use the source filter to exclude a source)", id, weight)
		}
	}

	return nil
}

// usesRRF reports whether the request fuses the two channels by rank, which is the one
// case where the raw scores play no part in the result.
func usesRRF(req Request) bool {
	return req.Mode != "lexical" && req.Mode != "vector" && req.Fusion == "rrf"
}

// scaleRawScores multiplies the raw channel score of every candidate from a weighted
// source, and returns the candidates unchanged when no weight applies.
//
// The weight goes on the raw score, before normalisation, and not on the fused score
// afterwards. Normalisation maps the weakest real match to exactly zero, and a multiplier
// cannot lift a zero: weighting the fused score would let a weight only strengthen sources
// that already rank well. Scaling the raw score makes a weight mean what it says, that
// this source's relevance counts that many times over, so it can move a result past one
// from another source. Raw scores that reach this point are positive, because withSignal
// has dropped the rest, so a positive weight always scales them the same way.
func scaleRawScores(candidates []db.Candidate, weights map[string]float64, scale func(*db.Candidate, float64)) []db.Candidate {
	if len(weights) == 0 {
		return candidates
	}

	scaled := make([]db.Candidate, len(candidates))
	copy(scaled, candidates)
	for i := range scaled {
		if weight, ok := weights[scaled[i].SourceID]; ok && weight != 1 {
			scale(&scaled[i], weight)
		}
	}

	return scaled
}

// weightRankedScores scales fused reciprocal-rank scores, which come from ranks and not
// from the raw scores, by the weight of their source. Those scores are positive and do
// not pass through min-max normalisation, so a multiplier is enough.
func weightRankedScores(weights map[string]float64, combined map[string]*Result, candidates map[string]db.Candidate) {
	for chunkID, result := range combined {
		candidate, ok := candidates[chunkID]
		if !ok {
			continue
		}
		if weight, ok := weights[candidate.SourceID]; ok && weight != 1 {
			result.Fused *= weight
		}
	}
}
