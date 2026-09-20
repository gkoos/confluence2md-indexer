package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

const initSchemaSQL = `
CREATE TABLE IF NOT EXISTS runs (
  id TEXT PRIMARY KEY,
  started_at TEXT NOT NULL,
  completed_at TEXT,
  mode TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS documents (
  id TEXT PRIMARY KEY,
  page_id TEXT NOT NULL,
  title TEXT NOT NULL,
  local_path TEXT NOT NULL,
  space_key TEXT NOT NULL DEFAULT '',
  source_url TEXT NOT NULL DEFAULT '',
  canonical_url TEXT NOT NULL DEFAULT '',
  host TEXT NOT NULL DEFAULT '',
  last_modified_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT '',
  crawled_at TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL DEFAULT 0,
  depth INTEGER NOT NULL DEFAULT 0,
  parent_id TEXT NOT NULL DEFAULT '',
  created_by_name TEXT NOT NULL DEFAULT '',
  modified_by_name TEXT NOT NULL DEFAULT '',
  is_seed INTEGER NOT NULL DEFAULT 0,
  link_in INTEGER NOT NULL DEFAULT 0,
  link_out INTEGER NOT NULL DEFAULT 0,
  attachment_count INTEGER NOT NULL DEFAULT 0,
  comment_count INTEGER NOT NULL DEFAULT 0,
  content_hash TEXT NOT NULL,
  metadata_hash TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS chunks (
  id TEXT PRIMARY KEY,
  document_id TEXT NOT NULL,
  chunk_index INTEGER NOT NULL,
  text TEXT NOT NULL,
  chunk_hash TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(document_id) REFERENCES documents(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS embeddings (
  chunk_id TEXT NOT NULL,
  name TEXT NOT NULL,
  dimension INTEGER NOT NULL,
  vector BLOB NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (chunk_id, name),
  FOREIGN KEY(chunk_id) REFERENCES chunks(id) ON DELETE CASCADE
);

CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
  chunk_id UNINDEXED,
  text,
  title,
  section
);

CREATE TABLE IF NOT EXISTS embedding_runs (
  run_id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  dimension INTEGER NOT NULL,
  capability TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS corpus_snapshot (
  run_id TEXT PRIMARY KEY,
  crawl_started_at TEXT NOT NULL DEFAULT '',
  crawl_completed_at TEXT NOT NULL DEFAULT '',
  crawl_succeeded_at TEXT NOT NULL DEFAULT '',
  crawl_mode TEXT NOT NULL DEFAULT '',
  seed_count INTEGER NOT NULL DEFAULT 0,
  page_count INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_documents_space_key ON documents(space_key);
CREATE INDEX IF NOT EXISTS idx_documents_page_id ON documents(page_id);
CREATE INDEX IF NOT EXISTS idx_documents_last_modified_at ON documents(last_modified_at);
CREATE INDEX IF NOT EXISTS idx_documents_host ON documents(host);
CREATE INDEX IF NOT EXISTS idx_documents_depth ON documents(depth);
CREATE INDEX IF NOT EXISTS idx_documents_is_seed ON documents(is_seed);
CREATE INDEX IF NOT EXISTS idx_documents_created_by_name ON documents(created_by_name COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_documents_modified_by_name ON documents(modified_by_name COLLATE NOCASE);
`

// candidateColumns is the projection both search paths scan into a Candidate. It is
// paired with scanCandidate, so the two stay in step.
const candidateColumns = `c.id, c.document_id, d.page_id, d.title, d.local_path, d.space_key, d.source_url,
d.last_modified_at, c.text, c.chunk_index, d.host, d.canonical_url, d.depth, d.is_seed, d.created_by_name,
d.modified_by_name, d.link_in, d.attachment_count, d.comment_count`

// documentColumns lists the writable crawler metadata columns of a document row, in
// the order documentValues binds them. One list keeps the insert and the two update
// statements from drifting apart.
const documentColumns = `page_id, title, local_path, space_key, source_url, canonical_url, host,
last_modified_at, created_at, crawled_at, version, depth, parent_id, created_by_name, modified_by_name,
is_seed, link_in, link_out, attachment_count, comment_count`

// schemaVersion identifies the schema this build writes. A database stamped with
// another version is refused rather than upgraded in place: rebuilding from the
// crawler output is deterministic, costs no embedding calls for unchanged text, and
// is the only path that populates every column.
const schemaVersion = 1

// rebuildHint is the command that fixes every schema mismatch.
const rebuildHint = "confluence2md-indexer index <folder> --rebuild"

// ftsBM25Weights weights the chunks_fts columns for ranking. The first entry
// belongs to the UNINDEXED chunk_id column and is ignored by SQLite: chunk text
// keeps the baseline, a title match counts four times as much, and the heading
// breadcrumb twice. Page id and space key are deliberately not indexed any more;
// they are filters, and indexing them made a space key match every page in that
// space.
const ftsBM25Weights = "1.0, 1.0, 4.0, 2.0"

type Run struct {
	ID          string
	StartedAt   time.Time
	CompletedAt *time.Time
	Mode        string
}

