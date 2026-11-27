package service

import (
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/indexer"
)

func TestCombineCrawlsOfOneSourceIsUnchanged(t *testing.T) {
	one := indexer.CrawlInfo{
		StartedAt: "2026-01-01T10:00:00Z", CompletedAt: "2026-01-01T10:05:00Z",
		SucceededAt: "2026-01-01T10:05:00Z", Mode: "full", SeedCount: 2, PageCount: 30,
	}

	if got := combineCrawls([]indexer.CrawlInfo{one}); got != one {
		t.Fatalf("combined = %+v, want %+v", got, one)
	}
}

func TestCombineCrawlsKeepsTheStaleCheckConservative(t *testing.T) {
	got := combineCrawls([]indexer.CrawlInfo{
		{StartedAt: "2026-01-01T10:00:00Z", CompletedAt: "2026-01-01T10:05:00Z", SucceededAt: "2026-01-01T10:05:00Z", Mode: "full", SeedCount: 1, PageCount: 10},
		{StartedAt: "2026-01-02T08:00:00Z", CompletedAt: "2026-01-02T08:09:00Z", SucceededAt: "2026-01-02T08:09:00Z", Mode: "updates", SeedCount: 3, PageCount: 5},
	})

	if got.CompletedAt != "2026-01-02T08:09:00Z" {
		t.Fatalf("completed = %q, want the latest", got.CompletedAt)
	}
	if got.SucceededAt != "2026-01-01T10:05:00Z" {
		t.Fatalf("succeeded = %q, want the earliest", got.SucceededAt)
	}
	if got.Mode != "mixed" || got.SeedCount != 4 || got.PageCount != 15 {
		t.Fatalf("unexpected totals: %+v", got)
	}
}

func TestCombineCrawlsClaimsNoSuccessWhenOneSourceReportedNone(t *testing.T) {
	got := combineCrawls([]indexer.CrawlInfo{
		{CompletedAt: "2026-01-01T10:05:00Z", SucceededAt: "2026-01-01T10:05:00Z"},
		{CompletedAt: "2026-01-01T11:05:00Z"},
	})

	if got.SucceededAt != "" {
		t.Fatalf("succeeded = %q, want empty", got.SucceededAt)
	}
}

func TestCombineCrawlsComparesTimesNotStrings(t *testing.T) {
	// "...:30Z" sorts after "...:30.5Z" as text but is the earlier time.
	got := combineCrawls([]indexer.CrawlInfo{
		{CompletedAt: "2026-01-01T10:00:30Z"},
		{CompletedAt: "2026-01-01T10:00:30.5Z"},
	})

	if got.CompletedAt != "2026-01-01T10:00:30.5Z" {
		t.Fatalf("completed = %q, want the later time", got.CompletedAt)
	}
}

func TestCombineCrawlsLetsAMalformedCompletionReachTheFreshnessCheck(t *testing.T) {
	for name, crawls := range map[string][]indexer.CrawlInfo{
		"malformed first": {{CompletedAt: "yesterday"}, {CompletedAt: "2026-01-01T10:00:00Z"}},
		"malformed last":  {{CompletedAt: "2026-01-01T10:00:00Z"}, {CompletedAt: "yesterday"}},
	} {
		t.Run(name, func(t *testing.T) {
			combined := combineCrawls(crawls)
			if combined.CompletedAt != "yesterday" {
				t.Fatalf("completed = %q, want the malformed value kept", combined.CompletedAt)
			}

			// The check that gates CI must reject it rather than pass a corrupt record.
			snapshot := &db.CorpusSnapshot{CompletedAt: combined.CompletedAt, IndexedAt: "2026-02-01T00:00:00Z"}
			if _, err := EvaluateFreshness(snapshot); err == nil {
				t.Fatal("a malformed completion must be an error, not fresh")
			}
		})
	}
}
