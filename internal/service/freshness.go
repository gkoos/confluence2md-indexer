package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/gkoos/confluence2md-indexer/internal/db"
)

// reindexCommand is what a stale index needs. It is deliberately not --rebuild: an
// incremental run already picks up the crawl that moved on, while a rebuild re-embeds the
// whole corpus for no freshness benefit.
const reindexCommand = "confluence2md-indexer index <folder>"

// Freshness is the comparison --fail-if-stale exposes: the crawl an index recorded
// completed after the run that recorded it started, which means that run read a corpus
// that was still moving.
type Freshness struct {
	Stale       bool
	IndexedAt   string
	CompletedAt string
}

// EvaluateFreshness compares the crawl a snapshot recorded with the run that wrote it.
//
// A nil snapshot, or one without a crawl completion timestamp, is not stale: there is
// nothing to compare, which is also what an index built from a crawler that reports no
// timestamps looks like. A malformed timestamp is an error rather than "fresh", so a
// corrupt record cannot pass a CI gate silently.
//
// The comparison only sees what the index stored. A crawl that happened entirely after
// the index run leaves the stored timestamps untouched, so this detects a run that raced
// the crawler, not a folder that has moved on since.
func EvaluateFreshness(snapshot *db.CorpusSnapshot) (Freshness, error) {
	if snapshot == nil {
		return Freshness{}, nil
	}

	freshness := Freshness{
		CompletedAt: strings.TrimSpace(snapshot.CompletedAt),
		IndexedAt:   strings.TrimSpace(snapshot.IndexedAt),
	}
	if freshness.CompletedAt == "" || freshness.IndexedAt == "" {
		return freshness, nil
	}

	completed, err := time.Parse(time.RFC3339, freshness.CompletedAt)
	if err != nil {
		return Freshness{}, fmt.Errorf("read crawl completion timestamp %q: %w", freshness.CompletedAt, err)
	}
	indexed, err := time.Parse(time.RFC3339, freshness.IndexedAt)
	if err != nil {
		return Freshness{}, fmt.Errorf("read index timestamp %q: %w", freshness.IndexedAt, err)
	}

	// Timestamps are stored in different formats (the crawler writes seconds, the run
	// records nanoseconds), so they are compared as times and never as strings.
	freshness.Stale = completed.After(indexed)

	return freshness, nil
}

// Err renders the failure both commands report, or nil when the index is fresh.
func (f Freshness) Err() error {
	if !f.Stale {
		return nil
	}

	return fmt.Errorf(
		"index is stale: the crawl completed at %s, after the index run started at %s; re-index it with: %s",
		f.CompletedAt, f.IndexedAt, reindexCommand,
	)
}
