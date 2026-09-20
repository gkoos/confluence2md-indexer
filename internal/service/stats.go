package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/gkoos/confluence2md-indexer/internal/db"
)

type StatsResponse struct {
	Stats *db.Stats
}

func Stats(ctx context.Context, dbPath string) (*StatsResponse, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, fmt.Errorf("stats requires a non-empty --db path")
	}

	database, err := db.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = database.Close() }()

	// Report a schema this build cannot read before touching any table.
	if err := db.Verify(ctx, database); err != nil {
		return nil, err
	}

	stats, err := db.GetStats(ctx, database)
	if err != nil {
		return nil, err
	}

	return &StatsResponse{Stats: stats}, nil
}
