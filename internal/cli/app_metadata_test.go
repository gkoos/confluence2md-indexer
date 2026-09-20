package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// writeCrawlerMetadata writes a one-page crawler output into dir. The markdown is
// deliberately identical on every call, so a metadata-only refresh can be told apart
// from a content change.
func writeCrawlerMetadata(t *testing.T, dir string, title string, depth int, author string) {
	t.Helper()

	metadata := `{
  "crawl_started_at": "2026-05-22T10:00:00Z",
  "last_completed_crawl_completed_at": "2026-05-22T10:20:03Z",
  "last_completed_crawl_mode": "updates",
  "seed_page_ids": ["1"],
  "pages": {
    "1": {
      "local_path": "a.md",
      "title": ` + strconv.Quote(title) + `,
      "space_key": "OPS",
      "host": "org.test",
      "canonical_url": "https://org.test/wiki/spaces/OPS/pages/1",
      "depth": ` + strconv.Itoa(depth) + `,
      "created_by_name": ` + strconv.Quote(author) + `,
      "incoming_links": ["2"],
      "attachments": ["design.pdf"],
      "comment_count": 1,
      "last_modified_at": "2026-05-20T10:00:00Z",
      "source_url": "https://org.test/1"
    }
  }
}`

	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# Page\n\nbody text for indexing\n"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
}

