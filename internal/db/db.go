package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

const CurrentSchemaVersion = 1

const initSchemaSQL = `
CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

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
  content_hash TEXT NOT NULL,
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
  chunk_id TEXT PRIMARY KEY,
  model TEXT NOT NULL,
  dimension INTEGER NOT NULL,
  vector BLOB NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(chunk_id) REFERENCES chunks(id) ON DELETE CASCADE
);

CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
  chunk_id UNINDEXED,
  text,
  title,
  page_id,
  space_key
);

INSERT INTO schema_version(version, applied_at)
VALUES (1, CURRENT_TIMESTAMP)
ON CONFLICT(version) DO NOTHING;
`

type Run struct {
	ID          string
	StartedAt   time.Time
	CompletedAt *time.Time
	Mode        string
}

type Stats struct {
	SchemaVersion int `json:"schemaVersion"`
	Runs          int `json:"runs"`
	Documents     int `json:"documents"`
	Chunks        int `json:"chunks"`
	Embeddings    int `json:"embeddings"`
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
}

type ChunkRecord struct {
	ID         string
	ChunkIndex int
	Text       string
	ChunkHash  string
}

type ChunkWindowItem struct {
	ChunkIndex int
	Text       string
}

type EmbeddingRecord struct {
	ChunkID   string
	Model     string
	Dimension int
	Vector    []float32
}

type Candidate struct {
	ChunkID         string
	DocumentID      string
	PageID          string
	Title           string
	LocalPath       string
	SpaceKey        string
	SourceURL       string
	LastModifiedAt  string
	ChunkText       string
	ChunkIndex      int
	LexicalScoreRaw float64
	VectorScoreRaw  float64
}

