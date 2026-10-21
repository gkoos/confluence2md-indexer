package service

import (
	"strings"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/db"
)

func TestEvaluateFreshness(t *testing.T) {
	cases := map[string]struct {
		snapshot *db.CorpusSnapshot
		stale    bool
	}{
		"crawl finished before the run": {
			snapshot: &db.CorpusSnapshot{
				CompletedAt: "2026-05-22T10:20:03Z",
				IndexedAt:   "2026-09-20T19:04:13.5Z",
			},
		},
		"crawl finished after the run": {
			snapshot: &db.CorpusSnapshot{
				CompletedAt: "2026-09-20T19:04:14Z",
				IndexedAt:   "2026-09-20T19:04:13.5Z",
			},
			stale: true,
		},
		// The crawler writes whole seconds and the run records nanoseconds; comparing the
		// strings would order these two the wrong way round.
		"same instant in different formats": {
			snapshot: &db.CorpusSnapshot{
				CompletedAt: "2026-09-20T19:04:13Z",
				IndexedAt:   "2026-09-20T19:04:13.000Z",
			},
		},
		"no completion timestamp": {
			snapshot: &db.CorpusSnapshot{IndexedAt: "2026-09-20T19:04:13Z"},
		},
		"no snapshot": {snapshot: nil},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			freshness, err := EvaluateFreshness(testCase.snapshot)
			if err != nil {
				t.Fatalf("evaluate freshness: %v", err)
			}
			if freshness.Stale != testCase.stale {
				t.Fatalf("stale = %t, want %t", freshness.Stale, testCase.stale)
			}
			err = StaleError(testCase.snapshot, "confluence2md-indexer index ./output --db ./index.db")
			if (err != nil) != testCase.stale {
				t.Fatalf("err = %v, want a failure only when stale", err)
			}
		})
	}
}

func TestStaleErrorNamesTheTimestampsAndTheGivenCommand(t *testing.T) {
	snapshot := &db.CorpusSnapshot{
		CompletedAt: "2026-09-20T19:04:14Z",
		IndexedAt:   "2026-09-20T19:04:13.5Z",
	}
	command := "confluence2md-indexer index '/srv/in put' --db '/srv/state/index.db'"

	err := StaleError(snapshot, command)
	if err == nil {
		t.Fatal("expected a stale error")
	}
	for _, want := range []string{
		snapshot.CompletedAt,
		snapshot.IndexedAt,
		command,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to mention %q", err, want)
		}
	}

	if err := StaleError(nil, command); err != nil {
		t.Fatalf("a missing snapshot must not fail, got %v", err)
	}
}

func TestStaleErrorFallsBackWithoutACommand(t *testing.T) {
	snapshot := &db.CorpusSnapshot{
		CompletedAt: "2026-09-20T19:04:14Z",
		IndexedAt:   "2026-09-20T19:04:13Z",
	}

	err := StaleError(snapshot, "  ")
	if err == nil || !strings.Contains(err.Error(), "confluence2md-indexer index <folder>") {
		t.Fatalf("error = %v, want the fallback command", err)
	}
}

func TestEvaluateFreshnessRejectsAnUnreadableTimestamp(t *testing.T) {
	_, err := EvaluateFreshness(&db.CorpusSnapshot{
		CompletedAt: "yesterday",
		IndexedAt:   "2026-09-20T19:04:13Z",
	})
	if err == nil || !strings.Contains(err.Error(), "crawl completion timestamp") {
		t.Fatalf("error = %v, want a timestamp that names the field", err)
	}

	if _, err := EvaluateFreshness(&db.CorpusSnapshot{CompletedAt: "2026-09-20T19:04:13Z", IndexedAt: "later"}); err == nil {
		t.Fatal("expected an unreadable index timestamp to fail")
	}
}