// Stats summarises the index for the stats command.
type Stats struct {
	Runs       int `json:"runs"`
	Documents  int `json:"documents"`
	Chunks     int `json:"chunks"`
	Embeddings int `json:"embeddings"`
	// VectorReady reports whether a usable vector channel exists in this index.
	VectorReady bool `json:"vectorReady"`
	// VectorName is the embedding identity stored in the index, empty when the
	// index holds no embeddings.
	VectorName string `json:"vectorName"`
	// VectorCapability describes the most recent embedding run
	// (semantic, lexical or none).
	VectorCapability string `json:"vectorCapability"`
	// EmbeddingModels lists the distinct identities and sizes present.
	EmbeddingModels []EmbeddingModelStat `json:"embeddingModels"`
	// Metadata counts what the stored documents carry, so an operator can see whether
	// the crawler output actually provided it.
	Metadata *MetadataCoverage `json:"metadata,omitempty"`
	// Corpus reports the crawl the index was built from, when a run recorded one.
	Corpus *CorpusSnapshot `json:"corpus,omitempty"`
}

// MetadataCoverage counts the documents that carry a metadata value.
//
// Zero means "none, or not reported": a crawler that does not write a field leaves it
// empty, so these are counts rather than availability percentages.
type MetadataCoverage struct {
	Documents       int `json:"documents"`
	WithAuthors     int `json:"withAuthors"`
	WithLinks       int `json:"withLinks"`
	WithAttachments int `json:"withAttachments"`
	WithComments    int `json:"withComments"`
	Seeds           int `json:"seeds"`
	// Nested counts pages below the seed level, which is the only depth distinction the
	// stored value supports: depth zero is a seed, or a crawler that reported nothing.
	Nested int `json:"nested"`
	Hosts  int `json:"hosts"`
}

// EmbeddingModelStat counts stored vectors per identity.
type EmbeddingModelStat struct {
	Name      string `json:"name"`
	Dimension int    `json:"dimension"`
	Chunks    int    `json:"chunks"`
}

// Manifest describes the embeddings actually stored in an index. It is the
// authority for detecting a provider that no longer matches the stored vectors.
type Manifest struct {
	// Names holds the distinct embedding identities present, sorted.
	Names []string
	// Chunks is the total number of stored vectors.
	Chunks int
	// Models lists per-identity counts.
	Models []EmbeddingModelStat
	// Capability comes from the most recent embedding run, empty when unknown.
	Capability string
}

// SingleName returns the stored identity when the index holds exactly one, and
// an empty string when the index is empty or mixes identities.
func (m *Manifest) SingleName() string {
	if m == nil || len(m.Names) != 1 {
		return ""
	}
	return m.Names[0]
}

type DocumentRecord struct {
	ID             string
	PageID         string
	Title          string
	LocalPath      string
	SpaceKey       string
	SourceURL      string
	LastModifiedAt string
	ContentHash    string
	// MetadataHash fingerprints the crawler metadata, so a run can tell an unchanged
	// page from one whose metadata alone moved.
	MetadataHash   string
	Host           string
	CanonicalURL   string
	Version        int
	Depth          int
	ParentID       string
	CreatedAt      string
	CrawledAt      string
	CreatedByName  string
	ModifiedByName string
	IsSeed         bool
	LinkIn         int
	LinkOut        int
	Attachments    int
	Comments       int
}

// documentValues returns the bound values that match documentColumns.
func (doc DocumentRecord) documentValues() []any {
	return []any{
		doc.PageID, doc.Title, doc.LocalPath, doc.SpaceKey, doc.SourceURL, doc.CanonicalURL, doc.Host,
		doc.LastModifiedAt, doc.CreatedAt, doc.CrawledAt, doc.Version, doc.Depth, doc.ParentID,
		doc.CreatedByName, doc.ModifiedByName, doc.IsSeed, doc.LinkIn, doc.LinkOut, doc.Attachments,
		doc.Comments,
	}
}

// CorpusSnapshot is the crawl a stored index was built from.
type CorpusSnapshot struct {
	RunID       string `json:"runId"`
	StartedAt   string `json:"crawlStartedAt"`
	CompletedAt string `json:"crawlCompletedAt"`
	SucceededAt string `json:"crawlSucceededAt"`
	Mode        string `json:"crawlMode"`
	SeedCount   int    `json:"seedCount"`
	PageCount   int    `json:"pageCount"`
	// IndexedAt is when the run behind this snapshot started. It is filled when the
	// snapshot is read from an index and empty when one is being recorded.
	IndexedAt string `json:"indexedAt,omitempty"`
}

type ChunkRecord struct {
	ID         string
	ChunkIndex int
	Text       string
	ChunkHash  string
	// Section is the heading breadcrumb the chunk sits under ("Deployment >
	// Rollback"), indexed for matching but not shown in results.
	Section string
}

type ChunkWindowItem struct {
	ChunkIndex int
	Text       string
}

// EmbeddingRecord is one stored vector, keyed by chunk and embedding identity.
type EmbeddingRecord struct {
	ChunkID   string
	Name      string
	Dimension int
	Vector    []float32
}

