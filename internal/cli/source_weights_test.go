package cli

import (
	"strings"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/config"
)

func TestResolveSourceWeightsMergesTheFlagOverTheConfig(t *testing.T) {
	fileQuery := &config.Query{SourceWeights: []config.SourceWeight{{ID: "docs", Weight: 1.5}, {ID: "wiki", Weight: 0.5}}}

	got, err := resolveSourceWeights(fileQuery, []string{"wiki=2", "extra=3"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	want := map[string]float64{"docs": 1.5, "wiki": 2, "extra": 3}
	if len(got) != len(want) {
		t.Fatalf("weights = %v, want %v", got, want)
	}
	for id, weight := range want {
		if got[id] != weight {
			t.Fatalf("weights = %v, want %v", got, want)
		}
	}
}

func TestResolveSourceWeightsWithNothingConfiguredIsNil(t *testing.T) {
	if got, err := resolveSourceWeights(nil, nil); got != nil || err != nil {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestResolveSourceWeightsRejectsBadFlags(t *testing.T) {
	cases := map[string]struct {
		spec string
		want string
	}{
		"no separator":   {"docs", "id=weight"},
		"not a number":   {"docs=heavy", "not a number"},
		"zero":           {"docs=0", "positive"},
		"negative":       {"docs=-1", "positive"},
		"not finite":     {"docs=NaN", "positive"},
		"missing source": {"=2", "no source id"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := resolveSourceWeights(nil, []string{tc.spec})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one containing %q", err, tc.want)
			}
		})
	}
}
