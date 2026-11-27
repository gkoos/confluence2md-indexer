package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveSourceFromTheCommandLine(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	root := t.TempDir()
	copyFixtureCorpus(t, filepath.Join("testdata", "e2e-corpus"), root)
	dbPath := filepath.Join(t.TempDir(), "index.db")
	app := newTestApp(t)

	_ = captureStdout(t, func() {
		if exit := app.Run([]string{"index", "--source", "docs=" + root, "--db", dbPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("index exit code: %d", exit)
		}
	})

	var out string
	stderr := captureStderr(t, func() {
		out = captureStdout(t, func() {
			if exit := app.Run([]string{"index", "--remove-source", "docs", "--db", dbPath, "--json"}); exit != exitCodeOK {
				t.Fatalf("remove exit code: %d", exit)
			}
		})
	})
	_ = stderr

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if payload["command"] != "remove-source" || payload["sourceId"] != "docs" {
		t.Fatalf("payload = %+v", payload)
	}
	if deleted, _ := payload["deleted"].(float64); deleted < 1 {
		t.Fatalf("deleted = %v, want at least one document", payload["deleted"])
	}
	if remaining, _ := payload["remainingDocuments"].(float64); remaining != 0 {
		t.Fatalf("remaining = %v, want 0", payload["remainingDocuments"])
	}
}

func TestRemoveSourceRefusesIncompatibleArguments(t *testing.T) {
	app := newTestApp(t)

	cases := map[string][]string{
		"no database":    {"index", "--remove-source", "docs"},
		"with a folder":  {"index", "--remove-source", "docs", "--db", "x.db", "some-folder"},
		"with --source":  {"index", "--remove-source", "docs", "--db", "x.db", "--source", "a=b"},
		"with --rebuild": {"index", "--remove-source", "docs", "--db", "x.db", "--rebuild"},
		"unknown index":  {"index", "--remove-source", "docs", "--db", filepath.Join(t.TempDir(), "missing.db")},
	}

	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var exit int
			stderr := captureStderr(t, func() { exit = app.Run(args) })
			if exit != exitCodeInvalidUsage {
				t.Fatalf("exit = %d, want %d (stderr: %s)", exit, exitCodeInvalidUsage, stderr)
			}
			if !strings.Contains(stderr, "index:") {
				t.Fatalf("stderr = %q, want an index: message", stderr)
			}
		})
	}
}

func TestBlankRemoveSourceIsRejectedAndNeverFallsThroughToIndexing(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	root := t.TempDir()
	copyFixtureCorpus(t, filepath.Join("testdata", "e2e-corpus"), root)
	dbPath := filepath.Join(t.TempDir(), "index.db")
	app := newTestApp(t)

	_ = captureStdout(t, func() {
		if exit := app.Run([]string{"index", "--source", "docs=" + root, "--db", dbPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("index exit code: %d", exit)
		}
	})
	before, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat index: %v", err)
	}

	// An empty variable in a script gives a blank value; with --rebuild it must not
	// recreate the index, and without it must not index the working directory.
	for name, args := range map[string][]string{
		"empty":                {"index", "--remove-source", "", "--db", dbPath},
		"whitespace":           {"index", "--remove-source", "  ", "--db", dbPath},
		"empty with --rebuild": {"index", "--remove-source", "", "--db", dbPath, "--rebuild"},
	} {
		t.Run(name, func(t *testing.T) {
			var exit int
			stderr := captureStderr(t, func() { exit = app.Run(args) })
			if exit != exitCodeInvalidUsage || !strings.Contains(stderr, "needs a source id") {
				t.Fatalf("exit = %d, stderr = %q, want a rejection naming the missing id", exit, stderr)
			}

			after, err := os.Stat(dbPath)
			if err != nil || !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
				t.Fatalf("the index must be untouched: %v (before %v, after %v)", err, before.ModTime(), after.ModTime())
			}
		})
	}
}
