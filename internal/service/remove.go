package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/gkoos/confluence2md-indexer/internal/db"
)

// RemoveSourceResponse reports what removing a source did.
type RemoveSourceResponse struct {
	DBPath             string
	SourceID           string
	Deleted            int
	RemainingDocuments int
}

// RemoveSource deletes every document one source contributed, with its chunks, vectors and
// full-text rows, and leaves the other sources alone. It reads no folder: the index is
// the only thing it touches, so a source can be dropped after its folder is gone.
//
// A source id the index does not hold is an error that lists the ids it does hold, so a
// typo is not mistaken for a successful removal.
func RemoveSource(ctx context.Context, dbPath string, sourceID string) (*RemoveSourceResponse, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, fmt.Errorf("remove-source requires a non-empty --db path")
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil, fmt.Errorf("remove-source requires a source id")
	}

	// Opening a missing file would create an empty index, so refuse it first.
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("index %s does not exist", dbPath)
		}
		return nil, fmt.Errorf("read index %s: %w", dbPath, err)
	}

	// Removing documents is a write like an incremental run, so it shares the index with
	// queries and with other incremental runs; only a rebuild needs it exclusively.
	lock, err := db.AcquireLock(dbPath, false)
	if err != nil {
		return nil, fmt.Errorf("index lock failed: %w", err)
	}
	defer func() { _ = lock.Release() }()

	database, err := db.Open(dbPath)
	if err != nil {
		return nil, db.WrapBusy(err)
	}
	defer func() { _ = database.Close() }()

	if err := db.Verify(ctx, database); err != nil {
		return nil, db.WrapBusy(err)
	}

	// The check, the deletion and the remaining count all happen in one transaction, so a
	// concurrent removal cannot change what this one reports.
	removal, err := db.RemoveSourceDocuments(ctx, database, sourceID)
	if errors.Is(err, db.ErrSourceNotFound) {
		return nil, fmt.Errorf("index holds no documents from source %q%s", sourceID, describeSources(removal.Known))
	}
	if err != nil {
		return nil, db.WrapBusy(err)
	}

	return &RemoveSourceResponse{DBPath: dbPath, SourceID: sourceID, Deleted: int(removal.Deleted), RemainingDocuments: int(removal.Remaining)}, nil
}

// describeSources renders the sources an index holds for an error message.
func describeSources(counts map[string]int) string {
	if len(counts) == 0 {
		return " (the index is empty)"
	}

	ids := make([]string, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	return " (it holds: " + strings.Join(ids, ", ") + ")"
}