func TestIndexRefreshesMetadataWithoutReembedding(t *testing.T) {
	dir := t.TempDir()
	writeCrawlerMetadata(t, dir, "Deployment", 1, "Ada Lovelace")

	dbPath := filepath.Join(t.TempDir(), "index.db")
	app := newTestApp(t)

	first := decodePayload(t, captureStdout(t, func() {
		if exit := app.Run([]string{"index", dir, "--db", dbPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("first index exit code: %d", exit)
		}
	}))
	if first.Documents.Inserted != 1 || first.Embedding.Written == 0 {
		t.Fatalf("first run = %+v, want one inserted document with embeddings", first)
	}

	// The crawler runs again: the page text is untouched, the metadata is not.
	writeCrawlerMetadata(t, dir, "Deployment renamed", 4, "Grace Hopper")

	second := decodePayload(t, captureStdout(t, func() {
		if exit := app.Run([]string{"index", dir, "--db", dbPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("second index exit code: %d", exit)
		}
	}))
	if second.Documents.Metadata != 1 {
		t.Fatalf("metadata updates = %d, want 1", second.Documents.Metadata)
	}
	if second.Documents.Updated != 0 || second.Embedding.Written != 0 {
		t.Fatalf("second run = %+v, want no chunk rewrites and no embedding calls", second)
	}

	// The rename is searchable, and the stored vectors still answer vector queries.
	lexical := decodeQueryPayload(t, captureStdout(t, func() {
		if exit := app.Run([]string{"query", "--db", dbPath, "--q", "renamed", "--mode", "lexical", "--json"}); exit != exitCodeOK {
			t.Fatalf("lexical query exit code: %d", exit)
		}
	}))
	if lexical.Count != 1 || lexical.Results[0].Title != "Deployment renamed" {
		t.Fatalf("lexical results = %+v, want the renamed page", lexical)
	}

	vector := decodeQueryPayload(t, captureStdout(t, func() {
		if exit := app.Run([]string{"query", "--db", dbPath, "--q", "body text", "--mode", "vector", "--json"}); exit != exitCodeOK {
			t.Fatalf("vector query exit code: %d", exit)
		}
	}))
	if vector.Count == 0 {
		t.Fatal("the stored vectors must still answer vector queries")
	}
}

// queryPayload mirrors the parts of query output these tests read.
type queryPayload struct {
	Count   int `json:"count"`
	Results []struct {
		Title           string             `json:"title"`
		MetadataBoost   float64            `json:"metadataBoost"`
		MetadataFactors map[string]float64 `json:"metadataFactors"`
	} `json:"results"`
	Request struct {
		Filters struct {
			Spaces         []string `json:"Spaces"`
			Host           string   `json:"Host"`
			Author         string   `json:"Author"`
			CreatedBy      string   `json:"CreatedBy"`
			ModifiedBy     string   `json:"ModifiedBy"`
			DepthMin       *int     `json:"DepthMin"`
			DepthMax       *int     `json:"DepthMax"`
			SeedOnly       bool     `json:"SeedOnly"`
			HasAttachments bool     `json:"HasAttachments"`
			UpdatedSince   string   `json:"UpdatedSince"`
		} `json:"Filters"`
	} `json:"request"`
}

// writeFilterCorpus writes three pages that differ in every metadata filter the query
// command exposes. Every body says "shared", so the filters are what narrow a search.
func writeFilterCorpus(t *testing.T, dir string) {
	t.Helper()

	page := func(id string, space string, host string, depth int, createdBy string, modifiedBy string, attachments string, modifiedAt string) string {
		return `"` + id + `": {
      "local_path": "` + id + `.md",
      "title": "Page ` + id + `",
      "space_key": "` + space + `",
      "host": "` + host + `",
      "depth": ` + strconv.Itoa(depth) + `,
      "created_by_name": "` + createdBy + `",
      "last_modified_by_name": "` + modifiedBy + `",
      "attachments": [` + attachments + `],
      "last_modified_at": "` + modifiedAt + `",
      "source_url": "https://` + host + `/` + id + `"
    }`
	}

	metadata := `{
  "crawl_started_at": "2026-05-22T10:00:00Z",
  "last_completed_crawl_completed_at": "2026-05-22T10:20:03Z",
  "last_completed_crawl_mode": "updates",
  "seed_page_ids": ["1"],
  "pages": {
    ` + page("1", "OPS", "a.test", 0, "Ada Lovelace", "Ada Lovelace", `"design.pdf"`, "2026-05-20T10:00:00Z") + `,
    ` + page("2", "OPS", "a.test", 1, "Grace Hopper", "Ada Lovelace", ``, "2025-01-01T10:00:00Z") + `,
    ` + page("3", "ENG", "b.test", 2, "Ada Lovelace", "Grace Hopper", `"notes.pdf"`, "2026-06-01T10:00:00Z") + `
  }
}`

	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	for _, id := range []string{"1", "2", "3"} {
		body := "# Page " + id + "\n\nshared body text\n"
		if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(body), 0644); err != nil {
			t.Fatalf("write markdown %s: %v", id, err)
		}
	}
}

func TestQueryMetadataFiltersNarrowResults(t *testing.T) {
	dir := t.TempDir()
	writeFilterCorpus(t, dir)

	dbPath := filepath.Join(t.TempDir(), "index.db")
	app := newTestApp(t)

	captureStdout(t, func() {
		if exit := app.Run([]string{"index", dir, "--db", dbPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("index exit code: %d", exit)
		}
	})

	cases := map[string]struct {
		args []string
		want int
	}{
		"no filters":               {nil, 3},
		"space":                    {[]string{"--space", "ENG"}, 1},
		"repeated space":           {[]string{"--space", "OPS", "--space", "ENG"}, 3},
		"host":                     {[]string{"--host", "b.test"}, 1},
		"author either role":       {[]string{"--author", "grace hopper"}, 2},
		"created by":               {[]string{"--created-by", "ada lovelace"}, 2},
		"modified by":              {[]string{"--modified-by", "ada lovelace"}, 2},
		"depth at least one":       {[]string{"--depth-min", "1"}, 2},
		"depth at most one":        {[]string{"--depth-max", "1"}, 2},
		"seed only":                {[]string{"--seed-only"}, 1},
		"has attachments":          {[]string{"--has-attachments"}, 2},
		"updated since a wide age": {[]string{"--updated-since", "3650d"}, 3},
		"page id":                  {[]string{"--page-id", "2"}, 1},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			args := append([]string{"query", "--db", dbPath, "--q", "shared", "--mode", "lexical", "--json"}, tc.args...)
			payload := decodeQueryPayload(t, captureStdout(t, func() {
				if exit := app.Run(args); exit != exitCodeOK {
					t.Fatalf("query exit code: %d", exit)
				}
			}))

			if payload.Count != tc.want {
				t.Fatalf("count = %d, want %d", payload.Count, tc.want)
			}
		})
	}
}

