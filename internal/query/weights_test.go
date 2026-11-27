package query

import (
	"math"
	"strings"
	"testing"
)

func TestValidateSourceWeights(t *testing.T) {
	if err := ValidateSourceWeights(nil); err != nil {
		t.Fatalf("no weights must be valid: %v", err)
	}
	if err := ValidateSourceWeights(map[string]float64{"a": 0.25, "b": 2, "c": 1}); err != nil {
		t.Fatalf("positive weights must be valid: %v", err)
	}

	for name, weights := range map[string]map[string]float64{
		"zero":       {"a": 0},
		"negative":   {"a": -0.5},
		"NaN":        {"a": math.NaN()},
		"infinite":   {"a": math.Inf(1)},
		"blank id":   {" ": 1},
		"one of two": {"ok": 1, "bad": 0},
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateSourceWeights(weights)
			if err == nil || !strings.Contains(err.Error(), "source") {
				t.Fatalf("err = %v, want a source weight error", err)
			}
		})
	}
}