type Candidate struct {
	ChunkID        string
	DocumentID     string
	PageID         string
	Title          string
	LocalPath      string
	SpaceKey       string
	SourceURL      string
	LastModifiedAt string
	ChunkText      string
	ChunkIndex     int
	// Metadata travels with every candidate so filters can be applied in SQL and
	// ranking priors can read it without a second query.
	Host           string
	CanonicalURL   string
	Depth          int
	IsSeed         bool
	CreatedByName  string
	ModifiedByName string
	LinkIn         int
	Attachments    int
	Comments       int

	LexicalScoreRaw float64
	VectorScoreRaw  float64
}

type SearchFilters struct {
	SpaceKey string
	// Spaces narrows to any of several spaces and wins over SpaceKey when both are
	// set, because a caller that fills both means "these spaces".
	Spaces   []string `json:",omitempty"`
	PageID   string
	FromDate string
	ToDate   string
	// Host narrows to one crawled site, which matters for corpora that span hosts,
	// where space keys can repeat.
	Host string `json:",omitempty"`
	// Author matches the creator or the last modifier, ignoring case.
	Author string `json:",omitempty"`
	// CreatedBy and ModifiedBy match one of those fields on its own.
	CreatedBy  string `json:",omitempty"`
	ModifiedBy string `json:",omitempty"`
	// DepthMin and DepthMax bound the crawl depth. Nil means unbounded; depth 0 is a
	// seed page, so DepthMin of 1 excludes seeds.
	DepthMin *int `json:",omitempty"`
	DepthMax *int `json:",omitempty"`
	// SeedOnly keeps the pages the crawl was started from.
	SeedOnly bool `json:",omitempty"`
	// HasAttachments keeps pages that carry at least one attachment.
	HasAttachments bool `json:",omitempty"`
	// UpdatedSince is an absolute lower bound for last_modified_at. The command layer
	// computes it from a relative age such as 30d, so the filter stays comparable to
	// the stored timestamp strings.
	UpdatedSince string `json:",omitempty"`

	Candidate int
	// EmbeddingName restricts vector search to one embedding identity. It is set
	// by the query pipeline after the provider has been checked against the
	// identities actually stored in the index, so it is not part of the request
	// contract.
	EmbeddingName string `json:"-"`
}

// filterClauses turns search filters into SQL predicates and their arguments. Both
// search paths share it, so a new filter applies to lexical and vector retrieval at
// once and the two cannot drift apart.
//
// A filter left at its zero value adds no predicate, which is why the new metadata
// filters are opt-in: an index built before those columns existed behaves exactly as
// it did before.
func filterClauses(filters SearchFilters) ([]string, []any) {
	where := []string{"1=1"}
	args := make([]any, 0, 8)

	spaces := compactValues(filters.Spaces)
	if len(spaces) == 0 {
		spaces = compactValues([]string{filters.SpaceKey})
	}
	switch len(spaces) {
	case 0:
	case 1:
		where = append(where, "d.space_key = ?")
		args = append(args, spaces[0])
	default:
		where = append(where, "d.space_key IN ("+questionMarks(len(spaces))+")")
		for _, space := range spaces {
			args = append(args, space)
		}
	}

	for _, predicate := range []struct {
		column string
		value  string
	}{
		{"d.page_id", filters.PageID},
		{"d.host", filters.Host},
	} {
		if value := strings.TrimSpace(predicate.value); value != "" {
			where = append(where, predicate.column+" = ?")
			args = append(args, value)
		}
	}

	for _, predicate := range []struct {
		column string
		value  string
	}{
		{"d.created_by_name", filters.CreatedBy},
		{"d.modified_by_name", filters.ModifiedBy},
	} {
		if value := strings.TrimSpace(predicate.value); value != "" {
			where = append(where, predicate.column+" = ? COLLATE NOCASE")
			args = append(args, value)
		}
	}

	if author := strings.TrimSpace(filters.Author); author != "" {
		where = append(where, "(d.created_by_name = ? COLLATE NOCASE OR d.modified_by_name = ? COLLATE NOCASE)")
		args = append(args, author, author)
	}

	if filters.DepthMin != nil {
		where = append(where, "d.depth >= ?")
		args = append(args, *filters.DepthMin)
	}
	if filters.DepthMax != nil {
		where = append(where, "d.depth <= ?")
		args = append(args, *filters.DepthMax)
	}
	if filters.SeedOnly {
		where = append(where, "d.is_seed = 1")
	}
	if filters.HasAttachments {
		where = append(where, "d.attachment_count > 0")
	}

	if value := strings.TrimSpace(filters.FromDate); value != "" {
		where = append(where, "d.last_modified_at >= ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filters.UpdatedSince); value != "" {
		where = append(where, "d.last_modified_at >= ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filters.ToDate); value != "" {
		where = append(where, "d.last_modified_at <= ?")
		args = append(args, value+"T23:59:59Z")
	}

	return where, args
}

// compactValues trims the values and drops the ones that carry nothing, so a filter
// list that was populated with blanks cannot narrow a search.
func compactValues(values []string) []string {
	compacted := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			compacted = append(compacted, trimmed)
		}
	}

	return compacted
}

func Open(path string) (*sql.DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("db path is empty")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve db path %q: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return nil, fmt.Errorf("create db parent directory: %w", err)
	}

	database, err := sql.Open("sqlite", filepath.ToSlash(absPath))
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	if _, err := database.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("ping sqlite db: %w", err)
	}

	return database, nil
}

