package service

import (
	"context"

	"github.com/gkoos/confluence2md-indexer/pkg/indexerapi"
)

type StatsResponse = indexerapi.StatsResponse

func Stats(ctx context.Context, dbPath string) (*StatsResponse, error) {
	return indexerapi.Stats(ctx, dbPath)
}
