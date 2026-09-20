package indexer

import (
	"os"
	"path/filepath"
	"testing"
)

// writeMetadataCorpus writes a metadata.json and the page markdown it references.
func writeMetadataCorpus(t *testing.T, metadata string, markdown string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, MetadataFileName), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "page.md"), []byte(markdown), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	return dir
}

func TestLoadCorpusReadsCrawlerMetadata(t *testing.T) {
	metadata := `{
  "crawl_started_at": "2026-05-22T10:00:00Z",
  "last_completed_crawl_completed_at": "2026-05-22T10:20:03Z",
  "last_completed_crawl_mode": "updates",
  "last_successful_crawl_completed_at": "2026-05-22T10:20:03Z",
  "seed_page_ids": ["org.test/1"],
  "pages": {
    "org.test/1": {
      "id": "org.test/1",
      "host": "org.test",
      "local_path": "page.md",
      "title": "Deployment",
      "space_key": "OPS",
      "version": 7,
      "depth": 0,
      "crawled_at": "2026-05-22T10:16:00Z",
      "created_at": "2024-01-01T00:00:00Z",
      "last_modified_at": "2026-05-20T10:00:00Z",
      "source_url": "https://org.test/wiki/pages/1",
      "canonical_url": "https://org.test/wiki/spaces/OPS/pages/1/Deployment",
      "confluence_parent_id": "99",
      "created_by_name": "Ada Lovelace",
      "last_modified_by_name": "Grace Hopper",
      "outgoing_links": ["org.test/2", "org.test/3"],
      "incoming_links": ["org.test/4"],
      "attachments": ["design.pdf"],
      "comment_count": 2
    }
  }
}`

	corpus, err := LoadCorpus(writeMetadataCorpus(t, metadata, "# Deployment\n\nbody\n"), DefaultChunkSize, DefaultChunkOverlap)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}

	if corpus.Crawl.StartedAt != "2026-05-22T10:00:00Z" || corpus.Crawl.CompletedAt != "2026-05-22T10:20:03Z" {
		t.Fatalf("crawl timestamps = %+v, want the crawler values", corpus.Crawl)
	}
	if corpus.Crawl.Mode != "updates" || corpus.Crawl.SeedCount != 1 || corpus.Crawl.PageCount != 1 {
		t.Fatalf("crawl summary = %+v, want mode/seed/page counts", corpus.Crawl)
	}

	if len(corpus.Documents) != 1 {
		t.Fatalf("documents = %d, want 1", len(corpus.Documents))
	}
	got := corpus.Documents[0].Metadata
	want := Metadata{
		Host:           "org.test",
		CanonicalURL:   "https://org.test/wiki/spaces/OPS/pages/1/Deployment",
		Version:        7,
		Depth:          0,
		ParentID:       "99",
		CreatedAt:      "2024-01-01T00:00:00Z",
		CrawledAt:      "2026-05-22T10:16:00Z",
		CreatedByName:  "Ada Lovelace",
		ModifiedByName: "Grace Hopper",
		IsSeed:         true,
		LinkIn:         1,
		LinkOut:        2,
		Attachments:    1,
		Comments:       2,
	}
	if got != want {
		t.Fatalf("metadata = %+v, want %+v", got, want)
	}
}

func TestLoadCorpusFillsGapsFromFrontMatter(t *testing.T) {
	// metadata.json carries only what an older crawler wrote; the front matter holds
	// the rest.
	metadata := `{"pages":{"1":{"local_path":"page.md","title":"Deployment","space_key":"OPS","last_modified_at":"2026-05-20T10:00:00Z","source_url":"https://org.test/1"}}}`
	markdown := "---\n" +
		"page_id: 1\n" +
		"title: Deployment\n" +
		"space_key: OPS\n" +
		"canonical_url: https://org.test/wiki/spaces/OPS/pages/1\n" +
		"is_seed: true\n" +
		"crawled_at: 2026-05-22T10:16:00Z\n" +
		"created_at: 2024-01-01T00:00:00Z\n" +
		"created_by: Ada Lovelace\n" +
		"last_modified_by: Grace Hopper\n" +
		"confluence_parent_id: 99\n" +
		"comment_count: 3\n" +
		"attachments:\n  - design.pdf\n" +
		"---\n\n# Deployment\n\nbody\n"

	corpus, err := LoadCorpus(writeMetadataCorpus(t, metadata, markdown), DefaultChunkSize, DefaultChunkOverlap)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}

	want := Metadata{
		CanonicalURL:   "https://org.test/wiki/spaces/OPS/pages/1",
		CreatedAt:      "2024-01-01T00:00:00Z",
		CrawledAt:      "2026-05-22T10:16:00Z",
		CreatedByName:  "Ada Lovelace",
		ModifiedByName: "Grace Hopper",
		ParentID:       "99",
		IsSeed:         true,
		Attachments:    1,
		Comments:       3,
	}
	if got := corpus.Documents[0].Metadata; got != want {
		t.Fatalf("metadata = %+v, want %+v", got, want)
	}
	if content := corpus.Documents[0].Chunks[0].Text; content != "# Deployment\n\nbody" {
		t.Fatalf("chunk text = %q, want the front matter stripped", content)
	}
}

