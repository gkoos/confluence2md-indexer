package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexProgressGoesToStderrAndLeavesStdoutAlone(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	root := t.TempDir()
	copyFixtureCorpus(t, filepath.Join("testdata", "e2e-corpus"), root)
	app := newTestApp(t)

	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			if exit := app.Run([]string{"index", root, "--json", "--progress"}); exit != exitCodeOK {
				t.Fatalf("index exit code: %d", exit)
			}
		})
	})

	// Stdout is still the single result document.
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}

	var events []map[string]any
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.HasPrefix(line, "{") {
			continue // the version line is the one line that is not an event
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("progress line is not JSON: %q: %v", line, err)
		}
		events = append(events, event)
	}

	if len(events) < 3 {
		t.Fatalf("got %d events, want start, documents and finished: %q", len(events), stderr)
	}
	if events[0]["event"] != "start" || events[len(events)-1]["event"] != "finished" {
		t.Fatalf("events must open with start and end with finished: %+v", events)
	}
}

func TestIndexWithoutProgressWritesNoEvents(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	root := t.TempDir()
	copyFixtureCorpus(t, filepath.Join("testdata", "e2e-corpus"), root)
	app := newTestApp(t)

	stderr := captureStderr(t, func() {
		_ = captureStdout(t, func() {
			if exit := app.Run([]string{"index", root, "--json"}); exit != exitCodeOK {
				t.Fatalf("index exit code: %d", exit)
			}
		})
	})

	if strings.Contains(stderr, `"event"`) {
		t.Fatalf("progress events appeared without --progress: %q", stderr)
	}
}