// Migrate creates the schema, stamps its version, and refuses a database written
// by another build instead of upgrading it in place.
func Migrate(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return fmt.Errorf("database is nil")
	}

	version, err := schemaVersionOf(ctx, database)
	if err != nil {
		return err
	}
	if version == schemaVersion {
		return nil
	}

	tables, err := tableCount(ctx, database)
	if err != nil {
		return err
	}

	// An empty file is a fresh index; --rebuild deletes the file, so this is the
	// path every rebuild takes.
	if version == 0 && tables == 0 {
		if _, err := database.ExecContext(ctx, initSchemaSQL); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
		if _, err := database.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d;", schemaVersion)); err != nil {
			return fmt.Errorf("record schema version: %w", err)
		}
		return nil
	}

	return schemaRefusalError(version)
}

// Verify refuses a database this build cannot read, so a command reports the
// mismatch instead of failing later with a SQL error about a missing column. Read
// paths call it right after Open; Migrate covers the writing path.
func Verify(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return fmt.Errorf("database is nil")
	}

	version, err := schemaVersionOf(ctx, database)
	if err != nil {
		return err
	}
	if version == schemaVersion {
		return nil
	}

	tables, err := tableCount(ctx, database)
	if err != nil {
		return err
	}

	if version == 0 && tables == 0 {
		return fmt.Errorf("this file holds no index yet; build one with: confluence2md-indexer index <folder>")
	}

	return schemaRefusalError(version)
}

// schemaVersionOf reads PRAGMA user_version, which is zero for files written
// before the schema was versioned.
func schemaVersionOf(ctx context.Context, database *sql.DB) (int, error) {
	var version int
	if err := database.QueryRowContext(ctx, "PRAGMA user_version;").Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

// tableCount reports how many tables a file holds, which distinguishes an empty
// file from a populated one with an unversioned schema.
func tableCount(ctx context.Context, database *sql.DB) (int, error) {
	var count int
	const q = `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`
	if err := database.QueryRowContext(ctx, q).Scan(&count); err != nil {
		return 0, fmt.Errorf("inspect schema: %w", err)
	}
	return count, nil
}

// schemaRefusalError explains how to move forward. Rebuilding is the supported
// upgrade path, so the message names the command rather than a migration step.
func schemaRefusalError(version int) error {
	if version > schemaVersion {
		return fmt.Errorf(
			"database schema %d is newer than the schema this build writes (%d); upgrade confluence2md-indexer",
			version, schemaVersion,
		)
	}

	return fmt.Errorf(
		"database schema %d is not the schema this build writes (%d); rebuild the index with: %s",
		version, schemaVersion, rebuildHint,
	)
}

func BeginRun(ctx context.Context, database *sql.DB, mode string) (*Run, error) {
	if database == nil {
		return nil, fmt.Errorf("database is nil")
	}

	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "incremental"
	}

	id, err := randomID(8)
	if err != nil {
		return nil, err
	}

	run := &Run{ID: id, StartedAt: time.Now().UTC(), Mode: mode}

	const q = `INSERT INTO runs(id, started_at, mode) VALUES(?, ?, ?)`
	if _, err := database.ExecContext(ctx, q, run.ID, run.StartedAt.Format(time.RFC3339Nano), run.Mode); err != nil {
		return nil, fmt.Errorf("insert run record: %w", err)
	}

	return run, nil
}

