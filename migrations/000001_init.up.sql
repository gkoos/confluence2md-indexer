-- Phase 0 placeholder schema migration.
-- Phase 2 will expand these tables and indexes.

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

INSERT INTO schema_version(version, applied_at)
VALUES (1, CURRENT_TIMESTAMP)
ON CONFLICT(version) DO NOTHING;