func decodeQueryPayload(t *testing.T, output string) queryPayload {
	t.Helper()

	var payload queryPayload
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("decode query output %q: %v", output, err)
	}

	return payload
}
func TestQueryEchoesActiveMetadataFilters(t *testing.T) {
	dir := t.TempDir()
	writeFilterCorpus(t, dir)

	dbPath := filepath.Join(t.TempDir(), "index.db")
	app := newTestApp(t)

	captureStdout(t, func() {
		if exit := app.Run([]string{"index", dir, "--db", dbPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("index exit code: %d", exit)
		}
	})

	payload := decodeQueryPayload(t, captureStdout(t, func() {
		args := []string{"query", "--db", dbPath, "--q", "shared", "--mode", "lexical", "--json",
			"--space", "OPS", "--host", "a.test", "--author", "Ada Lovelace",
			"--depth-min", "1", "--depth-max", "3", "--seed-only", "--has-attachments", "--updated-since", "30d"}
		if exit := app.Run(args); exit != exitCodeOK {
			t.Fatalf("query exit code: %d", exit)
		}
	}))

	filters := payload.Request.Filters
	if len(filters.Spaces) != 1 || filters.Spaces[0] != "OPS" || filters.Host != "a.test" {
		t.Fatalf("filters = %+v, want the space and host echoed", filters)
	}
	if filters.Author != "Ada Lovelace" || !filters.SeedOnly || !filters.HasAttachments {
		t.Fatalf("filters = %+v, want the author and the boolean filters echoed", filters)
	}
	if filters.DepthMin == nil || *filters.DepthMin != 1 || filters.DepthMax == nil || *filters.DepthMax != 3 {
		t.Fatalf("filters = %+v, want the depth range echoed", filters)
	}
	if filters.UpdatedSince == "" {
		t.Fatalf("filters = %+v, want a resolved --updated-since cutoff", filters)
	}
}

func TestQueryRejectsInvalidMetadataFilters(t *testing.T) {
	app := newTestApp(t)

	cases := map[string][]string{
		"negative depth minimum": {"query", "--q", "x", "--depth-min", "-2"},
		"negative depth maximum": {"query", "--q", "x", "--depth-max", "-5"},
		"inverted depth range":   {"query", "--q", "x", "--depth-min", "5", "--depth-max", "2"},
		"unparsable age":         {"query", "--q", "x", "--updated-since", "whenever"},
	}

	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			stderr := captureStderr(t, func() {
				if exit := app.Run(args); exit != exitCodeInvalidUsage {
					t.Fatalf("exit code = %d, want %d", exit, exitCodeInvalidUsage)
				}
			})
			if !strings.Contains(stderr, "depth") && !strings.Contains(stderr, "updated-since") {
				t.Fatalf("stderr = %q, want an explanation of the rejected flag", stderr)
			}
		})
	}
}

func TestParseUpdatedSince(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	cases := map[string]string{
		"":      "",
		"30d":   "2026-08-21T12:00:00Z",
		"2w":    "2026-09-06T12:00:00Z",
		"1 day": "2026-09-19T12:00:00Z",
		"12h":   "2026-09-20T00:00:00Z",
		"90m":   "2026-09-20T10:30:00Z",
	}

	for value, want := range cases {
		t.Run("age "+value, func(t *testing.T) {
			got, err := parseUpdatedSince(value, now)
			if err != nil {
				t.Fatalf("parseUpdatedSince(%q): %v", value, err)
			}
			if got != want {
				t.Fatalf("parseUpdatedSince(%q) = %q, want %q", value, got, want)
			}
		})
	}

	if _, err := parseUpdatedSince("whenever", now); err == nil {
		t.Fatal("expected an error for an unparsable age")
	}
}

