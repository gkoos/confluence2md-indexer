package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadReadsSourceWeights(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
query:
  source_weights:
    - id: Engineering
      weight: 1.5
    - id: archive
      weight: 0.25
`))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	got, err := cfg.File.Query.SourceWeightMap()
	if err != nil {
		t.Fatalf("weight map: %v", err)
	}
	want := map[string]float64{"Engineering": 1.5, "archive": 0.25}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("weights = %v, want %v (ids keep their case)", got, want)
	}
}

func TestLoadRejectsBadSourceWeights(t *testing.T) {
	cases := map[string]struct {
		yaml string
		want string
	}{
		"zero": {
			"query:\n  source_weights:\n    - id: a\n      weight: 0\n",
			"query.source_weights",
		},
		"missing weight": {
			"query:\n  source_weights:\n    - id: a\n",
			"query.source_weights",
		},
		"negative": {
			"query:\n  source_weights:\n    - id: a\n      weight: -2\n",
			"positive",
		},
		"duplicate id": {
			"query:\n  source_weights:\n    - id: a\n      weight: 1\n    - id: a\n      weight: 2\n",
			"more than once",
		},
		"missing id": {
			"query:\n  source_weights:\n    - weight: 2\n",
			"no source id",
		},
		"misspelled key": {
			"query:\n  source_weights:\n    - id: a\n      weigth: 2\n",
			"",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.yaml))
			if err == nil {
				t.Fatal("expected an error")
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestSourceWeightMapOfNothingIsNil(t *testing.T) {
	var none *Query
	if got, err := none.SourceWeightMap(); got != nil || err != nil {
		t.Fatalf("nil query: %v, %v", got, err)
	}
	if got, err := (&Query{}).SourceWeightMap(); got != nil || err != nil {
		t.Fatalf("empty query: %v, %v", got, err)
	}
}
