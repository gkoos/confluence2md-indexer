package query

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/embedding"
	"github.com/gkoos/confluence2md-indexer/internal/embedding/embeddingtest"
)

// queryModes lists the retrieval modes every result-shaping behaviour must cover.
var queryModes = []string{"lexical", "vector", "hybrid"}

// runInModes runs one request per retrieval mode, so a result-shaping change cannot
// work in the mode it was written for and silently break the others. Each mode gets
// its own subtest, so a failure names the mode, and every mode runs twice because a
// shaped result set has to be reproducible row for row.
func runInModes(t *testing.T, database *sql.DB, request Request, check func(t *testing.T, results []Result, total int)) {
	t.Helper()

	for _, mode := range queryModes {
		t.Run(mode, func(t *testing.T) {
			request.Mode = mode

			results, total, err := Run(context.Background(), database, embeddingtest.New(8), request)
			if err != nil {
				t.Fatalf("run %s query: %v", mode, err)
			}

			again, againTotal, err := Run(context.Background(), database, embeddingtest.New(8), request)
			if err != nil {
				t.Fatalf("run %s query again: %v", mode, err)
			}
			if againTotal != total || !reflect.DeepEqual(again, results) {
				t.Fatalf("%s results are not deterministic: %+v then %+v", mode, results, again)
			}

			check(t, results, total)
		})
	}
}

func TestRunHybridWeightedPrefersLexicalWithHighAlpha(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	results, total, err := Run(context.Background(), database, embeddingtest.New(8), Request{
		Text:       "banana",
		Mode:       "hybrid",
		Fusion:     "weighted",
		Alpha:      0.95,
		TopK:       5,
		CandidateK: 10,
	})
	if err != nil {
		t.Fatalf("run query: %v", err)
	}
	if total == 0 {
		t.Fatalf("expected total > 0")
	}
	if len(results) == 0 {
		t.Fatalf("expected non-empty results")
	}
	if results[0].ChunkID != "p2:000000" {
		t.Fatalf("expected lexical top result p2:000000 got %s", results[0].ChunkID)
	}
}

func TestRunHybridRRFCombinesBothChannels(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	results, _, err := Run(context.Background(), database, embeddingtest.New(8), Request{
		Text:       "banana",
		Mode:       "hybrid",
		Fusion:     "rrf",
		RRFK:       60,
		TopK:       5,
		CandidateK: 10,
	})
	if err != nil {
		t.Fatalf("run query: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results got %d", len(results))
	}
	if results[0].Fusion != "rrf" {
		t.Fatalf("expected rrf fusion got %s", results[0].Fusion)
	}
	if results[0].Fused < results[1].Fused {
		t.Fatalf("expected sorted fused scores desc")
	}
}

func TestRunTopKLimit(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	results, _, err := Run(context.Background(), database, embeddingtest.New(8), Request{
		Text:       "banana",
		Mode:       "lexical",
		TopK:       1,
		CandidateK: 10,
	})
	if err != nil {
		t.Fatalf("run query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected top-k to limit to 1 result, got %d", len(results))
	}
}

func TestRunIncludesCanonicalURLOnlyWhenStored(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	results, _, err := Run(context.Background(), database, embeddingtest.New(8), Request{
		Text:       "banana",
		Mode:       "lexical",
		TopK:       1,
		CandidateK: 10,
	})
	if err != nil {
		t.Fatalf("run query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result got %d", len(results))
	}
	if results[0].CanonicalURL != "https://example.test/pages/p2" {
		t.Fatalf("expected the stored canonical url, got %q", results[0].CanonicalURL)
	}

	// A page the crawler reported no canonical url for must not invent one from
	// sourceUrl; the field is additive and absent.
	results, _, err = Run(context.Background(), database, embeddingtest.New(8), Request{
		Text:       "apple",
		Mode:       "lexical",
		TopK:       1,
		CandidateK: 10,
	})
	if err != nil {
		t.Fatalf("run query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result got %d", len(results))
	}
	if results[0].CanonicalURL != "" {
		t.Fatalf("expected no canonical url, got %q", results[0].CanonicalURL)
	}
	if results[0].SourceURL == "" {
		t.Fatalf("expected sourceUrl to stay populated")
	}
}