type SearchFilters struct {
	SpaceKey  string
	PageID    string
	FromDate  string
	ToDate    string
	Candidate int
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

func Migrate(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return fmt.Errorf("database is nil")
	}

	if _, err := database.ExecContext(ctx, initSchemaSQL); err != nil {
		return fmt.Errorf("apply schema migration: %w", err)
	}

	if err := ensureDocumentColumn(ctx, database, "space_key", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureDocumentColumn(ctx, database, "source_url", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureDocumentColumn(ctx, database, "last_modified_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	return nil
}

func ensureDocumentColumn(ctx context.Context, database *sql.DB, name string, definition string) error {
	rows, err := database.QueryContext(ctx, `PRAGMA table_info(documents)`)
	if err != nil {
		return fmt.Errorf("inspect documents schema: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var colName string
		var colType string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &colName, &colType, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan documents schema: %w", err)
		}
		if colName == name {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate documents schema: %w", err)
	}

	q := fmt.Sprintf("ALTER TABLE documents ADD COLUMN %s %s", name, definition)
	if _, err := database.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("add documents column %s: %w", name, err)
	}

	return nil
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

	stats := &Stats{}

	if err := database.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&stats.SchemaVersion); err != nil {
		return nil, fmt.Errorf("query schema_version: %w", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs`).Scan(&stats.Runs); err != nil {
		return nil, fmt.Errorf("query runs count: %w", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents`).Scan(&stats.Documents); err != nil {
		return nil, fmt.Errorf("query documents count: %w", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&stats.Chunks); err != nil {
		return nil, fmt.Errorf("query chunks count: %w", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM embeddings`).Scan(&stats.Embeddings); err != nil {
		return nil, fmt.Errorf("query embeddings count: %w", err)
	}

	return stats, nil
}

func UpsertDocumentWithChunks(ctx context.Context, database *sql.DB, doc DocumentRecord, chunks []ChunkRecord) (string, int, error) {
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

	var existingHash string
	err := database.QueryRowContext(ctx, `SELECT content_hash FROM documents WHERE id = ?`, doc.ID).Scan(&existingHash)
	if err != nil && err != sql.ErrNoRows {
		return "", 0, fmt.Errorf("query existing document: %w", err)
	}
	isNew := err == sql.ErrNoRows
	if err == nil && existingHash == doc.ContentHash {
		return "skipped", 0, nil
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return "", 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if isNew {
		const ins = `INSERT INTO documents(id, page_id, title, local_path, space_key, source_url, last_modified_at, content_hash, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`
		if _, err := tx.ExecContext(ctx, ins, doc.ID, doc.PageID, doc.Title, doc.LocalPath, doc.SpaceKey, doc.SourceURL, doc.LastModifiedAt, doc.ContentHash, now); err != nil {
			return "", 0, fmt.Errorf("insert document: %w", err)
		}
	} else {
		const upd = `UPDATE documents SET page_id = ?, title = ?, local_path = ?, space_key = ?, source_url = ?, last_modified_at = ?, content_hash = ?, updated_at = ? WHERE id = ?`
		if _, err := tx.ExecContext(ctx, upd, doc.PageID, doc.Title, doc.LocalPath, doc.SpaceKey, doc.SourceURL, doc.LastModifiedAt, doc.ContentHash, now, doc.ID); err != nil {
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
	const ftsIns = `INSERT INTO chunks_fts(chunk_id, text, title, page_id, space_key) VALUES(?, ?, ?, ?, ?)`
	for _, ch := range chunks {
		if _, err := tx.ExecContext(ctx, chunkIns, ch.ID, doc.ID, ch.ChunkIndex, ch.Text, ch.ChunkHash, now); err != nil {
			return "", 0, fmt.Errorf("insert chunk %s: %w", ch.ID, err)
		}
		if _, err := tx.ExecContext(ctx, ftsIns, ch.ID, ch.Text, doc.Title, doc.PageID, doc.SpaceKey); err != nil {
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
	const q = `INSERT INTO embeddings(chunk_id, model, dimension, vector, updated_at)
	VALUES(?, ?, ?, ?, ?)
	ON CONFLICT(chunk_id) DO UPDATE SET model=excluded.model, dimension=excluded.dimension, vector=excluded.vector, updated_at=excluded.updated_at`

	written := 0
	for _, rec := range records {
		if strings.TrimSpace(rec.ChunkID) == "" {
			continue
		}
		if rec.Dimension <= 0 {
			return 0, fmt.Errorf("invalid embedding dimension for chunk %s", rec.ChunkID)
		}
		if len(rec.Vector) != rec.Dimension {
			return 0, fmt.Errorf("embedding dimension mismatch for chunk %s", rec.ChunkID)
		}
		blob := encodeFloat32Vector(rec.Vector)
		if _, err := tx.ExecContext(ctx, q, rec.ChunkID, rec.Model, rec.Dimension, blob, now); err != nil {
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

	where := []string{"1=1"}
	args := []any{queryText}
	if strings.TrimSpace(filters.SpaceKey) != "" {
		where = append(where, "d.space_key = ?")
		args = append(args, strings.TrimSpace(filters.SpaceKey))
	}
	if strings.TrimSpace(filters.PageID) != "" {
		where = append(where, "d.page_id = ?")
		args = append(args, strings.TrimSpace(filters.PageID))
	}
	if strings.TrimSpace(filters.FromDate) != "" {
		where = append(where, "d.last_modified_at >= ?")
		args = append(args, strings.TrimSpace(filters.FromDate))
	}
	if strings.TrimSpace(filters.ToDate) != "" {
		where = append(where, "d.last_modified_at <= ?")
		args = append(args, strings.TrimSpace(filters.ToDate)+"T23:59:59Z")
	}

	q := fmt.Sprintf(`
SELECT c.id, c.document_id, d.page_id, d.title, d.local_path, d.space_key, d.source_url, d.last_modified_at, c.text, c.chunk_index, bm25(chunks_fts) as bm
FROM chunks_fts
JOIN chunks c ON c.id = chunks_fts.chunk_id
JOIN documents d ON d.id = c.document_id
WHERE chunks_fts MATCH ? AND %s
ORDER BY bm25(chunks_fts)
LIMIT ?`, strings.Join(where, " AND "))
	args = append(args, filters.Candidate)

	rows, err := database.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("lexical query: %w", err)
	}
	defer rows.Close()

	out := make([]Candidate, 0, filters.Candidate)
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.ChunkID, &c.DocumentID, &c.PageID, &c.Title, &c.LocalPath, &c.SpaceKey, &c.SourceURL, &c.LastModifiedAt, &c.ChunkText, &c.ChunkIndex, &c.LexicalScoreRaw); err != nil {
			return nil, fmt.Errorf("scan lexical row: %w", err)
		}
		// bm25 lower is better; convert to higher-is-better positive score.
		c.LexicalScoreRaw = -c.LexicalScoreRaw
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

	where := []string{"1=1"}
	args := []any{}
	if strings.TrimSpace(filters.SpaceKey) != "" {
		where = append(where, "d.space_key = ?")
		args = append(args, strings.TrimSpace(filters.SpaceKey))
	}
	if strings.TrimSpace(filters.PageID) != "" {
		where = append(where, "d.page_id = ?")
		args = append(args, strings.TrimSpace(filters.PageID))
	}
	if strings.TrimSpace(filters.FromDate) != "" {
		where = append(where, "d.last_modified_at >= ?")
		args = append(args, strings.TrimSpace(filters.FromDate))
	}
	if strings.TrimSpace(filters.ToDate) != "" {
		where = append(where, "d.last_modified_at <= ?")
		args = append(args, strings.TrimSpace(filters.ToDate)+"T23:59:59Z")
	}

	q := fmt.Sprintf(`
SELECT c.id, c.document_id, d.page_id, d.title, d.local_path, d.space_key, d.source_url, d.last_modified_at, c.text, c.chunk_index, e.dimension, e.vector
FROM embeddings e
JOIN chunks c ON c.id = e.chunk_id
JOIN documents d ON d.id = c.document_id
WHERE %s`, strings.Join(where, " AND "))

	rows, err := database.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("vector query: %w", err)
	}
	defer rows.Close()

	out := make([]Candidate, 0, filters.Candidate)
	for rows.Next() {
		var c Candidate
		var dim int
		var blob []byte
		if err := rows.Scan(&c.ChunkID, &c.DocumentID, &c.PageID, &c.Title, &c.LocalPath, &c.SpaceKey, &c.SourceURL, &c.LastModifiedAt, &c.ChunkText, &c.ChunkIndex, &dim, &blob); err != nil {
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
	defer rows.Close()

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
