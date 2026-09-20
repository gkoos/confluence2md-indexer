package query

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gkoos/confluence2md-indexer/internal/db"
)

func TestParsePriors(t *testing.T) {
	cases := map[string]struct {
		list string
		want []Prior
	}{
		"empty means off":  {"", []Prior{}},
		"single":           {"recency", []Prior{PriorRecency}},
		"list":             {"recency,seed", []Prior{PriorRecency, PriorSeed}},
		"spacing and case": {" Recency , AUTHORITY ", []Prior{PriorRecency, PriorAuthority}},
		"duplicates":       {"seed,seed", []Prior{PriorSeed}},
		"blank entries":    {",recency,,", []Prior{PriorRecency}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParsePriors(tc.list)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.list, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parse %q = %v, want %v", tc.list, got, tc.want)
			}
		})
	}

	_, err := ParsePriors("freshness")
	if err == nil || !strings.Contains(err.Error(), "available") {
		t.Fatalf("error = %v, want the unknown prior to list what is available", err)
	}
}

func TestPriorValues(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	halfLife := 180 * 24 * time.Hour

	cases := map[string]struct {
		prior     Prior
		candidate db.Candidate
		want      float64
	}{
		"recency of a page modified now":  {PriorRecency, db.Candidate{LastModifiedAt: "2026-09-20T12:00:00Z"}, 1},
		"recency after one half-life":     {PriorRecency, db.Candidate{LastModifiedAt: "2026-03-24T12:00:00Z"}, 0.5},
		"recency of a future timestamp":   {PriorRecency, db.Candidate{LastModifiedAt: "2027-01-01T00:00:00Z"}, 1},
		"recency without a timestamp":     {PriorRecency, db.Candidate{}, 0},
		"recency with a broken timestamp": {PriorRecency, db.Candidate{LastModifiedAt: "yesterday"}, 0},
		"authority without links":         {PriorAuthority, db.Candidate{LinkIn: 0}, 0},
		"authority at the cap":            {PriorAuthority, db.Candidate{LinkIn: authorityLinkCap}, 1},
		"authority beyond the cap":        {PriorAuthority, db.Candidate{LinkIn: 400}, 1},
		"seed page":                       {PriorSeed, db.Candidate{IsSeed: true}, 1},
		"not a seed page":                 {PriorSeed, db.Candidate{}, 0},
		"depth zero":                      {PriorDepth, db.Candidate{Depth: 0}, 1},
		"depth two":                       {PriorDepth, db.Candidate{Depth: 2}, 1.0 / 3.0},
		"richness at the cap":             {PriorRichness, db.Candidate{Attachments: 3, Comments: 2}, 1},
		"richness below the cap":          {PriorRichness, db.Candidate{Attachments: 2}, 0.4},
		"richness without extras":         {PriorRichness, db.Candidate{}, 0},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			config := PriorConfig{Priors: []Prior{tc.prior}, RecencyHalfLife: halfLife, Now: now}
			got := config.Values(tc.candidate)[string(tc.prior)]
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("value = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPriorConfigResolvesDefaultsAndClampsStrength(t *testing.T) {
	resolved := PriorConfig{Priors: []Prior{PriorSeed}, Strength: 2}.Resolved()

	if resolved.Strength != 1 {
		t.Fatalf("strength = %v, want it clamped to 1", resolved.Strength)
	}
	if resolved.RecencyHalfLife != DefaultRecencyHalfLife {
		t.Fatalf("half-life = %v, want the default", resolved.RecencyHalfLife)
	}
	if resolved.Now.IsZero() {
		t.Fatal("Now must be filled in, so recency is computable")
	}

	if got := (PriorConfig{Priors: []Prior{PriorSeed}, Strength: -1}).Resolved().Strength; got != 0 {
		t.Fatalf("strength = %v, want it clamped to 0", got)
	}
}

func TestParseAge(t *testing.T) {
	cases := map[string]time.Duration{
		"":      0,
		"30d":   30 * 24 * time.Hour,
		"2w":    14 * 24 * time.Hour,
		"1 day": 24 * time.Hour,
		"12h":   12 * time.Hour,
		"90m":   90 * time.Minute,
	}

	for value, want := range cases {
		t.Run("age "+value, func(t *testing.T) {
			got, err := ParseAge(value)
			if err != nil {
				t.Fatalf("ParseAge(%q): %v", value, err)
			}
			if got != want {
				t.Fatalf("ParseAge(%q) = %v, want %v", value, got, want)
			}
		})
	}

	if _, err := ParseAge("soon"); err == nil {
		t.Fatal("expected an error for an unparsable age")
	}
}

func TestApplyPriorsIsBoundedMultiplicativeAndExplained(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	config := PriorConfig{
		Priors:          []Prior{PriorSeed, PriorRecency},
		Strength:        0.2,
		RecencyHalfLife: 180 * 24 * time.Hour,
		Now:             now,
	}

	combined := map[string]*Result{"1:000000": {ChunkID: "1:000000", Fused: 0.5}}
	candidates := map[string]db.Candidate{
		"1:000000": {ChunkID: "1:000000", IsSeed: true, LastModifiedAt: "2026-03-24T12:00:00Z"},
	}

	applyPriors(config, combined, candidates)

	result := combined["1:000000"]
	// mean(seed=1, recency=0.5) = 0.75, so the score grows by 0.5*0.2*0.75 = 0.075.
	if math.Abs(result.Fused-0.575) > 1e-9 {
		t.Fatalf("fused = %v, want 0.575", result.Fused)
	}
	if math.Abs(result.MetadataBoost-0.075) > 1e-9 {
		t.Fatalf("boost = %v, want 0.075", result.MetadataBoost)
	}
	if result.MetadataBoost > result.Fused*0.2 {
		t.Fatalf("boost = %v, want at most 20%% of the fused score", result.MetadataBoost)
	}
	want := map[string]float64{"seed": 1, "recency": 0.5}
	if !reflect.DeepEqual(result.MetadataFactors, want) {
		t.Fatalf("factors = %v, want %v", result.MetadataFactors, want)
	}
}

func TestApplyPriorsSkipsUnknownCandidates(t *testing.T) {
	combined := map[string]*Result{"1:000000": {ChunkID: "1:000000", Fused: 0.5}}
	applyPriors(PriorConfig{Priors: []Prior{PriorSeed}}, combined, map[string]db.Candidate{})

	if got := combined["1:000000"].Fused; got != 0.5 {
		t.Fatalf("fused = %v, want the score untouched", got)
	}
}

func TestFuseReordersWithinThePriorBound(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	candidates := []db.Candidate{
		{ChunkID: "a", LexicalScoreRaw: 10, LastModifiedAt: "2020-01-01T00:00:00Z"},
		{ChunkID: "b", LexicalScoreRaw: 10, LastModifiedAt: "2026-09-19T12:00:00Z"},
	}

	plain := fuse(Request{Mode: "lexical"}, candidates, nil)
	if plain[0].ChunkID != "a" {
		t.Fatalf("without priors the chunk id breaks the tie, got %s first", plain[0].ChunkID)
	}

	boosted := fuse(Request{
		Mode: "lexical",
		Priors: PriorConfig{
			Priors:          []Prior{PriorRecency},
			Strength:        0.15,
			RecencyHalfLife: 180 * 24 * time.Hour,
			Now:             now,
		},
	}, candidates, nil)
	if boosted[0].ChunkID != "b" {
		t.Fatalf("with the recency prior the fresh page must lead, got %s first", boosted[0].ChunkID)
	}
	// Recency decays asymptotically, so a very old page keeps a sliver of signal: what
	// matters is that it gains far less than a fresh one.
	if boosted[1].MetadataBoost >= boosted[0].MetadataBoost {
		t.Fatalf("stale boost %v must be smaller than fresh boost %v", boosted[1].MetadataBoost, boosted[0].MetadataBoost)
	}
	if boosted[1].MetadataBoost > boosted[0].MetadataBoost*0.2 {
		t.Fatalf("stale boost %v must be a fraction of the fresh boost %v", boosted[1].MetadataBoost, boosted[0].MetadataBoost)
	}
}

func TestFuseWithoutPriorsLeavesScoresUntouched(t *testing.T) {
	candidates := []db.Candidate{
		{ChunkID: "a", LexicalScoreRaw: 10, IsSeed: true, Depth: 0},
		{ChunkID: "b", LexicalScoreRaw: 4, Depth: 3},
	}

	plain := fuse(Request{Mode: "lexical"}, candidates, nil)
	withEmptyConfig := fuse(Request{Mode: "lexical", Priors: PriorConfig{}}, candidates, nil)

	if !reflect.DeepEqual(plain, withEmptyConfig) {
		t.Fatalf("an empty prior config changed the results:\n%+v\n%+v", plain, withEmptyConfig)
	}
	for _, result := range plain {
		if result.MetadataBoost != 0 || result.MetadataFactors != nil {
			t.Fatalf("result %s carries prior fields while priors are off: %+v", result.ChunkID, result)
		}
	}
}

func TestFuseAppliesPriorsDeterministically(t *testing.T) {
	request := Request{
		Mode:   "lexical",
		Priors: PriorConfig{Priors: []Prior{PriorSeed, PriorDepth, PriorRichness}, Strength: 0.15},
	}
	candidates := []db.Candidate{
		{ChunkID: "a", LexicalScoreRaw: 8, Depth: 2, Attachments: 1},
		{ChunkID: "b", LexicalScoreRaw: 8, Depth: 1, Comments: 4},
		{ChunkID: "c", LexicalScoreRaw: 8, IsSeed: true, Depth: 0},
	}

	first := fuse(request, candidates, nil)
	for i := range 5 {
		again := fuse(request, candidates, nil)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("run %d differed:\n%+v\n%+v", i, first, again)
		}
	}

	if first[0].ChunkID != "c" {
		t.Fatalf("the seed page at depth zero must lead, got %s", first[0].ChunkID)
	}
}