// indexedFilterCorpus writes the three-page metadata corpus into a temporary database.
func indexedFilterCorpus(t *testing.T, app *App) string {
	t.Helper()

	dir := t.TempDir()
	writeFilterCorpus(t, dir)

	dbPath := filepath.Join(t.TempDir(), "index.db")
	captureStdout(t, func() {
		if exit := app.Run([]string{"index", dir, "--db", dbPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("index exit code: %d", exit)
		}
	})

	return dbPath
}

func TestQueryPriorsReorderResultsAndExplainThemselves(t *testing.T) {
	app := newTestApp(t)
	dbPath := indexedFilterCorpus(t, app)

	plain := decodeQueryPayload(t, captureStdout(t, func() {
		if exit := app.Run([]string{"query", "--db", dbPath, "--q", "shared", "--mode", "lexical", "--json"}); exit != exitCodeOK {
			t.Fatalf("query exit code: %d", exit)
		}
	}))
	if plain.Results[0].Title != "Page 1" {
		t.Fatalf("without priors the chunk id breaks the tie, got %q first", plain.Results[0].Title)
	}
	if plain.Results[0].MetadataBoost != 0 || plain.Results[0].MetadataFactors != nil {
		t.Fatalf("prior fields appeared without --priors: %+v", plain.Results[0])
	}

	// Page 3 is the freshest of the three, page 2 the stalest.
	boosted := decodeQueryPayload(t, captureStdout(t, func() {
		args := []string{"query", "--db", dbPath, "--q", "shared", "--mode", "lexical", "--json", "--priors", "recency"}
		if exit := app.Run(args); exit != exitCodeOK {
			t.Fatalf("query exit code: %d", exit)
		}
	}))
	if boosted.Results[0].Title != "Page 3" {
		t.Fatalf("with the recency prior the freshest page must lead, got %q", boosted.Results[0].Title)
	}
	if boosted.Results[0].MetadataBoost <= 0 {
		t.Fatalf("boost = %v, want a positive adjustment", boosted.Results[0].MetadataBoost)
	}
	if boosted.Results[0].MetadataFactors["recency"] <= boosted.Results[1].MetadataFactors["recency"] {
		t.Fatalf("factors = %+v, want the fresher page to score higher", boosted.Results[0].MetadataFactors)
	}

	explain := captureStdout(t, func() {
		args := []string{"query", "--db", dbPath, "--q", "shared", "--mode", "lexical", "--explain",
			"--priors", "recency,seed", "--prior-strength", "0.2", "--recency-half-life", "30d"}
		if exit := app.Run(args); exit != exitCodeOK {
			t.Fatalf("explain exit code: %d", exit)
		}
	})
	for _, want := range []string{"priors=recency,seed", "strength=0.20", "recency-half-life=720h0m0s", "metadata factors="} {
		if !strings.Contains(explain, want) {
			t.Fatalf("explain output %q, want it to mention %q", explain, want)
		}
	}
}

func TestStatsReportsMetadataCoverageAndCrawl(t *testing.T) {
	app := newTestApp(t)
	dbPath := indexedFilterCorpus(t, app)

	text := captureStdout(t, func() {
		if exit := app.Run([]string{"stats", "--db", dbPath}); exit != exitCodeOK {
			t.Fatalf("stats exit code: %d", exit)
		}
	})
	for _, want := range []string{
		"metadata: authors=3 links=0 attachments=2 comments=0 seeds=1 nested=2 hosts=2",
		"corpus: mode=updates pages=3 seeds=1",
		"crawl completed 2026-05-22T10:20:03Z",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("stats output %q, want it to mention %q", text, want)
		}
	}

	var payload struct {
		Stats struct {
			Metadata struct {
				Documents       int `json:"documents"`
				WithAuthors     int `json:"withAuthors"`
				WithAttachments int `json:"withAttachments"`
				Seeds           int `json:"seeds"`
				Hosts           int `json:"hosts"`
			} `json:"metadata"`
			Corpus struct {
				Mode        string `json:"crawlMode"`
				SeedCount   int    `json:"seedCount"`
				PageCount   int    `json:"pageCount"`
				CompletedAt string `json:"crawlCompletedAt"`
				IndexedAt   string `json:"indexedAt"`
			} `json:"corpus"`
		} `json:"stats"`
	}
	raw := captureStdout(t, func() {
		if exit := app.Run([]string{"stats", "--db", dbPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("stats --json exit code: %d", exit)
		}
	})
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode stats output %q: %v", raw, err)
	}

	if payload.Stats.Metadata.Documents != 3 || payload.Stats.Metadata.WithAuthors != 3 {
		t.Fatalf("metadata = %+v, want the indexed pages counted", payload.Stats.Metadata)
	}
	if payload.Stats.Metadata.WithAttachments != 2 || payload.Stats.Metadata.Seeds != 1 || payload.Stats.Metadata.Hosts != 2 {
		t.Fatalf("metadata = %+v, want the attachments, seeds and hosts counted", payload.Stats.Metadata)
	}
	if payload.Stats.Corpus.Mode != "updates" || payload.Stats.Corpus.PageCount != 3 || payload.Stats.Corpus.SeedCount != 1 {
		t.Fatalf("corpus = %+v, want the crawl the index was built from", payload.Stats.Corpus)
	}
	if payload.Stats.Corpus.IndexedAt == "" {
		t.Fatal("IndexedAt must be reported, so an operator can compare it with the crawl timestamps")
	}
}