func TestRunParentFilterKeepsDirectChildren(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	// p2 is a child of p1 and p3 is a child of p2, so the filter must stop at the
	// direct children. The query text is p2's own chunk, which keeps the vector
	// channel's arbitrary stub scores out of the assertion.
	runInModes(t, database, Request{
		Text:       "banana banana yellow",
		TopK:       10,
		CandidateK: 10,
		Filters:    db.SearchFilters{ParentID: "p1"},
	}, func(t *testing.T, results []Result, _ int) {
		if len(results) != 1 {
			t.Fatalf("expected only the direct child, got %d results", len(results))
		}
		if results[0].PageID != "p2" {
			t.Fatalf("expected page p2 got %s", results[0].PageID)
		}
	})

	// A page with no children matches nothing, and the pages without a parent id
	// (p1, whose parent is empty) never match a parent filter either.
	runInModes(t, database, Request{
		Text:       "banana banana yellow",
		TopK:       10,
		CandidateK: 10,
		Filters:    db.SearchFilters{ParentID: "p3"},
	}, func(t *testing.T, results []Result, total int) {
		if len(results) != 0 || total != 0 {
			t.Fatalf("expected no results, got %d (total %d)", len(results), total)
		}
	})
}

func TestRunMaxPerPageCapsChunksPerDocument(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	// "left neighbor" is p3's first chunk, so the vector channel always scores that
	// page positively, and lexical retrieval also matches its last chunk.
	runInModes(t, database, Request{
		Text:       "left neighbor",
		TopK:       10,
		CandidateK: 10,
		MaxPerPage: 1,
	}, func(t *testing.T, results []Result, _ int) {
		seen := map[string]bool{}
		for _, result := range results {
			if seen[result.DocumentID] {
				t.Fatalf("document %s appeared more than once: %+v", result.DocumentID, results)
			}
			seen[result.DocumentID] = true
		}
		if !seen["p3"] {
			t.Fatalf("expected the multi-chunk page in the results, got %+v", results)
		}
		if !sortedByFused(results) {
			t.Fatalf("expected the capped results to stay in ranked order: %+v", results)
		}
	})
}

func TestRunMaxPerPageDefaultsToUnlimited(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	// "neighbor" matches p3's first and last chunk, so the same page owns both
	// slots unless the caller caps it.
	query := func(maxPerPage int) ([]Result, int) {
		t.Helper()

		results, total, err := Run(context.Background(), database, embeddingtest.New(8), Request{
			Text:       "neighbor",
			Mode:       "lexical",
			TopK:       10,
			CandidateK: 10,
			MaxPerPage: maxPerPage,
		})
		if err != nil {
			t.Fatalf("run query: %v", err)
		}

		return results, total
	}

	uncapped, uncappedTotal := query(0)
	if len(uncapped) != 2 || uncappedTotal != 2 {
		t.Fatalf("expected both chunks of the page by default, got %d results (total %d)", len(uncapped), uncappedTotal)
	}
	for _, result := range uncapped {
		if result.DocumentID != "p3" {
			t.Fatalf("expected only p3 chunks, got %s", result.DocumentID)
		}
	}

	// A negative value is treated like "unlimited" for callers that build the
	// request themselves; the command line rejects it.
	negative, negativeTotal := query(-1)
	if len(negative) != len(uncapped) || negativeTotal != uncappedTotal {
		t.Fatalf("expected a negative cap to be unlimited, got %d results (total %d)", len(negative), negativeTotal)
	}

	capped, cappedTotal := query(1)
	if len(capped) != 1 || cappedTotal != 1 {
		t.Fatalf("expected one chunk after the cap, got %d results (total %d)", len(capped), cappedTotal)
	}
	if capped[0].ChunkID != uncapped[0].ChunkID {
		t.Fatalf("expected the top-ranked chunk to survive the cap, got %s", capped[0].ChunkID)
	}
}

// sortedByFused reports whether the results keep the ranked order, which every
// result-shaping stage must preserve.
func sortedByFused(results []Result) bool {
	for i := 1; i < len(results); i++ {
		if results[i-1].Fused < results[i].Fused {
			return false
		}
	}

	return true
}

