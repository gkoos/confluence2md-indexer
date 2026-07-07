package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	app := NewApp()
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

	app := NewApp()
	exit := app.Run([]string{"index", dir, "--rebuild", "--json"})
	if exit != exitCodeOK {
		t.Fatalf("expected exit %d got %d", exitCodeOK, exit)
	}
}

func TestRunIndexPreflightFail(t *testing.T) {
	app := NewApp()
	exit := app.Run([]string{"index", t.TempDir()})
	if exit != exitCodeInvalidUsage {
		t.Fatalf("expected exit %d got %d", exitCodeInvalidUsage, exit)
	}
}

func TestRunQueryValidationFail(t *testing.T) {
	app := NewApp()
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

	app := NewApp()
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

	app := NewApp()
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

	app := NewApp()
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

	app := NewApp()
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