func TestMergePageMetadataPrefersTheCrawlerRecord(t *testing.T) {
	page := pageRecord{CanonicalURL: "https://from-json/1", CreatedByName: "JSON Author", CommentCount: 5}
	front := frontMatter{CanonicalURL: "https://from-front/1", CreatedBy: "Front Author", CommentCount: 9}
	seeds := map[string]bool{"1": true}

	got := mergePageMetadata("1", page, front, seeds)
	if got.CanonicalURL != "https://from-json/1" || got.CreatedByName != "JSON Author" || got.Comments != 5 {
		t.Fatalf("metadata = %+v, want the metadata.json values", got)
	}

	// seed_page_ids is authoritative: a page missing from it is not a seed even when
	// its front matter claims otherwise.
	frontSeed := frontMatter{IsSeed: boolPointer(true)}
	if mergePageMetadata("2", pageRecord{}, frontSeed, seeds).IsSeed {
		t.Fatal("a page outside seed_page_ids must not be marked as a seed")
	}

	// Without seed_page_ids the front matter is the only source.
	if !mergePageMetadata("2", pageRecord{}, frontSeed, map[string]bool{}).IsSeed {
		t.Fatal("front matter is_seed must be honoured when the crawler writes no seed list")
	}
}

func TestFrontMatterBlockHandlesEdgeCases(t *testing.T) {
	cases := map[string]struct {
		content string
		want    string
		ok      bool
	}{
		"plain block":  {"---\nsource_url: https://x\n---\n\nbody", "source_url: https://x", true},
		"crlf block":   {"---\r\nsource_url: https://x\r\n---\r\n\r\nbody", "source_url: https://x", true},
		"unterminated": {"---\nsource_url: https://x\n\nbody", "", false},
		"no block":     {"# Title\n\nbody", "", false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := frontMatterBlock(tc.content)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("frontMatterBlock(%q) = (%q, %t), want (%q, %t)", tc.content, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestParseFrontMatterIgnoresBrokenYAML(t *testing.T) {
	parsed := parseFrontMatter("---\nsource_url: [unclosed\n---\n\nbody")
	if parsed.SourceURL != "" || parsed.IsSeed != nil || len(parsed.Attachments) != 0 {
		t.Fatalf("parsed = %+v, want an empty front matter", parsed)
	}
}

func TestMetadataFingerprintTracksEveryStoredValue(t *testing.T) {
	base := DocumentInput{
		Title:      "Deployment",
		SpaceKey:   "OPS",
		SourceURL:  "https://org.test/1",
		ModifiedAt: "2026-05-20T10:00:00Z",
		Metadata:   Metadata{Depth: 1, CreatedByName: "Ada Lovelace"},
	}
	if again := base; again.MetadataFingerprint() != base.MetadataFingerprint() {
		t.Fatal("the same document must produce the same fingerprint")
	}

	cases := map[string]DocumentInput{
		"title":       {Title: "Deployment renamed"},
		"space":       {SpaceKey: "ENG"},
		"source url":  {SourceURL: "https://org.test/2"},
		"modified at": {ModifiedAt: "2026-05-21T10:00:00Z"},
		"depth":       {Metadata: Metadata{Depth: 2, CreatedByName: "Ada Lovelace"}},
		"author":      {Metadata: Metadata{Depth: 1, CreatedByName: "Grace Hopper"}},
		"seed":        {Metadata: Metadata{Depth: 1, CreatedByName: "Ada Lovelace", IsSeed: true}},
		"links":       {Metadata: Metadata{Depth: 1, CreatedByName: "Ada Lovelace", LinkIn: 3}},
	}

	for name, changed := range cases {
		t.Run(name, func(t *testing.T) {
			if changed.MetadataFingerprint() == base.MetadataFingerprint() {
				t.Fatalf("a changed %s must change the fingerprint", name)
			}
		})
	}
}

func boolPointer(value bool) *bool { return &value }