func TestRunGroupDocumentReturnsOneResultPerPage(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	// "left neighbor" is p3's first chunk, so the vector channel scores that page
	// positively too, and lexical retrieval matches its last chunk as well.
	runInModes(t, database, Request{
		Text:       "left neighbor",
		TopK:       10,
		CandidateK: 10,
		Group:      "document",
	}, func(t *testing.T, results []Result, _ int) {
		seen := map[string]bool{}
		for _, result := range results {
			if seen[result.DocumentID] {
				t.Fatalf("document %s appeared more than once: %+v", result.DocumentID, results)
			}
			seen[result.DocumentID] = true
			if result.ChunkCount < 1 {
				t.Fatalf("expected chunkCount >= 1 on a grouped result: %+v", result)
			}
		}
		if !seen["p3"] {
			t.Fatalf("expected the multi-chunk page in the results, got %+v", results)
		}
		if !sortedByFused(results) {
			t.Fatalf("expected the grouped results to stay in ranked order: %+v", results)
		}
	})
}

func TestRunGroupDocumentKeepsTheBestChunkAndCount(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	chunked, chunkedTotal, err := Run(context.Background(), database, embeddingtest.New(8), Request{
		Text:       "neighbor",
		Mode:       "lexical",
		TopK:       10,
		CandidateK: 10,
	})
	if err != nil {
		t.Fatalf("run chunked query: %v", err)
	}
	if chunkedTotal != 2 || len(chunked) != 2 {
		t.Fatalf("expected both chunks of the page, got %d results (total %d)", len(chunked), chunkedTotal)
	}

	grouped, groupedTotal, err := Run(context.Background(), database, embeddingtest.New(8), Request{
		Text:       "neighbor",
		Mode:       "lexical",
		TopK:       10,
		CandidateK: 10,
		Group:      "document",
	})
	if err != nil {
		t.Fatalf("run grouped query: %v", err)
	}
	if groupedTotal != 1 || len(grouped) != 1 {
		t.Fatalf("expected one document, got %d results (total %d)", len(grouped), groupedTotal)
	}
	if grouped[0].ChunkID != chunked[0].ChunkID {
		t.Fatalf("expected the best chunk %s to represent the page, got %s", chunked[0].ChunkID, grouped[0].ChunkID)
	}
	if grouped[0].ChunkCount != 2 {
		t.Fatalf("expected chunkCount 2 for the page, got %d", grouped[0].ChunkCount)
	}
	if grouped[0].Fused != chunked[0].Fused {
		t.Fatalf("expected the page score to be the best chunk score, got %.4f", grouped[0].Fused)
	}

	// A cap cannot remove a page that is already represented once, so the two
	// options compose without changing the grouped result.
	capped, cappedTotal, err := Run(context.Background(), database, embeddingtest.New(8), Request{
		Text:       "neighbor",
		Mode:       "lexical",
		TopK:       10,
		CandidateK: 10,
		Group:      "document",
		MaxPerPage: 1,
	})
	if err != nil {
		t.Fatalf("run grouped query with cap: %v", err)
	}
	if cappedTotal != groupedTotal || !reflect.DeepEqual(capped, grouped) {
		t.Fatalf("expected --max-per-page to be a no-op in group mode, got %+v", capped)
	}
}

