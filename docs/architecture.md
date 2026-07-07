# Architecture and Data Flow

This document explains how `confluence2md-indexer` is structured and how data flows through indexing and query execution.

## Design Summary

The architecture is service-first:

- `internal/service` contains command execution orchestration.
- `internal/cli` is a thin adapter for argument parsing, validation, and output formatting.
- Retrieval and persistence logic remains in dedicated packages (`internal/query`, `internal/db`, `internal/indexer`, `internal/embedding`).

This keeps one implementation path for command behavior while preserving stable CLI contracts.

## Package Roles

- `cmd/confluence2md-indexer`
  - process entrypoint
- `internal/cli`
  - subcommand routing
  - flag parsing
  - output rendering (text/JSON)
- `internal/service`
  - high-level command execution for index/query/stats
- `internal/indexer`
  - metadata loading
  - markdown chunking
- `internal/db`
  - SQLite access, schema migration, persistence, search helpers
- `internal/query`
  - retrieval and fusion logic
- `internal/embedding`
  - embedding provider selection and generation

## Index Command Flow

1. CLI parses args and resolves DB path.
2. CLI calls `service.Index`.
3. Service performs:
   - preflight validation (`indexer.Preflight`)
   - DB open + migrate
   - run tracking (`BeginRun`/`CompleteRun`)
   - document loading/chunking
   - document/chunk upsert
   - embedding generation + upsert for changed chunks
   - stale document cleanup
   - stats readback
4. CLI renders text or JSON output.

## Query Command Flow

1. CLI parses query request options and validates flags.
2. CLI calls `service.Query` with typed request.
3. Service opens DB and delegates to `query.Run`.
4. Query pipeline executes lexical/vector/hybrid retrieval and optional expansion.
5. CLI renders text output, JSON contract, and optional explain diagnostics.

## Stats Command Flow

1. CLI validates `--db` and output mode.
2. CLI calls `service.Stats`.
3. Service opens DB and returns aggregated counters.
4. CLI renders text or JSON.

## Data Stores

SQLite database stores:

- schema version table
- indexing runs
- documents
- chunks
- embeddings
- FTS virtual table for lexical search

## Output Contract Stability

JSON command outputs include `schemaVersion`.

Contract stability is protected by:

- command-level output tests
- golden end-to-end JSON tests
- CI quality gates

## Related Docs

- [Query examples](query-examples.md)
- [Operations and troubleshooting](operations.md)
- [MCP integration decision](mcp-integration-decision.md)
