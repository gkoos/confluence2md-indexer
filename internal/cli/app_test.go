package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/config"
	"github.com/gkoos/confluence2md-indexer/internal/query"
)

func TestRunIndexPreflightOK(t *testing.T) {
	dir := t.TempDir()
	metadata := `{
  "pages": {
    "1": {"local_path": "a.md"}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# test"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	app := newTestApp(t)
	exit := app.Run([]string{"index", dir})
	if exit != exitCodeOK {
		t.Fatalf("expected exit %d got %d", exitCodeOK, exit)
	}
}

func TestRunIndexPreflightOKWithFolderBeforeFlags(t *testing.T) {
	dir := t.TempDir()
	metadata := `{
  "pages": {
    "1": {"local_path": "a.md"}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# test"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	app := newTestApp(t)
	exit := app.Run([]string{"index", dir, "--rebuild", "--json"})
	if exit != exitCodeOK {
		t.Fatalf("expected exit %d got %d", exitCodeOK, exit)
	}
}

func TestRunIndexPreflightFail(t *testing.T) {
	app := newTestApp(t)
	exit := app.Run([]string{"index", t.TempDir()})
	if exit != exitCodeInvalidUsage {
		t.Fatalf("expected exit %d got %d", exitCodeInvalidUsage, exit)
	}
}

func TestRunQueryValidationFail(t *testing.T) {
	app := newTestApp(t)
	exit := app.Run([]string{"query", "--q", "abc", "--alpha", "2"})
	if exit != exitCodeInvalidUsage {
		t.Fatalf("expected exit %d got %d", exitCodeInvalidUsage, exit)
	}
}

func TestRunQueryJSONSuccess(t *testing.T) {
	dir := t.TempDir()
	metadata := `{
  "pages": {
    "1": {
      "local_path": "a.md",
      "title": "Alpha",
      "space_key": "ENG",
      "last_modified_at": "2026-01-15T12:00:00Z",
      "source_url": "https://example.test/1"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# heading\n\nbanana text"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	app := newTestApp(t)
	if exit := app.Run([]string{"index", dir}); exit != exitCodeOK {
		t.Fatalf("expected index exit %d got %d", exitCodeOK, exit)
	}

	queryOutput := captureStdout(t, func() {
		exit := app.Run([]string{"query", "--db", filepath.Join(dir, defaultDBFileName), "--q", "banana", "--json"})
		if exit != exitCodeOK {
			t.Fatalf("expected query exit %d got %d", exitCodeOK, exit)
		}
	})

	if !strings.Contains(queryOutput, `"command": "query"`) {
		t.Fatalf("expected query json command field in output")
	}
	if !strings.Contains(queryOutput, `"schemaVersion": "1"`) {
		t.Fatalf("expected schemaVersion in output")
	}
	if !strings.Contains(queryOutput, `"results":`) {
		t.Fatalf("expected query results in output")
	}
}

func TestRunQueryPaginationJSON(t *testing.T) {
	dir := t.TempDir()
	metadata := `{
  "pages": {
    "1": {
      "local_path": "a.md",
      "title": "Alpha",
      "space_key": "ENG",
      "last_modified_at": "2026-01-15T12:00:00Z",
      "source_url": "https://example.test/1"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# banana\n\nbanana one\n\n## two\n\nbanana two"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	app := newTestApp(t)
	if exit := app.Run([]string{"index", dir}); exit != exitCodeOK {
		t.Fatalf("expected index exit %d got %d", exitCodeOK, exit)
	}

	queryOutput := captureStdout(t, func() {
		exit := app.Run([]string{
			"query",
			"--db", filepath.Join(dir, defaultDBFileName),
			"--q", "banana",
			"--top-k", "10",
			"--offset", "1",
			"--limit", "1",
			"--json",
		})
		if exit != exitCodeOK {
			t.Fatalf("expected query exit %d got %d", exitCodeOK, exit)
		}
	})

	if !strings.Contains(queryOutput, `"pagination":`) {
		t.Fatalf("expected pagination block in output")
	}
	if !strings.Contains(queryOutput, `"offset": 1`) {
		t.Fatalf("expected pagination offset in output")
	}
	if !strings.Contains(queryOutput, `"limit": 1`) {
		t.Fatalf("expected pagination limit in output")
	}
}

func TestRunQueryParentFilterJSON(t *testing.T) {
	dir := t.TempDir()
	metadata := `{
  "pages": {
    "1": {"local_path": "parent.md", "title": "Parent", "space_key": "ENG"},
    "2": {"local_path": "child.md", "title": "Child", "space_key": "ENG", "confluence_parent_id": "1"}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "parent.md"), []byte("# parent\n\nbanana parent"), 0644); err != nil {
		t.Fatalf("write parent markdown: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "child.md"), []byte("# child\n\nbanana child"), 0644); err != nil {
		t.Fatalf("write child markdown: %v", err)
	}

	app := newTestApp(t)
	if exit := app.Run([]string{"index", dir}); exit != exitCodeOK {
		t.Fatalf("expected index exit %d got %d", exitCodeOK, exit)
	}

	queryOutput := captureStdout(t, func() {
		exit := app.Run([]string{
			"query",
			"--db", filepath.Join(dir, defaultDBFileName),
			"--q", "banana",
			"--parent", "1",
			"--json",
		})
		if exit != exitCodeOK {
			t.Fatalf("expected query exit %d got %d", exitCodeOK, exit)
		}
	})

	if !strings.Contains(queryOutput, `"ParentID": "1"`) {
		t.Fatalf("expected the parent filter in the echoed request, output=%s", queryOutput)
	}
	if !strings.Contains(queryOutput, `"pageId": "2"`) {
		t.Fatalf("expected the direct child in the results, output=%s", queryOutput)
	}
	if strings.Contains(queryOutput, `"pageId": "1"`) {
		t.Fatalf("expected the parent page to be excluded, output=%s", queryOutput)
	}
}

func TestRunQueryMaxPerPageJSON(t *testing.T) {
	dir := t.TempDir()
	metadata := `{
  "pages": {
    "1": {"local_path": "a.md", "title": "Alpha", "space_key": "ENG"}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# banana\n\nbanana one\n\n## two\n\nbanana two"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	app := newTestApp(t)
	if exit := app.Run([]string{"index", dir}); exit != exitCodeOK {
		t.Fatalf("expected index exit %d got %d", exitCodeOK, exit)
	}

	query := func(args ...string) string {
		t.Helper()

		return captureStdout(t, func() {
			runArgs := append([]string{"query", "--db", filepath.Join(dir, defaultDBFileName), "--q", "banana", "--top-k", "10", "--json"}, args...)
			if exit := app.Run(runArgs); exit != exitCodeOK {
				t.Fatalf("expected query exit %d got %d", exitCodeOK, exit)
			}
		})
	}

	if uncapped := query(); !strings.Contains(uncapped, `"count": 2`) {
		t.Fatalf("expected both chunks of the page by default, output=%s", uncapped)
	}

	capped := query("--max-per-page", "1")
	if !strings.Contains(capped, `"count": 1`) {
		t.Fatalf("expected the cap to keep one chunk, output=%s", capped)
	}
	if !strings.Contains(capped, `"MaxPerPage": 1`) {
		t.Fatalf("expected the cap in the echoed request, output=%s", capped)
	}
}

func TestRunQueryMaxPerPageValidation(t *testing.T) {
	app := newTestApp(t)
	if exit := app.Run([]string{"query", "--q", "abc", "--max-per-page", "-1"}); exit != exitCodeInvalidUsage {
		t.Fatalf("expected exit %d got %d", exitCodeInvalidUsage, exit)
	}
}

func TestRunQueryGroupDocumentJSON(t *testing.T) {
	dir := t.TempDir()
	metadata := `{
  "pages": {
    "1": {"local_path": "a.md", "title": "Alpha", "space_key": "ENG"}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# banana\n\nbanana one\n\n## two\n\nbanana two"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	app := newTestApp(t)
	if exit := app.Run([]string{"index", dir}); exit != exitCodeOK {
		t.Fatalf("expected index exit %d got %d", exitCodeOK, exit)
	}

	queryOutput := captureStdout(t, func() {
		exit := app.Run([]string{
			"query",
			"--db", filepath.Join(dir, defaultDBFileName),
			"--q", "banana",
			"--group", "document",
			"--json",
		})
		if exit != exitCodeOK {
			t.Fatalf("expected query exit %d got %d", exitCodeOK, exit)
		}
	})

	if !strings.Contains(queryOutput, `"Group": "document"`) {
		t.Fatalf("expected the group in the echoed request, output=%s", queryOutput)
	}
	if !strings.Contains(queryOutput, `"count": 1`) {
		t.Fatalf("expected one result for the page, output=%s", queryOutput)
	}
	if !strings.Contains(queryOutput, `"chunkCount": 2`) {
		t.Fatalf("expected the chunk count of the page, output=%s", queryOutput)
	}
}

func TestRunQueryGroupValidation(t *testing.T) {
	app := newTestApp(t)
	if exit := app.Run([]string{"query", "--q", "abc", "--group", "chunk"}); exit != exitCodeInvalidUsage {
		t.Fatalf("expected exit %d got %d", exitCodeInvalidUsage, exit)
	}
}

func TestRunQueryFacetsJSONAndText(t *testing.T) {
	dir := t.TempDir()
	metadata := `{
  "pages": {
    "1": {"local_path": "a.md", "title": "Alpha", "space_key": "ENG", "created_by_name": "Ada Lovelace", "last_modified_by_name": "Ada Lovelace"},
    "2": {"local_path": "b.md", "title": "Beta", "space_key": "OPS", "created_by_name": "Grace Hopper", "last_modified_by_name": "Grace Hopper"}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	for _, name := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# banana\n\nbanana text"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	app := newTestApp(t)
	if exit := app.Run([]string{"index", dir}); exit != exitCodeOK {
		t.Fatalf("expected index exit %d got %d", exitCodeOK, exit)
	}

	query := func(args ...string) string {
		t.Helper()

		return captureStdout(t, func() {
			runArgs := append([]string{"query", "--db", filepath.Join(dir, defaultDBFileName), "--q", "banana"}, args...)
			if exit := app.Run(runArgs); exit != exitCodeOK {
				t.Fatalf("expected query exit %d got %d", exitCodeOK, exit)
			}
		})
	}

	jsonOutput := query("--facets", "space,author", "--json")
	for _, want := range []string{
		`"Facets": [`,
		`"facets": {`,
		`"ENG": 1`,
		`"OPS": 1`,
		`"Ada Lovelace": 1`,
		`"Grace Hopper": 1`,
	} {
		if !strings.Contains(jsonOutput, want) {
			t.Fatalf("expected %s in the json output, output=%s", want, jsonOutput)
		}
	}

	textOutput := query("--facets", "space,author")
	for _, want := range []string{"facets:", "- space: ENG=1, OPS=1", "- author: Ada Lovelace=1, Grace Hopper=1"} {
		if !strings.Contains(textOutput, want) {
			t.Fatalf("expected %q in the text output, output=%s", want, textOutput)
		}
	}
}

func TestRunQueryFacetsValidation(t *testing.T) {
	app := newTestApp(t)
	if exit := app.Run([]string{"query", "--q", "abc", "--facets", "label"}); exit != exitCodeInvalidUsage {
		t.Fatalf("expected exit %d got %d", exitCodeInvalidUsage, exit)
	}
}

func TestRunQueryExplainSuccess(t *testing.T) {
	dir := t.TempDir()
	metadata := `{
  "pages": {
    "1": {
      "local_path": "a.md",
      "title": "Alpha",
      "space_key": "ENG",
      "last_modified_at": "2026-01-15T12:00:00Z",
      "source_url": "https://example.test/1"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# heading\n\nbanana text"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	app := newTestApp(t)
	if exit := app.Run([]string{"index", dir}); exit != exitCodeOK {
		t.Fatalf("expected index exit %d got %d", exitCodeOK, exit)
	}

	queryOutput := captureStdout(t, func() {
		exit := app.Run([]string{"query", "--db", filepath.Join(dir, defaultDBFileName), "--q", "banana", "--explain"})
		if exit != exitCodeOK {
			t.Fatalf("expected query exit %d got %d", exitCodeOK, exit)
		}
	})

	if !strings.Contains(queryOutput, "explain:") {
		t.Fatalf("expected explain section in output")
	}
	if !strings.Contains(queryOutput, "fusion=") {
		t.Fatalf("expected explain details in output")
	}
}

func TestRunQueryExpandShowsContextRange(t *testing.T) {
	dir := t.TempDir()
	metadata := `{
  "pages": {
    "1": {
      "local_path": "a.md",
      "title": "Alpha",
      "space_key": "ENG",
      "last_modified_at": "2026-01-15T12:00:00Z",
      "source_url": "https://example.test/1"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# heading\n\nleft\n\n## middleterm\n\ncenter\n\n## right\n\nright"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	app := newTestApp(t)
	if exit := app.Run([]string{"index", dir}); exit != exitCodeOK {
		t.Fatalf("expected index exit %d got %d", exitCodeOK, exit)
	}

	queryOutput := captureStdout(t, func() {
		exit := app.Run([]string{"query", "--db", filepath.Join(dir, defaultDBFileName), "--q", "middleterm", "--expand", "1", "--explain"})
		if exit != exitCodeOK {
			t.Fatalf("expected query exit %d got %d", exitCodeOK, exit)
		}
	})

	if !strings.Contains(queryOutput, "context-range=") {
		t.Fatalf("expected context range details in output")
	}
}

func TestBuildExplainSummaryUsesEffectiveFusionForLexicalMode(t *testing.T) {
	lines := buildExplainSummary([]query.Result{{
		ChunkID: "c1",
		Fused:   1,
		Lexical: 1,
		Vector:  0,
	}}, query.Request{Mode: "lexical", Fusion: "weighted", Alpha: 0.7, RRFK: 60})

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "fusion=lexical") {
		t.Fatalf("expected effective lexical fusion in explain output")
	}
	if strings.Contains(joined, "top weighted-components") {
		t.Fatalf("did not expect weighted-components line for lexical mode")
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = original

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	_ = r.Close()

	return buf.String()
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stderr = w

	fn()

	_ = w.Close()
	os.Stderr = original

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read stderr pipe: %v", err)
	}
	_ = r.Close()

	return buf.String()
}

// writeIndexFixture creates a minimal valid confluence2md output folder.
func writeIndexFixture(t *testing.T, markdown string) string {
	t.Helper()

	dir := t.TempDir()
	metadata := `{
  "pages": {
    "1": {
      "local_path": "a.md",
      "title": "Alpha",
      "space_key": "ENG",
      "last_modified_at": "2026-01-15T12:00:00Z",
      "source_url": "https://example.test/1"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(markdown), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	return dir
}

func TestIndexEmbeddingFlagsReachProvider(t *testing.T) {
	dir := writeIndexFixture(t, "# heading\n\nbanana text")

	app := newTestApp(t)
	output := captureStdout(t, func() {
		// Flags placed after the positional folder must still parse.
		if exit := app.Run([]string{"index", dir, "--embedding-dim", "64", "--json"}); exit != exitCodeOK {
			t.Fatalf("expected exit %d got %d", exitCodeOK, exit)
		}
	})

	if !strings.Contains(output, `"provider": "bow-local:fnv1a@64"`) {
		t.Fatalf("expected the flag to set the vector dimension, got %s", output)
	}
	if !strings.Contains(output, `"dimension": 64`) {
		t.Fatalf("expected the dimension to be reported, got %s", output)
	}
}

func TestIndexEmbeddingFlagsOverrideEnvironment(t *testing.T) {
	t.Setenv("CONFLUENCE2MD_EMBEDDING_DIM", "64")
	dir := writeIndexFixture(t, "# heading\n\nbanana text")

	app := newTestApp(t)
	output := captureStdout(t, func() {
		if exit := app.Run([]string{"index", dir, "--embedding-dim", "128", "--json"}); exit != exitCodeOK {
			t.Fatalf("expected exit %d got %d", exitCodeOK, exit)
		}
	})

	if !strings.Contains(output, `"provider": "bow-local:fnv1a@128"`) {
		t.Fatalf("expected the flag to win over the environment, got %s", output)
	}
}

func TestIndexReportsExplicitProviderSource(t *testing.T) {
	dir := writeIndexFixture(t, "# heading\n\nbanana text")

	app := newTestApp(t)
	output := captureStdout(t, func() {
		if exit := app.Run([]string{"index", dir, "--embedding", "bow-local", "--json"}); exit != exitCodeOK {
			t.Fatalf("expected exit %d got %d", exitCodeOK, exit)
		}
	})

	if !strings.Contains(output, `"source": "flag"`) {
		t.Fatalf("expected an explicit provider id to be reported as a flag source, got %s", output)
	}
}

func TestEmbeddingListEnumeratesProviders(t *testing.T) {
	app := newTestApp(t)

	for _, args := range [][]string{
		{"index", "--embedding", "list"},
		{"query", "--q", "x", "--embedding", "list"},
	} {
		output := captureStdout(t, func() {
			if exit := app.Run(args); exit != exitCodeOK {
				t.Fatalf("expected exit %d for %v, got %d", exitCodeOK, args, exit)
			}
		})
		if !strings.Contains(output, "bow-local") || !strings.Contains(output, "openai") {
			t.Fatalf("expected provider ids for %v, got %q", args, output)
		}
	}
}

func TestEmbeddingFlagValidation(t *testing.T) {
	app := newTestApp(t)
	dbPath := filepath.Join(t.TempDir(), "index.db")

	cases := [][]string{
		{"index", t.TempDir(), "--embedding-dim", "-1"},
		{"index", t.TempDir(), "--embedding-batch-size", "-2"},
		{"index", t.TempDir(), "--embedding-max-retries", "-5"},
		{"query", "--db", dbPath, "--q", "x", "--embedding-timeout", "-1s"},
	}

	for _, args := range cases {
		if exit := app.Run(args); exit != exitCodeInvalidUsage {
			t.Fatalf("expected exit %d for %v, got %d", exitCodeInvalidUsage, args, exit)
		}
	}
}

func TestUnknownEmbeddingProviderIsActionable(t *testing.T) {
	app := newTestApp(t)
	dbPath := filepath.Join(t.TempDir(), "index.db")

	stderr := captureStderr(t, func() {
		exit := app.Run([]string{"query", "--db", dbPath, "--q", "x", "--embedding", "not-a-provider", "--mode", "hybrid"})
		if exit != exitCodeInvalidUsage {
			t.Fatalf("expected exit %d got %d", exitCodeInvalidUsage, exit)
		}
	})

	if !strings.Contains(stderr, "unknown embedding provider") || !strings.Contains(stderr, "available:") {
		t.Fatalf("expected an actionable provider error, got %q", stderr)
	}
}

func TestIndexSkipEmbeddingsLeavesNoVectorChannel(t *testing.T) {
	dir := writeIndexFixture(t, "# heading\n\nbanana text")
	app := newTestApp(t)

	output := captureStdout(t, func() {
		if exit := app.Run([]string{"index", dir, "--skip-embeddings", "--json"}); exit != exitCodeOK {
			t.Fatalf("expected exit %d got %d", exitCodeOK, exit)
		}
	})
	if !strings.Contains(output, `"written": 0`) {
		t.Fatalf("expected no embeddings to be written, got %s", output)
	}
	if !strings.Contains(output, `"vectorReady": false`) {
		t.Fatalf("expected an empty vector channel, got %s", output)
	}

	// A vector query against an index without embeddings must explain itself.
	stderr := captureStderr(t, func() {
		exit := app.Run([]string{"query", "--db", filepath.Join(dir, defaultDBFileName), "--q", "banana", "--mode", "vector"})
		if exit != exitCodeInvalidUsage {
			t.Fatalf("expected exit %d got %d", exitCodeInvalidUsage, exit)
		}
	})
	if !strings.Contains(stderr, "holds no embeddings") {
		t.Fatalf("expected a missing embeddings explanation, got %q", stderr)
	}
}

func TestQueryLexicalOnlyNeedsNoProvider(t *testing.T) {
	dir := writeIndexFixture(t, "# heading\n\nbanana text")
	dbPath := filepath.Join(dir, defaultDBFileName)
	app := newTestApp(t)

	captureStdout(t, func() {
		if exit := app.Run([]string{"index", dir, "--skip-embeddings"}); exit != exitCodeOK {
			t.Fatalf("expected index exit %d got %d", exitCodeOK, exit)
		}
	})

	// Lexical-only must work even though the index holds no embeddings.
	output := captureStdout(t, func() {
		if exit := app.Run([]string{"query", "--db", dbPath, "--q", "banana", "--lexical-only"}); exit != exitCodeOK {
			t.Fatalf("expected query exit %d got %d", exitCodeOK, exit)
		}
	})
	if !strings.Contains(output, "Alpha") {
		t.Fatalf("expected a lexical hit, got %q", output)
	}

	// A conflicting mode is rejected instead of being silently ignored.
	stderr := captureStderr(t, func() {
		exit := app.Run([]string{"query", "--db", dbPath, "--q", "banana", "--lexical-only", "--mode", "vector"})
		if exit != exitCodeInvalidUsage {
			t.Fatalf("expected exit %d got %d", exitCodeInvalidUsage, exit)
		}
	})
	if !strings.Contains(stderr, "--lexical-only conflicts with --mode vector") {
		t.Fatalf("expected a conflict explanation, got %q", stderr)
	}
}

// newTestApp returns an app whose commands read an empty configuration file, so a
// developer's own config.yaml cannot change what these tests observe.
// CONFLUENCE2MD_CONFIG takes precedence over the default file name in the working
// directory, which keeps every assertion independent of where the tests run.
func newTestApp(t *testing.T) *App {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "empty-config.yaml")
	if err := os.WriteFile(configPath, []byte("# deliberately empty\n"), 0644); err != nil {
		t.Fatalf("write empty config: %v", err)
	}
	t.Setenv(config.EnvFileName, configPath)

	return NewApp()
}