func TestRunGroupDocumentCountsDocumentsAndPaginates(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	// "banana" hits p2 and "neighbor" hits two chunks of p3, so the grouped total
	// counts documents while the chunked total counts rows.
	results, total, err := Run(context.Background(), database, embeddingtest.New(8), Request{
		Text:       "banana neighbor",
		Mode:       "lexical",
		TopK:       10,
		CandidateK: 10,
		Group:      "document",
	})
	if err != nil {
		t.Fatalf("run grouped query: %v", err)
	}
	if total != 2 || len(results) != 2 {
		t.Fatalf("expected two documents, got %d results (total %d)", len(results), total)
	}

	documents := map[string]int{}
	for _, result := range results {
		documents[result.DocumentID] = result.ChunkCount
	}
	if documents["p2"] != 1 || documents["p3"] != 2 {
		t.Fatalf("expected chunk counts p2=1 p3=2, got %v", documents)
	}

	paged, pagedTotal, err := Run(context.Background(), database, embeddingtest.New(8), Request{
		Text:       "banana neighbor",
		Mode:       "lexical",
		TopK:       10,
		Offset:     1,
		Limit:      1,
		CandidateK: 10,
		Group:      "document",
	})
	if err != nil {
		t.Fatalf("run paged grouped query: %v", err)
	}
	if pagedTotal != 2 || len(paged) != 1 {
		t.Fatalf("expected one document at offset 1 of 2, got %d results (total %d)", len(paged), pagedTotal)
	}
	if paged[0].Rank != 2 {
		t.Fatalf("expected rank 2 after paging, got %d", paged[0].Rank)
	}

	again, _, err := Run(context.Background(), database, embeddingtest.New(8), Request{
		Text:       "banana neighbor",
		Mode:       "lexical",
		TopK:       10,
		CandidateK: 10,
		Group:      "document",
	})
	if err != nil {
		t.Fatalf("run repeated grouped query: %v", err)
	}
	if !reflect.DeepEqual(again, results) {
		t.Fatalf("expected deterministic grouping, got %+v then %+v", results, again)
	}
}

func TestRunFacetsCountTheMatchingSetNotTheReturnedPage(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	// Four chunks match: p2, two of p3's three chunks, and p4. A top-k of one returns a
	// single result, so any count that matched the returned page would be wrong.
	response, err := RunWithFacets(context.Background(), database, embeddingtest.New(8), Request{
		Text:       "banana neighbor unrelated",
		Mode:       "lexical",
		TopK:       1,
		CandidateK: 10,
		Facets:     []string{FacetSpace, FacetAuthor},
	})
	if err != nil {
		t.Fatalf("run query: %v", err)
	}
	if len(response.Results) != 1 || response.Total != 1 {
		t.Fatalf("expected one returned result, got %d (total %d)", len(response.Results), response.Total)
	}

	wantSpaces := map[string]int{"ENG": 2, "OPS": 1}
	if !reflect.DeepEqual(response.Facets[FacetSpace], wantSpaces) {
		t.Fatalf("space facets = %v, want %v", response.Facets[FacetSpace], wantSpaces)
	}

	// p2 and p3 are touched by both people; p4 only by its creator and modifier, who
	// are the same person.
	wantAuthors := map[string]int{"Ada Lovelace": 3, "Grace Hopper": 2}
	if !reflect.DeepEqual(response.Facets[FacetAuthor], wantAuthors) {
		t.Fatalf("author facets = %v, want %v", response.Facets[FacetAuthor], wantAuthors)
	}
}

func TestRunFacetsCountInEveryMode(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	for _, mode := range queryModes {
		t.Run(mode, func(t *testing.T) {
			response, err := RunWithFacets(context.Background(), database, embeddingtest.New(8), Request{
				Text:       "banana neighbor unrelated",
				Mode:       mode,
				TopK:       10,
				CandidateK: 10,
				Facets:     []string{FacetSpace},
			})
			if err != nil {
				t.Fatalf("run %s query: %v", mode, err)
			}

			documents := map[string]bool{}
			for _, result := range response.Results {
				documents[result.DocumentID] = true
			}

			// Every seeded page has a space, so the buckets have to add up to the
			// documents behind them instead of to the returned rows.
			counted := 0
			for _, count := range response.Facets[FacetSpace] {
				counted += count
			}
			if counted != len(documents) {
				t.Fatalf("space facets count %d documents, but %d were returned: %v", counted, len(documents), response.Facets)
			}
		})
	}
}

func TestRunFacetsAreOptInAndDoNotAffectRanking(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	request := Request{Text: "banana neighbor", Mode: "hybrid", TopK: 10, CandidateK: 10}
	plain, err := RunWithFacets(context.Background(), database, embeddingtest.New(8), request)
	if err != nil {
		t.Fatalf("run query without facets: %v", err)
	}
	if plain.Facets != nil {
		t.Fatalf("expected no facets unless they are requested, got %v", plain.Facets)
	}

	request.Facets = []string{FacetSpace, FacetAuthor}
	faceted, err := RunWithFacets(context.Background(), database, embeddingtest.New(8), request)
	if err != nil {
		t.Fatalf("run query with facets: %v", err)
	}
	if !reflect.DeepEqual(faceted.Results, plain.Results) || faceted.Total != plain.Total {
		t.Fatalf("expected facets to leave ranking and total alone, got %+v", faceted)
	}
}

