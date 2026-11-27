package indexerapi

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gkoos/confluence2md-indexer/internal/db"
)

// Spaces lists the distinct, non-empty space keys an index holds, sorted. A source
// without spaces, such as a folder of plain files, contributes none. It takes the same
// shared lock and runs the same schema check as Stats, so a rebuild in progress reads as
// ErrBusy instead of a half-written file.
func Spaces(ctx context.Context, dbPath string) ([]string, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, fmt.Errorf("spaces requires a non-empty --db path")
	}

	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("resolve db path %q: %w", dbPath, err)
	}

	lock, err := db.AcquireLock(absPath, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Release() }()

	database, err := db.Open(absPath)
	if err != nil {
		return nil, db.WrapBusy(err)
	}
	defer func() { _ = database.Close() }()

	if err := db.Verify(ctx, database); err != nil {
		return nil, db.WrapBusy(err)
	}

	spaces, err := db.DistinctSpaces(ctx, database)
	if err != nil {
		return nil, db.WrapBusy(err)
	}

	return spaces, nil
}
