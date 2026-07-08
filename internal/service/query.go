package service

import (
	"context"

	"github.com/gkoos/confluence2md-indexer/internal/query"
	"github.com/gkoos/confluence2md-indexer/pkg/indexerapi"
)

const OutputSchemaVersion = indexerapi.OutputSchemaVersion

type QueryResponse = indexerapi.QueryResponse

func Query(ctx context.Context, dbPath string, req query.Request) (*QueryResponse, error) {
	return indexerapi.Query(ctx, dbPath, req)
}
