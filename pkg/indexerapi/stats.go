package indexerapi

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gkoos/confluence2md-indexer/internal/db"
)

// IndexStats is the index summary the stats command reports. It is aliased here so a
// caller of this package can name the type without importing an internal package.
type IndexStats = db.Stats

// StatsResponse mirrors the stats --json payload field for field, including the envelope
// the command adds, so marshalling it produces the same document the CLI prints for the
// same database.
type StatsResponse struct {
	SchemaVersion string      `json:"schemaVersion"`
	Command       string      `json:"command"`
	DBPath        string      `json:"dbPath"`
	Stats         *IndexStats `json:"stats"`
}

// Stats reports an index's size, freshness, metadata coverage and schema state without
// going through the CLI. It validates the database the same way the stats command does,
// so a schema mismatch or a file that holds no index fails with the same wording.
func Stats(ctx context.Context, dbPath string) (*StatsResponse, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, fmt.Errorf("stats requires a non-empty --db path")
	}

	// Open through an absolute path so the database resolves against the working
	// directory once, but report the path the caller supplied: the command echoes its
	// --db value (or its configured default), and rewriting it here would change the
	// payload the docs promise is identical.
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("resolve db path %q: %w", dbPath, err)
	}

	database, err := db.Open(absPath)
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

	// Crawl freshness and metadata coverage belong to this report: index output stays
	// lean, and stats is where an operator looks for them.
	coverage, err := db.DocumentMetadataCoverage(ctx, database)
	if err != nil {
		return nil, err
	}
	stats.Metadata = coverage

	snapshot, err := db.LatestCorpusSnapshot(ctx, database)
	if err != nil {
		return nil, err
	}
	stats.Corpus = snapshot

	return &StatsResponse{
		SchemaVersion: OutputSchemaVersion,
		Command:       "stats",
		DBPath:        dbPath,
		Stats:         stats,
	}, nil
}