func CompleteRun(ctx context.Context, database *sql.DB, runID string) error {
	if database == nil {
		return fmt.Errorf("database is nil")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("run id is empty")
	}

	completed := time.Now().UTC().Format(time.RFC3339Nano)
	const q = `UPDATE runs SET completed_at = ? WHERE id = ?`
	res, err := database.ExecContext(ctx, q, completed, runID)
	if err != nil {
		return fmt.Errorf("complete run record: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for run completion: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("run id not found: %s", runID)
	}

	return nil
}

func GetStats(ctx context.Context, database *sql.DB) (*Stats, error) {
	if database == nil {
		return nil, fmt.Errorf("database is nil")
	}

	stats := &Stats{EmbeddingModels: []EmbeddingModelStat{}}

	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs`).Scan(&stats.Runs); err != nil {
		return nil, fmt.Errorf("query runs count: %w", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents`).Scan(&stats.Documents); err != nil {
		return nil, fmt.Errorf("query documents count: %w", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&stats.Chunks); err != nil {
		return nil, fmt.Errorf("query chunks count: %w", err)
	}

	manifest, err := EmbeddingManifest(ctx, database)
	if err != nil {
		return nil, err
	}

	stats.Embeddings = manifest.Chunks
	stats.EmbeddingModels = manifest.Models
	stats.VectorName = manifest.SingleName()
	stats.VectorReady = stats.VectorName != ""
	stats.VectorCapability = manifest.Capability

	return stats, nil
}

// DocumentMetadataCoverage aggregates the crawler metadata the index holds.
func DocumentMetadataCoverage(ctx context.Context, database *sql.DB) (*MetadataCoverage, error) {
	if database == nil {
		return nil, fmt.Errorf("database is nil")
	}

	const q = `
SELECT COUNT(*),
  COALESCE(SUM(CASE WHEN created_by_name != '' OR modified_by_name != '' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN link_in > 0 OR link_out > 0 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN attachment_count > 0 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN comment_count > 0 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN is_seed = 1 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN depth > 0 THEN 1 ELSE 0 END), 0),
  COUNT(DISTINCT CASE WHEN host != '' THEN host END)
FROM documents`

	coverage := &MetadataCoverage{}
	if err := database.QueryRowContext(ctx, q).Scan(
		&coverage.Documents,
		&coverage.WithAuthors,
		&coverage.WithLinks,
		&coverage.WithAttachments,
		&coverage.WithComments,
		&coverage.Seeds,
		&coverage.Nested,
		&coverage.Hosts,
	); err != nil {
		return nil, fmt.Errorf("query metadata coverage: %w", err)
	}

	return coverage, nil
}

// LatestCorpusSnapshot returns the crawl of the most recent index run that recorded
// one, and nil when no run did. The reported IndexedAt is when that run started, which
// is what makes staleness comparable: a crawl completed after the index was written
// means the corpus has moved on since.
func LatestCorpusSnapshot(ctx context.Context, database *sql.DB) (*CorpusSnapshot, error) {
	if database == nil {
		return nil, fmt.Errorf("database is nil")
	}

	const q = `
SELECT s.run_id, s.crawl_started_at, s.crawl_completed_at, s.crawl_succeeded_at, s.crawl_mode,
       s.seed_count, s.page_count, r.started_at
FROM corpus_snapshot s
JOIN runs r ON r.id = s.run_id
ORDER BY r.started_at DESC, s.updated_at DESC
LIMIT 1`

	snapshot := &CorpusSnapshot{}
	err := database.QueryRowContext(ctx, q).Scan(
		&snapshot.RunID,
		&snapshot.StartedAt,
		&snapshot.CompletedAt,
		&snapshot.SucceededAt,
		&snapshot.Mode,
		&snapshot.SeedCount,
		&snapshot.PageCount,
		&snapshot.IndexedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query latest corpus snapshot: %w", err)
	}

	return snapshot, nil
}

// EmbeddingManifest reports which embeddings are stored so callers can detect
// that the configured provider no longer matches the index.
func EmbeddingManifest(ctx context.Context, database *sql.DB) (*Manifest, error) {
	if database == nil {
		return nil, fmt.Errorf("database is nil")
	}

	manifest := &Manifest{Models: []EmbeddingModelStat{}, Names: []string{}}

	rows, err := database.QueryContext(ctx, `
SELECT name, dimension, COUNT(*)
FROM embeddings
GROUP BY name, dimension
ORDER BY name ASC, dimension ASC`)
	if err != nil {
		return nil, fmt.Errorf("query embedding manifest: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	names := make([]string, 0, 4)
	for rows.Next() {
		var model EmbeddingModelStat
		if err := rows.Scan(&model.Name, &model.Dimension, &model.Chunks); err != nil {
			return nil, fmt.Errorf("scan embedding manifest: %w", err)
		}
		manifest.Models = append(manifest.Models, model)
		manifest.Chunks += model.Chunks
		if len(names) == 0 || names[len(names)-1] != model.Name {
			names = append(names, model.Name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate embedding manifest: %w", err)
	}
	manifest.Names = names

	var capability string
	err = database.QueryRowContext(ctx, `SELECT capability FROM embedding_runs ORDER BY updated_at DESC LIMIT 1`).Scan(&capability)
	switch {
	case err == nil:
		manifest.Capability = capability
	case errors.Is(err, sql.ErrNoRows):
		// No embedding run recorded yet; the index predates run tracking.
	default:
		return nil, fmt.Errorf("query embedding runs: %w", err)
	}

	return manifest, nil
}

// RecordEmbeddingRun stores which identity produced the vectors of a run.
func RecordEmbeddingRun(ctx context.Context, database *sql.DB, runID string, name string, dimension int, capability string) error {
	if database == nil {
		return fmt.Errorf("database is nil")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("run id is empty")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("embedding name is empty")
	}

	const q = `INSERT INTO embedding_runs(run_id, name, dimension, capability, updated_at)
VALUES(?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET name=excluded.name, dimension=excluded.dimension, capability=excluded.capability, updated_at=excluded.updated_at`

	if _, err := database.ExecContext(ctx, q, runID, name, dimension, capability, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("record embedding run: %w", err)
	}

	return nil
}

// countChunkEmbeddings counts stored vectors for one document under one identity.
func countChunkEmbeddings(ctx context.Context, database *sql.DB, documentID string, name string) (int, error) {
	if strings.TrimSpace(name) == "" {
		return 0, nil
	}

	var count int
	err := database.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM embeddings e
JOIN chunks c ON c.id = e.chunk_id
WHERE c.document_id = ? AND e.name = ?`, documentID, name).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count chunk embeddings: %w", err)
	}

	return count, nil
}

// DeleteEmbeddingsNotNamed removes vectors belonging to another identity, so an
// index only ever holds one vector space.
func DeleteEmbeddingsNotNamed(ctx context.Context, database *sql.DB, name string) (int64, error) {
	if database == nil {
		return 0, fmt.Errorf("database is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("embedding name is empty")
	}

	res, err := database.ExecContext(ctx, `DELETE FROM embeddings WHERE name != ?`, name)
	if err != nil {
		return 0, fmt.Errorf("delete stale embeddings: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected deleting stale embeddings: %w", err)
	}

	return rows, nil
}

// UpsertDocumentWithChunks writes a document and its chunks, and reports whether
// they were inserted, updated or skipped.
//
// embeddingName is the identity of the vectors the caller intends to store. A
// document is skipped only when its content is unchanged AND its chunks already
// carry vectors for that identity, so switching provider re-embeds the corpus
// instead of silently keeping vectors from the previous one.
func UpsertDocumentWithChunks(ctx context.Context, database *sql.DB, doc DocumentRecord, chunks []ChunkRecord, embeddingName string) (string, int, error) {
	if database == nil {
		return "", 0, fmt.Errorf("database is nil")
	}
	doc.ID = strings.TrimSpace(doc.ID)
	if doc.ID == "" {
		return "", 0, fmt.Errorf("document id is empty")
	}
	doc.PageID = strings.TrimSpace(doc.PageID)
	if doc.PageID == "" {
		return "", 0, fmt.Errorf("document page id is empty")
	}

	var existingHash, existingMetadataHash string
	err := database.QueryRowContext(
		ctx, `SELECT content_hash, metadata_hash FROM documents WHERE id = ?`, doc.ID,
	).Scan(&existingHash, &existingMetadataHash)
	if err != nil && err != sql.ErrNoRows {
		return "", 0, fmt.Errorf("query existing document: %w", err)
	}
	isNew := err == sql.ErrNoRows

	if !isNew && existingHash == doc.ContentHash {
		// The text is unchanged, so chunks and vectors can be reused. What is left to
		// decide is whether the metadata moved and whether the vectors are complete.
		vectorsComplete := true
		if strings.TrimSpace(embeddingName) != "" {
			stored, err := countChunkEmbeddings(ctx, database, doc.ID, embeddingName)
			if err != nil {
				return "", 0, err
			}
			vectorsComplete = stored == len(chunks)
		}

		switch {
		case existingMetadataHash == doc.MetadataHash && vectorsComplete:
			return "skipped", 0, nil
		case vectorsComplete:
			// Only metadata changed: refresh it without touching chunks or vectors, so
			// a re-crawled corpus costs no embedding calls for unchanged text.
			if err := updateDocumentMetadata(ctx, database, doc); err != nil {
				return "", 0, err
			}
			return "metadata", 0, nil
		}
		// Vectors are missing, so the document is rewritten below.
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return "", 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if isNew {
		insert := `INSERT INTO documents(id, ` + documentColumns + `, content_hash, metadata_hash, updated_at) VALUES(` + questionMarks(24) + `)`
		args := append([]any{doc.ID}, doc.documentValues()...)
		args = append(args, doc.ContentHash, doc.MetadataHash, now)
		if _, err := tx.ExecContext(ctx, insert, args...); err != nil {
			return "", 0, fmt.Errorf("insert document: %w", err)
		}
	} else {
		update := `UPDATE documents SET ` + assignments(documentColumns) + `, content_hash = ?, metadata_hash = ?, updated_at = ? WHERE id = ?`
		args := doc.documentValues()
		args = append(args, doc.ContentHash, doc.MetadataHash, now, doc.ID)
		if _, err := tx.ExecContext(ctx, update, args...); err != nil {
			return "", 0, fmt.Errorf("update document: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks_fts WHERE chunk_id IN (SELECT id FROM chunks WHERE document_id = ?)`, doc.ID); err != nil {
		return "", 0, fmt.Errorf("delete existing fts rows: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, doc.ID); err != nil {
		return "", 0, fmt.Errorf("delete existing chunks: %w", err)
	}

	const chunkIns = `INSERT INTO chunks(id, document_id, chunk_index, text, chunk_hash, updated_at) VALUES(?, ?, ?, ?, ?, ?)`
	const ftsIns = `INSERT INTO chunks_fts(chunk_id, text, title, section) VALUES(?, ?, ?, ?)`
	for _, ch := range chunks {
		if _, err := tx.ExecContext(ctx, chunkIns, ch.ID, doc.ID, ch.ChunkIndex, ch.Text, ch.ChunkHash, now); err != nil {
			return "", 0, fmt.Errorf("insert chunk %s: %w", ch.ID, err)
		}
		if _, err := tx.ExecContext(ctx, ftsIns, ch.ID, ch.Text, doc.Title, ch.Section); err != nil {
			return "", 0, fmt.Errorf("insert fts chunk %s: %w", ch.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", 0, fmt.Errorf("commit tx: %w", err)
	}

	if isNew {
		return "inserted", len(chunks), nil
	}
	return "updated", len(chunks), nil
}

// updateDocumentMetadata refreshes the crawler metadata of a document whose text is
// unchanged. Chunks and vectors are left alone; only the document row and the title
// copy in the full-text index are rewritten.
func updateDocumentMetadata(ctx context.Context, database *sql.DB, doc DocumentRecord) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	query := `UPDATE documents SET ` + assignments(documentColumns) + `, metadata_hash = ?, updated_at = ? WHERE id = ?`
	args := doc.documentValues()
	args = append(args, doc.MetadataHash, now, doc.ID)
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("update document metadata: %w", err)
	}

	// The title is stored once per chunk in the full-text index, so it has to follow
	// when a rename arrives through metadata alone.
	const ftsTitle = `UPDATE chunks_fts SET title = ? WHERE chunk_id IN (SELECT id FROM chunks WHERE document_id = ?)`
	if _, err := tx.ExecContext(ctx, ftsTitle, doc.Title, doc.ID); err != nil {
		return fmt.Errorf("update indexed title: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

// RecordCorpusSnapshot stores the crawl a run indexed, so a later report can show
// how stale the index is.
func RecordCorpusSnapshot(ctx context.Context, database *sql.DB, runID string, snapshot CorpusSnapshot) error {
	if database == nil {
		return fmt.Errorf("database is nil")
	}

	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("run id is empty")
	}

	const q = `INSERT INTO corpus_snapshot(run_id, crawl_started_at, crawl_completed_at, crawl_succeeded_at, crawl_mode, seed_count, page_count, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET crawl_started_at = excluded.crawl_started_at, crawl_completed_at = excluded.crawl_completed_at,
crawl_succeeded_at = excluded.crawl_succeeded_at, crawl_mode = excluded.crawl_mode, seed_count = excluded.seed_count,
page_count = excluded.page_count, updated_at = excluded.updated_at`
	if _, err := database.ExecContext(ctx, q, runID, snapshot.StartedAt, snapshot.CompletedAt, snapshot.SucceededAt,
		snapshot.Mode, snapshot.SeedCount, snapshot.PageCount, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("record corpus snapshot: %w", err)
	}

	return nil
}

// assignments turns a column list into an assignment list ("a, b" -> "a = ?, b = ?").
func assignments(columns string) string {
	parts := strings.Split(columns, ",")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part) + " = ?"
	}

	return strings.Join(parts, ", ")
}

// questionMarks returns n comma separated placeholders.
func questionMarks(n int) string {
	marks := make([]string, n)
	for i := range marks {
		marks[i] = "?"
	}

	return strings.Join(marks, ", ")
}

func UpsertEmbeddings(ctx context.Context, database *sql.DB, records []EmbeddingRecord) (int, error) {
	if database == nil {
		return 0, fmt.Errorf("database is nil")
	}
	if len(records) == 0 {
		return 0, nil
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin embedding tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	const q = `INSERT INTO embeddings(chunk_id, name, dimension, vector, updated_at)
	VALUES(?, ?, ?, ?, ?)
	ON CONFLICT(chunk_id, name) DO UPDATE SET dimension=excluded.dimension, vector=excluded.vector, updated_at=excluded.updated_at`

	written := 0
	for _, rec := range records {
		if strings.TrimSpace(rec.ChunkID) == "" {
			continue
		}
		if strings.TrimSpace(rec.Name) == "" {
			return 0, fmt.Errorf("invalid embedding identity for chunk %s", rec.ChunkID)
		}
		if rec.Dimension <= 0 {
			return 0, fmt.Errorf("invalid embedding dimension for chunk %s", rec.ChunkID)
		}
		if len(rec.Vector) != rec.Dimension {
			return 0, fmt.Errorf("embedding dimension mismatch for chunk %s", rec.ChunkID)
		}
		blob := encodeFloat32Vector(rec.Vector)
		if _, err := tx.ExecContext(ctx, q, rec.ChunkID, rec.Name, rec.Dimension, blob, now); err != nil {
			return 0, fmt.Errorf("upsert embedding %s: %w", rec.ChunkID, err)
		}
		written++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit embedding tx: %w", err)
	}

	return written, nil
}

func DeleteDocumentsNotIn(ctx context.Context, database *sql.DB, keepIDs []string) (int64, error) {
	if database == nil {
		return 0, fmt.Errorf("database is nil")
	}

	clean := make([]string, 0, len(keepIDs))
	for _, id := range keepIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			clean = append(clean, id)
		}
	}
	sort.Strings(clean)

	if len(clean) == 0 {
		res, err := database.ExecContext(ctx, `DELETE FROM documents`)
		if err != nil {
			return 0, fmt.Errorf("delete all documents: %w", err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("rows affected deleting all documents: %w", err)
		}
		return rows, nil
	}

	placeholders := strings.TrimRight(strings.Repeat("?,", len(clean)), ",")
	query := fmt.Sprintf("DELETE FROM documents WHERE id NOT IN (%s)", placeholders)
	args := make([]any, len(clean))
	for i, id := range clean {
		args[i] = id
	}

	res, err := database.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("delete stale documents: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected deleting stale documents: %w", err)
	}

	return rows, nil
}

func SearchLexical(ctx context.Context, database *sql.DB, queryText string, filters SearchFilters) ([]Candidate, error) {
	if database == nil {
		return nil, fmt.Errorf("database is nil")
	}
	queryText = strings.TrimSpace(queryText)
	if queryText == "" {
		return nil, nil
	}
	if filters.Candidate <= 0 {
		filters.Candidate = 50
	}

	expression := BuildMatchExpression(queryText)
	if expression == "" {
		return nil, nil
	}

	where, filterArgs := filterClauses(filters)
	args := append([]any{expression}, filterArgs...)

	q := fmt.Sprintf(`
SELECT %s, bm25(chunks_fts, %s) as bm
FROM chunks_fts
JOIN chunks c ON c.id = chunks_fts.chunk_id
JOIN documents d ON d.id = c.document_id
WHERE chunks_fts MATCH ? AND %s
ORDER BY bm25(chunks_fts, %s)
LIMIT ?`, candidateColumns, ftsBM25Weights, strings.Join(where, " AND "), ftsBM25Weights)
	args = append(args, filters.Candidate)

	rows, err := database.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("lexical query: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]Candidate, 0, filters.Candidate)
	for rows.Next() {
		var raw float64
		c, err := scanCandidate(rows, &raw)
		if err != nil {
			return nil, fmt.Errorf("scan lexical row: %w", err)
		}
		// bm25 lower is better; convert to higher-is-better positive score.
		c.LexicalScoreRaw = -raw
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lexical rows: %w", err)
	}

	return out, nil
}

func SearchVector(ctx context.Context, database *sql.DB, queryVector []float32, filters SearchFilters) ([]Candidate, error) {
	if database == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if len(queryVector) == 0 {
		return nil, nil
	}
	if filters.Candidate <= 0 {
		filters.Candidate = 50
	}

	where, args := filterClauses(filters)
	if name := strings.TrimSpace(filters.EmbeddingName); name != "" {
		where = append(where, "e.name = ?")
		args = append(args, name)
	}

	q := fmt.Sprintf(`
SELECT %s, e.dimension, e.vector
FROM embeddings e
JOIN chunks c ON c.id = e.chunk_id
JOIN documents d ON d.id = c.document_id
WHERE %s`, candidateColumns, strings.Join(where, " AND "))

	rows, err := database.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("vector query: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]Candidate, 0, filters.Candidate)
	for rows.Next() {
		var dim int
		var blob []byte
		c, err := scanCandidate(rows, &dim, &blob)
		if err != nil {
			return nil, fmt.Errorf("scan vector row: %w", err)
		}
		vec := decodeFloat32Vector(blob, dim)
		if len(vec) == 0 {
			continue
		}
		c.VectorScoreRaw = cosine(queryVector, vec)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vector rows: %w", err)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].VectorScoreRaw == out[j].VectorScoreRaw {
			return out[i].ChunkID < out[j].ChunkID
		}
		return out[i].VectorScoreRaw > out[j].VectorScoreRaw
	})
	if len(out) > filters.Candidate {
		out = out[:filters.Candidate]
	}

	return out, nil
}

func FetchChunkWindow(ctx context.Context, database *sql.DB, documentID string, centerIndex int, expand int) ([]ChunkWindowItem, error) {
	if database == nil {
		return nil, fmt.Errorf("database is nil")
	}
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil, fmt.Errorf("document id is empty")
	}
	if expand < 0 {
		expand = 0
	}

	start := max(centerIndex-expand, 0)
	end := centerIndex + expand

	rows, err := database.QueryContext(ctx, `
SELECT chunk_index, text
FROM chunks
WHERE document_id = ?
  AND chunk_index >= ?
  AND chunk_index <= ?
ORDER BY chunk_index ASC`, documentID, start, end)
	if err != nil {
		return nil, fmt.Errorf("fetch chunk window: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]ChunkWindowItem, 0, end-start+1)
	for rows.Next() {
		var item ChunkWindowItem
		if err := rows.Scan(&item.ChunkIndex, &item.Text); err != nil {
			return nil, fmt.Errorf("scan chunk window row: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chunk window rows: %w", err)
	}

	return out, nil
}

// rowScanner is the subset of *sql.Row and *sql.Rows that scanCandidate needs.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanCandidate reads one row projected with candidateColumns plus the trailing
// channel-specific destinations, so the projection and the scan never drift apart.
func scanCandidate(row rowScanner, extra ...any) (Candidate, error) {
	var c Candidate
	var isSeed int

	dest := []any{
		&c.ChunkID, &c.DocumentID, &c.PageID, &c.Title, &c.LocalPath, &c.SpaceKey, &c.SourceURL,
		&c.LastModifiedAt, &c.ChunkText, &c.ChunkIndex, &c.Host, &c.CanonicalURL, &c.Depth, &isSeed,
		&c.CreatedByName, &c.ModifiedByName, &c.LinkIn, &c.Attachments, &c.Comments,
	}
	dest = append(dest, extra...)

	if err := row.Scan(dest...); err != nil {
		return Candidate{}, err
	}
	c.IsSeed = isSeed == 1

	return c, nil
}

func encodeFloat32Vector(vector []float32) []byte {
	buf := make([]byte, len(vector)*4)
	for i, v := range vector {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

func decodeFloat32Vector(blob []byte, dim int) []float32 {
	if dim <= 0 || len(blob) != dim*4 {
		return nil
	}
	out := make([]float32, dim)
	for i := range dim {
		bits := binary.LittleEndian.Uint32(blob[i*4:])
		out[i] = math.Float32frombits(bits)
	}
	return out
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func randomID(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
