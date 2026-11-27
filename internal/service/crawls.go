package service

import (
	"strings"
	"time"

	"github.com/gkoos/confluence2md-indexer/internal/indexer"
)

// combineCrawls folds the crawl each source reported into the single snapshot an index
// stores, in the way that keeps the stale check conservative: the index is stale when
// any source's crawl finished after the run started, so the completion is the latest
// one, and a last-success time is only claimed when every source reported one.
func combineCrawls(crawls []indexer.CrawlInfo) indexer.CrawlInfo {
	var combined indexer.CrawlInfo
	allSucceeded := true
	succeededSeen := false
	modes := map[string]bool{}

	for _, crawl := range crawls {
		combined.SeedCount += crawl.SeedCount
		combined.PageCount += crawl.PageCount

		combined.StartedAt = laterTimestamp(combined.StartedAt, crawl.StartedAt)
		combined.CompletedAt = laterTimestamp(combined.CompletedAt, crawl.CompletedAt)

		if strings.TrimSpace(crawl.SucceededAt) == "" {
			allSucceeded = false
		} else {
			combined.SucceededAt = earlierTimestamp(combined.SucceededAt, crawl.SucceededAt)
			succeededSeen = true
		}

		if mode := strings.TrimSpace(crawl.Mode); mode != "" {
			modes[mode] = true
		}
	}

	if !allSucceeded || !succeededSeen {
		combined.SucceededAt = ""
	}
	switch len(modes) {
	case 0:
	case 1:
		for mode := range modes {
			combined.Mode = mode
		}
	default:
		combined.Mode = "mixed"
	}

	return combined
}

// laterTimestamp and earlierTimestamp compare as times, because the crawler writes
// seconds and a run writes nanoseconds. A value that is not a timestamp wins over one
// that is, so a corrupt record from any source reaches the freshness check, which rejects
// it, instead of being hidden by another source. The result does not depend on order.
func laterTimestamp(a, b string) string {
	return pickTimestamp(a, b, func(x, y time.Time) bool { return y.After(x) })
}

func earlierTimestamp(a, b string) string {
	return pickTimestamp(a, b, func(x, y time.Time) bool { return y.Before(x) })
}

func pickTimestamp(a, b string, preferB func(x, y time.Time) bool) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}

	ta, errA := time.Parse(time.RFC3339Nano, a)
	tb, errB := time.Parse(time.RFC3339Nano, b)
	switch {
	case errA != nil && errB != nil:
		return min(a, b)
	case errA != nil:
		return a
	case errB != nil:
		return b
	case preferB(ta, tb):
		return b
	default:
		return a
	}
}
