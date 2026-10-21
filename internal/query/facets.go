package query

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gkoos/confluence2md-indexer/internal/db"
)

const (
	// FacetSpace buckets documents by space key.
	FacetSpace = "space"
	// FacetAuthor buckets documents by creator and last modifier.
	FacetAuthor = "author"
)

// FacetDimensions lists the dimensions a request may ask for, in documentation order.
var FacetDimensions = []string{FacetSpace, FacetAuthor}

// Facets holds per-bucket document counts, keyed by dimension and then by bucket value.
// It is nil unless the request asked for facets, so a default response stays unchanged.
type Facets map[string]map[string]int

// FacetBucket is one counted value of a dimension, for output that needs a stable order.
type FacetBucket struct {
	Bucket string `json:"bucket"`
	Count  int    `json:"count"`
}

// ParseFacets turns a comma separated list into the dimensions to count. An empty list
// means "no facets", which is the default and leaves ranking and output unchanged.
func ParseFacets(list string) ([]string, error) {
	dimensions := make([]string, 0, len(FacetDimensions))
	seen := make(map[string]bool, len(FacetDimensions))

	for _, raw := range strings.Split(list, ",") {
		dimension := strings.ToLower(strings.TrimSpace(raw))
		if dimension == "" {
			continue
		}
		if !isFacetDimension(dimension) {
			return nil, fmt.Errorf("unknown facet %q (available: %s)", dimension, strings.Join(FacetDimensions, ", "))
		}
		if seen[dimension] {
			continue
		}

		seen[dimension] = true
		dimensions = append(dimensions, dimension)
	}

	return dimensions, nil
}

func isFacetDimension(dimension string) bool {
	for _, available := range FacetDimensions {
		if dimension == available {
			return true
		}
	}

	return false
}

// buildFacets counts the documents behind each requested dimension. The counts describe
// the ranked set the caller would page through, not the page that was returned, so they
// are built before truncation and never feed back into ranking.
//
// A page counts once per dimension even when several of its chunks matched, and an empty
// value (a page the crawler reported no space or author for) adds no bucket. Author
// buckets follow the --author filter: a page counts for its creator and for its last
// modifier, so filtering by an author and faceting on authors agree.
func buildFacets(dimensions []string, results []Result, candidates map[string]db.Candidate) Facets {
	if len(dimensions) == 0 {
		return nil
	}

	facets := make(Facets, len(dimensions))
	for _, dimension := range dimensions {
		counts := map[string]int{}
		spellings := map[string]string{}
		counted := make(map[string]bool, len(results))

		for _, result := range results {
			for _, value := range facetValues(dimension, result, candidates) {
				key := strings.ToLower(value)
				documentKey := result.DocumentID + "\x00" + key
				if counted[documentKey] {
					continue
				}
				counted[documentKey] = true

				spelling, ok := spellings[key]
				if !ok {
					spelling = value
					spellings[key] = value
				}
				counts[spelling]++
			}
		}

		if len(counts) > 0 {
			facets[dimension] = counts
		}
	}

	return facets
}

// facetValues returns the bucket values one result contributes to a dimension. Space is
// a single stored column; author can be two, because a page usually has a creator and a
// different last modifier.
func facetValues(dimension string, result Result, candidates map[string]db.Candidate) []string {
	switch dimension {
	case FacetSpace:
		if space := strings.TrimSpace(result.SpaceKey); space != "" {
			return []string{space}
		}
	case FacetAuthor:
		candidate, ok := candidates[result.ChunkID]
		if !ok {
			return nil
		}

		created := strings.TrimSpace(candidate.CreatedByName)
		modified := strings.TrimSpace(candidate.ModifiedByName)
		values := make([]string, 0, 2)
		if created != "" {
			values = append(values, created)
		}
		if modified != "" && !strings.EqualFold(modified, created) {
			values = append(values, modified)
		}

		return values
	}

	return nil
}

// SortedBuckets returns one dimension's buckets ordered by count descending and then by
// value, which keeps text output stable. JSON output is a map, and encoding/json already
// sorts its keys.
func (f Facets) SortedBuckets(dimension string) []FacetBucket {
	counts := f[dimension]
	buckets := make([]FacetBucket, 0, len(counts))
	for value, count := range counts {
		buckets = append(buckets, FacetBucket{Bucket: value, Count: count})
	}

	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].Count == buckets[j].Count {
			return buckets[i].Bucket < buckets[j].Bucket
		}

		return buckets[i].Count > buckets[j].Count
	})

	return buckets
}
