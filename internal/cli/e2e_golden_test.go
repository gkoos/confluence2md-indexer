package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestE2EGoldenContracts(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	root := t.TempDir()
	copyFixtureCorpus(t, filepath.Join("testdata", "e2e-corpus"), root)

	app := NewApp()

	indexOut := captureStdout(t, func() {
		exit := app.Run([]string{"index", root, "--json"})
		if exit != exitCodeOK {
			t.Fatalf("index exit code: %d", exit)
		}
	})
	assertGoldenJSON(t, "index.golden.json", indexOut, func(payload map[string]any) {
		payload["dbPath"] = "<db-path>"
		payload["inputFolder"] = "<input-folder>"
		payload["metadataPath"] = "<metadata-path>"
		payload["runId"] = "<run-id>"
	})

	queryOut := captureStdout(t, func() {
		exit := app.Run([]string{
			"query",
			"--db", filepath.Join(root, defaultDBFileName),
			"--q", "apple",
			"--mode", "hybrid",
			"--fusion", "weighted",
			"--json",
		})
		if exit != exitCodeOK {
			t.Fatalf("query exit code: %d", exit)
		}
	})
	assertGoldenJSON(t, "query.golden.json", queryOut, func(payload map[string]any) {
		payload["dbPath"] = "<db-path>"
		payload["total"] = "<total>"
	})

	statsOut := captureStdout(t, func() {
		exit := app.Run([]string{"stats", "--db", filepath.Join(root, defaultDBFileName), "--json"})
		if exit != exitCodeOK {
			t.Fatalf("stats exit code: %d", exit)
		}
	})
	assertGoldenJSON(t, "stats.golden.json", statsOut, func(payload map[string]any) {
		payload["dbPath"] = "<db-path>"
	})
}

func copyFixtureCorpus(t *testing.T, sourceDir, targetDir string) {
	t.Helper()

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}

	for _, entry := range entries {
		src := filepath.Join(sourceDir, entry.Name())
		dst := filepath.Join(targetDir, entry.Name())
		if entry.IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				t.Fatalf("create target dir %s: %v", dst, err)
			}
			copyFixtureCorpus(t, src, dst)
			continue
		}

		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read fixture file %s: %v", src, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatalf("write fixture file %s: %v", dst, err)
		}
	}
}

func assertGoldenJSON(t *testing.T, goldenName string, actual string, normalize func(map[string]any)) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(actual), &payload); err != nil {
		t.Fatalf("parse json output: %v\noutput=%s", err, actual)
	}
	normalize(payload)

	formatted, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal normalized payload: %v", err)
	}
	formatted = append(formatted, '\n')

	goldenPath := filepath.Join("testdata", "golden", goldenName)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, formatted, 0o644); err != nil {
			t.Fatalf("update golden file %s: %v", goldenPath, err)
		}
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file %s: %v", goldenPath, err)
	}

	if string(expected) != string(formatted) {
		t.Fatalf("golden mismatch for %s\nexpected:\n%s\nactual:\n%s", goldenName, string(expected), string(formatted))
	}
}