func TestRunFacetsRejectAnUnknownDimension(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	_, err := RunWithFacets(context.Background(), database, embeddingtest.New(8), Request{
		Text:   "banana",
		Mode:   "lexical",
		Facets: []string{"label"},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown facet") {
		t.Fatalf("expected an unknown facet error, got %v", err)
	}
}

func TestRunExpandStitchesNeighborChunks(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	results, _, err := Run(context.Background(), database, embeddingtest.New(8), Request{
		Text:       "middleterm",
		Mode:       "lexical",
		TopK:       1,
		CandidateK: 10,
		Expand:     1,
	})
	if err != nil {
		t.Fatalf("run query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result got %d", len(results))
	}
	if results[0].ChunkID != "p3:000001" {
		t.Fatalf("expected center chunk p3:000001 got %s", results[0].ChunkID)
	}
	if results[0].ContextStartIndex != 0 || results[0].ContextEndIndex != 2 {
		t.Fatalf("expected context range 0..2 got %d..%d", results[0].ContextStartIndex, results[0].ContextEndIndex)
	}
	if results[0].ContextChunkCount != 3 {
		t.Fatalf("expected context chunk count 3 got %d", results[0].ContextChunkCount)
	}
	if results[0].BaseChunkText == "" {
		t.Fatalf("expected base chunk text to be preserved")
	}
	if !strings.Contains(results[0].ChunkText, "left neighbor") || !strings.Contains(results[0].ChunkText, "right neighbor") {
		t.Fatalf("expected expanded chunk text to include neighboring chunks")
	}
}

func TestRunPaginationOffsetLimit(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	results, total, err := Run(context.Background(), database, embeddingtest.New(8), Request{
		Text:       "neighbor",
		Mode:       "lexical",
		TopK:       5,
		Offset:     1,
		Limit:      1,
		CandidateK: 10,
	})
	if err != nil {
		t.Fatalf("run query: %v", err)
	}
	if total < 2 {
		t.Fatalf("expected total >= 2 got %d", total)
	}
	if len(results) != 1 {
		t.Fatalf("expected paged result length 1 got %d", len(results))
	}
	if results[0].Rank != 2 {
		t.Fatalf("expected paged rank to be global rank 2 got %d", results[0].Rank)
	}
}

func TestFuseLexicalFiltersZeroScoreTail(t *testing.T) {
	results := fuse(Request{Mode: "lexical"}, []db.Candidate{
		{ChunkID: "c-keep", LexicalScoreRaw: 10},
		{ChunkID: "c-drop", LexicalScoreRaw: 0},
	}, nil)

	if len(results) != 1 {
		t.Fatalf("expected one positive lexical result after filtering, got %d", len(results))
	}
	if results[0].ChunkID != "c-keep" {
		t.Fatalf("expected c-keep to remain, got %s", results[0].ChunkID)
	}
}

func TestFuseKeepsTheWeakestRealCandidate(t *testing.T) {
	// Min-max normalisation maps the weakest of several positive scores to exactly
	// zero, and the "no signal" filter used to drop it, which silently removed the
	// last real match of every query.
	results := fuse(Request{Mode: "lexical"}, []db.Candidate{
		{ChunkID: "c-strong", LexicalScoreRaw: 10},
		{ChunkID: "c-middle", LexicalScoreRaw: 4},
		{ChunkID: "c-weak", LexicalScoreRaw: 1},
	}, nil)

	if len(results) != 3 {
		t.Fatalf("expected three lexical results, got %d", len(results))
	}
	if results[len(results)-1].ChunkID != "c-weak" {
		t.Fatalf("expected the weakest match to rank last, got %s", results[len(results)-1].ChunkID)
	}
}

func TestFuseDropsCandidatesWithoutSimilarity(t *testing.T) {
	results := fuse(Request{Mode: "vector"}, nil, []db.Candidate{
		{ChunkID: "c-keep", VectorScoreRaw: 0.4},
		{ChunkID: "c-zero", VectorScoreRaw: 0},
		{ChunkID: "c-negative", VectorScoreRaw: -0.2},
	})

	if len(results) != 1 {
		t.Fatalf("expected one scored vector result, got %d", len(results))
	}
	if results[0].ChunkID != "c-keep" {
		t.Fatalf("expected c-keep to remain, got %s", results[0].ChunkID)
	}
}

func TestFuseKeepsBothChannelsInHybridMode(t *testing.T) {
	results := fuse(Request{Mode: "hybrid", Fusion: "weighted", Alpha: 0.5},
		[]db.Candidate{{ChunkID: "c-lex", LexicalScoreRaw: 8}},
		[]db.Candidate{
			{ChunkID: "c-vec", VectorScoreRaw: 0.9},
			{ChunkID: "c-vec-weak", VectorScoreRaw: 0.1},
		},
	)

	if len(results) != 3 {
		t.Fatalf("expected three hybrid results, got %d", len(results))
	}
}

func TestRunRejectsEmbeddingIdentityMismatch(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	// Querying with a provider whose vectors live in another space must fail with
	// a diagnostic instead of scoring zero everywhere and returning no results.
	other := embeddingtest.NewWithCaps("stub:other", 8, embedding.Caps{Semantic: true})

	_, _, err := Run(context.Background(), database, other, Request{
		Text:       "banana",
		Mode:       "hybrid",
		TopK:       5,
		CandidateK: 10,
	})
	if err == nil || !strings.Contains(err.Error(), "embedding mismatch") {
		t.Fatalf("expected an embedding mismatch error, got %v", err)
	}

	// Lexical retrieval must keep working without the vector channel.
	results, _, err := Run(context.Background(), database, other, Request{
		Text:       "banana",
		Mode:       "lexical",
		TopK:       5,
		CandidateK: 10,
	})
	if err != nil {
		t.Fatalf("lexical query failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected lexical results despite the embedding mismatch")
	}
}

func TestRunRejectsVectorModeWithoutEmbeddings(t *testing.T) {
	database := setupQueryTestDB(t)

	_, _, err := Run(context.Background(), database, embeddingtest.New(8), Request{
		Text: "banana",
		Mode: "vector",
	})
	if err == nil || !strings.Contains(err.Error(), "holds no embeddings") {
		t.Fatalf("expected a missing embeddings error, got %v", err)
	}
}

func TestRunRejectsMixedEmbeddingIdentities(t *testing.T) {
	database := setupQueryTestDB(t)
	seedQueryDocs(t, database)

	provider := embeddingtest.New(8)
	vectors, err := provider.Embed(context.Background(), embedding.KindDocument, []string{"banana"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	// Simulate an index that was written by two different providers.
	if _, err := db.UpsertEmbeddings(context.Background(), database, []db.EmbeddingRecord{{
		ChunkID:   "p2:000000",
		Name:      "stub:other",
		Dimension: len(vectors[0]),
		Vector:    vectors[0],
	}}); err != nil {
		t.Fatalf("upsert embeddings: %v", err)
	}

	_, _, err = Run(context.Background(), database, provider, Request{
		Text: "banana",
		Mode: "hybrid",
	})
	if err == nil || !strings.Contains(err.Error(), "mixes embedding identities") {
		t.Fatalf("expected a mixed identity error, got %v", err)
	}
}

func setupQueryTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "query-test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return database
}

func seedQueryDocs(t *testing.T, database *sql.DB) {
	t.Helper()

	docs := []struct {
		doc    db.DocumentRecord
		chunks []db.ChunkRecord
	}{
		{
			doc: db.DocumentRecord{
				ID:             "p1",
				PageID:         "p1",
				Title:          "Alpha",
				LocalPath:      "p1.md",
				SpaceKey:       "ENG",
				SourceURL:      "https://example.test/p1",
				LastModifiedAt: "2026-01-01T00:00:00Z",
				CreatedByName:  "Ada Lovelace",
				ModifiedByName: "Ada Lovelace",
				ContentHash:    "h1",
			},
			chunks: []db.ChunkRecord{{ID: "p1:000000", ChunkIndex: 0, Text: "apple and pear", ChunkHash: "c1"}},
		},
		{
			doc: db.DocumentRecord{
				ID:             "p2",
				PageID:         "p2",
				Title:          "Banana",
				LocalPath:      "p2.md",
				SpaceKey:       "ENG",
				SourceURL:      "https://example.test/p2",
				CanonicalURL:   "https://example.test/pages/p2",
				ParentID:       "p1",
				LastModifiedAt: "2026-01-01T00:00:00Z",
				CreatedByName:  "Grace Hopper",
				ModifiedByName: "Ada Lovelace",
				ContentHash:    "h2",
			},
			chunks: []db.ChunkRecord{{ID: "p2:000000", ChunkIndex: 0, Text: "banana banana yellow", ChunkHash: "c2"}},
		},
		{
			doc: db.DocumentRecord{
				ID:             "p3",
				PageID:         "p3",
				Title:          "Middle",
				LocalPath:      "p3.md",
				SpaceKey:       "ENG",
				SourceURL:      "https://example.test/p3",
				ParentID:       "p2",
				LastModifiedAt: "2026-01-01T00:00:00Z",
				CreatedByName:  "Ada Lovelace",
				ModifiedByName: "Grace Hopper",
				ContentHash:    "h3",
			},
			chunks: []db.ChunkRecord{
				{ID: "p3:000000", ChunkIndex: 0, Text: "left neighbor", ChunkHash: "c3a"},
				{ID: "p3:000001", ChunkIndex: 1, Text: "middleterm center", ChunkHash: "c3b"},
				{ID: "p3:000002", ChunkIndex: 2, Text: "right neighbor", ChunkHash: "c3c"},
			},
		},
		{
			// A second space with a single chunk, so facet counts have two buckets to
			// distinguish and the page does not share the first space's author pair.
			doc: db.DocumentRecord{
				ID:             "p4",
				PageID:         "p4",
				Title:          "Orphan",
				LocalPath:      "p4.md",
				SpaceKey:       "OPS",
				SourceURL:      "https://example.test/p4",
				LastModifiedAt: "2026-01-01T00:00:00Z",
				CreatedByName:  "Ada Lovelace",
				ModifiedByName: "Ada Lovelace",
				ContentHash:    "h4",
			},
			chunks: []db.ChunkRecord{{ID: "p4:000000", ChunkIndex: 0, Text: "unrelated content", ChunkHash: "c4"}},
		},
	}

	provider := embeddingtest.New(8)

	for _, item := range docs {
		if _, _, err := db.UpsertDocumentWithChunks(context.Background(), database, item.doc, item.chunks, provider.Name()); err != nil {
			t.Fatalf("upsert doc %s: %v", item.doc.ID, err)
		}
	}

	texts := []string{"apple and pear", "banana banana yellow", "left neighbor", "middleterm center", "right neighbor", "unrelated content"}
	vectors, err := provider.Embed(context.Background(), embedding.KindDocument, texts)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	_, err = db.UpsertEmbeddings(context.Background(), database, []db.EmbeddingRecord{
		{ChunkID: "p1:000000", Name: provider.Name(), Dimension: len(vectors[0]), Vector: vectors[0]},
		{ChunkID: "p2:000000", Name: provider.Name(), Dimension: len(vectors[1]), Vector: vectors[1]},
		{ChunkID: "p3:000000", Name: provider.Name(), Dimension: len(vectors[2]), Vector: vectors[2]},
		{ChunkID: "p3:000001", Name: provider.Name(), Dimension: len(vectors[3]), Vector: vectors[3]},
		{ChunkID: "p3:000002", Name: provider.Name(), Dimension: len(vectors[4]), Vector: vectors[4]},
		{ChunkID: "p4:000000", Name: provider.Name(), Dimension: len(vectors[5]), Vector: vectors[5]},
	})
	if err != nil {
		t.Fatalf("upsert embeddings: %v", err)
	}
}
