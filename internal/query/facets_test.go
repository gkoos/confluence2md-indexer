package query

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/db"
)

func TestParseFacetsNormalizesAndRejectsUnknown(t *testing.T) {
	dimensions, err := ParseFacets(" Space , author ,space")
	if err != nil {
		t.Fatalf("parse facets: %v", err)
	}
	if want := []string{FacetSpace, FacetAuthor}; !reflect.DeepEqual(dimensions, want) {
		t.Fatalf("dimensions = %v, want %v", dimensions, want)
	}

	if dimensions, err := ParseFacets("  "); err != nil || len(dimensions) != 0 {
		t.Fatalf("expected an empty list, got %v (err %v)", dimensions, err)
	}

	if _, err := ParseFacets("space,label"); err == nil || !strings.Contains(err.Error(), "unknown facet") {
		t.Fatalf("expected an unknown facet error, got %v", err)
	}
}

func TestBuildFacetsCountsDocumentsOncePerDimension(t *testing.T) {
	results := []Result{
		{DocumentID: "d1", ChunkID: "d1:0", SpaceKey: "ENG"},
		{DocumentID: "d1", ChunkID: "d1:1", SpaceKey: "ENG"},
		{DocumentID: "d2", ChunkID: "d2:0", SpaceKey: "ENG"},
		{DocumentID: "d3", ChunkID: "d3:0", SpaceKey: ""},
	}
	candidates := map[string]db.Candidate{
		// Two chunks of one page count once, and the same person in both roles
		// counts once even when the crawler cased the names differently.
		"d1:0": {CreatedByName: "Ada Lovelace", ModifiedByName: "ada lovelace"},
		"d1:1": {CreatedByName: "Ada Lovelace", ModifiedByName: "ada lovelace"},
		// A creator and a different last modifier are two buckets, which is what
		// --author matches.
		"d2:0": {CreatedByName: "Grace Hopper", ModifiedByName: "Ada Lovelace"},
		// A page with no author metadata contributes no author bucket.
		"d3:0": {},
	}

	facets := buildFacets([]string{FacetSpace, FacetAuthor}, results, candidates)

	wantSpaces := map[string]int{"ENG": 2}
	if !reflect.DeepEqual(facets[FacetSpace], wantSpaces) {
		t.Fatalf("space facets = %v, want %v (a page without a space adds no bucket)", facets[FacetSpace], wantSpaces)
	}

	wantAuthors := map[string]int{"Ada Lovelace": 2, "Grace Hopper": 1}
	if !reflect.DeepEqual(facets[FacetAuthor], wantAuthors) {
		t.Fatalf("author facets = %v, want %v", facets[FacetAuthor], wantAuthors)
	}

	if facets := buildFacets(nil, results, candidates); facets != nil {
		t.Fatalf("expected no facets when nothing was requested, got %v", facets)
	}
}

func TestBuildFacetsKeepsSpaceCaseExact(t *testing.T) {
	// --space filters with case-sensitive equality, so merging ENG and eng would
	// report a count that filtering by either spelling cannot reproduce.
	results := []Result{
		{DocumentID: "d1", ChunkID: "d1:0", SpaceKey: "ENG"},
		{DocumentID: "d2", ChunkID: "d2:0", SpaceKey: "eng"},
	}
	candidates := map[string]db.Candidate{
		"d1:0": {CreatedByName: "Ada Lovelace", ModifiedByName: "Ada Lovelace"},
		"d2:0": {CreatedByName: "ada lovelace", ModifiedByName: "ada lovelace"},
	}

	facets := buildFacets([]string{FacetSpace, FacetAuthor}, results, candidates)

	wantSpaces := map[string]int{"ENG": 1, "eng": 1}
	if !reflect.DeepEqual(facets[FacetSpace], wantSpaces) {
		t.Fatalf("space facets = %v, want %v", facets[FacetSpace], wantSpaces)
	}

	// Author filtering is case-insensitive, so those buckets still fold together
	// under the first spelling seen.
	wantAuthors := map[string]int{"Ada Lovelace": 2}
	if !reflect.DeepEqual(facets[FacetAuthor], wantAuthors) {
		t.Fatalf("author facets = %v, want %v", facets[FacetAuthor], wantAuthors)
	}
}

func TestFacetsSortedBucketsOrdersByCountThenValue(t *testing.T) {
	facets := Facets{FacetSpace: map[string]int{"OPS": 1, "ENG": 2, "SRE": 2}}

	want := []FacetBucket{
		{Bucket: "ENG", Count: 2},
		{Bucket: "SRE", Count: 2},
		{Bucket: "OPS", Count: 1},
	}
	if got := facets.SortedBuckets(FacetSpace); !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted buckets = %v, want %v", got, want)
	}
}
