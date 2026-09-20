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

	// Configuration is validated before the database is opened, so a mistyped
	// provider is reported as a provider problem rather than as a problem with the
	// index. Lexical retrieval needs no provider at all, so provider configuration
	// is only resolved (and validated) for the modes that use vectors. Resolution
	// errors already name the provider and the setting to fix, so they are returned
	// unchanged.
	var provider embedding.Provider
	if req.Mode == "vector" || req.Mode == "hybrid" {
		resolution, err := embedding.Resolve(req.Embedding)
		if err != nil {
			return nil, err
		}
		provider = resolution.Provider
	}

	database, err := db.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = database.Close() }()

	// Refuse a database written by another build instead of failing later with a
	// SQL error about a missing column.
	if err := db.Verify(ctx, database); err != nil {
		return nil, err
	}

	results, total, err := query.Run(ctx, database, provider, req)
	if err != nil {
		return nil, err
	}

	return &QueryResponse{Results: results, Total: total}, nil
}