func TestStatsOmitsCoverageAndCrawlWithoutData(t *testing.T) {
	app := newTestApp(t)
	dir := writeIndexFixture(t, "# Doc\n\nbody text\n")
	dbPath := filepath.Join(t.TempDir(), "index.db")

	captureStdout(t, func() {
		if exit := app.Run([]string{"index", dir, "--db", dbPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("index exit code: %d", exit)
		}
	})

	// The fixture carries no crawler metadata and no crawl timestamps, so the coverage
	// block is empty and the corpus block reports only what the run knew.
	raw := captureStdout(t, func() {
		if exit := app.Run([]string{"stats", "--db", dbPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("stats exit code: %d", exit)
		}
	})
	if !strings.Contains(raw, `"metadata"`) || !strings.Contains(raw, `"corpus"`) {
		t.Fatalf("stats output %q, want both blocks present", raw)
	}
}

func TestQueryUsesConfiguredDefaultsAndFlagsStillWin(t *testing.T) {
	app := newTestApp(t)
	dbPath := indexedFilterCorpus(t, app)
	configPath := writeConfigFile(t, `query:
  mode: "lexical"
  top_k: 1
  priors: ["recency"]
  prior_strength: 0.2
  recency_half_life: 30d
`)

	// Nothing passed, so the file decides the mode, the result count and the priors.
	configured := decodeQueryPayload(t, captureStdout(t, func() {
		if exit := app.Run([]string{"query", "--db", dbPath, "--config", configPath, "--q", "shared", "--json"}); exit != exitCodeOK {
			t.Fatalf("query exit code: %d", exit)
		}
	}))
	if configured.Count != 1 {
		t.Fatalf("count = %d, want the configured top_k of 1", configured.Count)
	}
	if configured.Results[0].Title != "Page 3" {
		t.Fatalf("first result = %q, want the configured recency prior to lead with the freshest page", configured.Results[0].Title)
	}
	if configured.Results[0].MetadataBoost <= 0 {
		t.Fatalf("boost = %v, want the configured prior to apply", configured.Results[0].MetadataBoost)
	}

	// A flag that is passed wins, and an empty one clears a configured value.
	overridden := decodeQueryPayload(t, captureStdout(t, func() {
		args := []string{"query", "--db", dbPath, "--config", configPath, "--q", "shared", "--json", "--top-k", "3", "--priors", ""}
		if exit := app.Run(args); exit != exitCodeOK {
			t.Fatalf("query exit code: %d", exit)
		}
	}))
	if overridden.Count != 3 {
		t.Fatalf("count = %d, want --top-k 3 to beat the configured top_k", overridden.Count)
	}
	if overridden.Results[0].MetadataBoost != 0 || overridden.Results[0].MetadataFactors != nil {
		t.Fatalf("priors applied although the flag cleared them: %+v", overridden.Results[0])
	}
}

func TestQueryRejectsInvalidConfiguredDefaults(t *testing.T) {
	app := newTestApp(t)
	configPath := writeConfigFile(t, "query:\n  mode: \"semantic\"\n")

	stderr := captureStderr(t, func() {
		if exit := app.Run([]string{"query", "--q", "x", "--db", "unused.db", "--config", configPath}); exit != exitCodeInvalidUsage {
			t.Fatalf("exit code = %d, want %d", exit, exitCodeInvalidUsage)
		}
	})
	if !strings.Contains(stderr, "query.mode") {
		t.Fatalf("stderr = %q, want it to name the rejected key", stderr)
	}
}

func TestQueryRejectsInvalidPriors(t *testing.T) {
	app := newTestApp(t)

	cases := map[string]struct {
		args []string
		want string
	}{
		"unknown prior":        {[]string{"query", "--q", "x", "--priors", "freshness"}, "available"},
		"strength above one":   {[]string{"query", "--q", "x", "--priors", "seed", "--prior-strength", "1.5"}, "prior-strength"},
		"negative strength":    {[]string{"query", "--q", "x", "--priors", "seed", "--prior-strength", "-0.2"}, "prior-strength"},
		"unparsable half-life": {[]string{"query", "--q", "x", "--priors", "recency", "--recency-half-life", "soon"}, "recency-half-life"},
		"negative half-life":   {[]string{"query", "--q", "x", "--priors", "recency", "--recency-half-life", "-1h"}, "must not be negative"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stderr := captureStderr(t, func() {
				if exit := app.Run(tc.args); exit != exitCodeInvalidUsage {
					t.Fatalf("exit code = %d, want %d", exit, exitCodeInvalidUsage)
				}
			})
			if !strings.Contains(stderr, tc.want) {
				t.Fatalf("stderr = %q, want it to mention %q", stderr, tc.want)
			}
		})
	}
}

func TestParseUpdatedSinceResolvesAges(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	if got, err := parseUpdatedSince("", now); err != nil || got != "" {
		t.Fatalf("parseUpdatedSince(\"\") = (%q, %v), want no cutoff", got, err)
	}
	if got, err := parseUpdatedSince("30d", now); err != nil || got != "2026-08-21T12:00:00Z" {
		t.Fatalf("parseUpdatedSince(30d) = (%q, %v), want 2026-08-21T12:00:00Z", got, err)
	}
	if _, err := parseUpdatedSince("soon", now); err == nil {
		t.Fatal("expected an error for an unparsable age")
	}
}
