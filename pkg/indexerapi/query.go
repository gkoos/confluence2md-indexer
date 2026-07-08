package indexerapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/embedding"
	"github.com/gkoos/confluence2md-indexer/internal/query"
)

const OutputSchemaVersion = "1"

type QueryRequest = query.Request
type QueryResult = query.Result

type QueryResponse struct {
	Results []QueryResult
	Total   int
}

func Query(ctx context.Context, dbPath string, req QueryRequest) (*QueryResponse, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, fmt.Errorf("query requires a non-empty --db path")
	}

	database, err := db.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = database.Close() }()

	provider := embedding.NewDefaultFromEnv().Provider
	results, total, err := query.Run(ctx, database, provider, req)
	if err != nil {
		return nil, err
	}

	return &QueryResponse{Results: results, Total: total}, nil
}