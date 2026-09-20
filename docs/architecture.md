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
  - embedding provider interface, registry and resolution
  - provider implementations (offline bag-of-words, OpenAI, OpenAI-compatible)
  - identity fingerprinting and capability reporting

## Index Command Flow

1. CLI parses args and resolves DB path.
2. CLI calls `service.Index`.
3. Service performs:
   - preflight validation (`indexer.Preflight`)
   - DB open + migrate
   - run tracking (`BeginRun`/`CompleteRun`)
   - document loading/chunking, including the crawler metadata merge and the heading breadcrumb
   - document/chunk upsert, which decides between a full rewrite, a metadata-only refresh and a skip
   - embedding generation + upsert for changed chunks
   - corpus snapshot recording and stale document cleanup
   - stats readback
4. CLI renders text or JSON output, including the count of metadata-only refreshes.

## Query Command Flow

1. CLI parses query request options, validates flags and resolves any relative age
   (`--updated-since 30d`) into a timestamp.
2. CLI calls `service.Query` with typed request.
3. Service opens DB, verifies the schema version and delegates to `query.Run`.
4. Query pipeline runs the metadata filters as SQL predicates, the lexical channel
   (a sanitised FTS5 expression with weighted BM25) and/or the vector channel, then
   normalises and fuses the surviving candidates and applies optional expansion.
5. CLI renders text output, JSON contract, and optional explain diagnostics.

## Stats Command Flow

1. CLI validates `--db` and output mode.
2. CLI calls `service.Stats`.
3. Service opens DB and returns aggregated counters.
4. CLI renders text or JSON.

## Data Stores

SQLite database stores:

- indexing runs and the embedding identity each run recorded
- documents, including the crawler metadata (host, canonical URL, version, depth,
  parent, authors, seed flag, link and attachment counts, comment count) and a
  metadata fingerprint
- chunks, with their heading breadcrumb
- embeddings, keyed by chunk and embedding identity
- `chunks_fts`, an FTS5 table over `text`, `title` and `section`; page id and space
  key are filtered in SQL instead of being indexed
- corpus snapshots, which record the crawl each run indexed

The schema is versioned through `PRAGMA user_version`. A database stamped with another
version is refused with a message that names the rebuild command, because a rebuild from
the crawler output is deterministic and is the only path that fills columns added after
a page was first indexed. See [metadata.md](metadata.md) for the full model.

## Embedding Resolution

Provider selection and identity tracking run through one code path shared by
indexing and querying:

1. Flags, then `CONFLUENCE2MD_EMBEDDING_*` variables, then defaults select a provider.
2. The provider reports an identity string (model, dimension, endpoint, prefixes).
3. Indexing stores that identity with every vector it writes and removes vectors of other identities.
4. Querying compares the resolved provider with the identities stored in the index.
5. A mismatch fails the query with exit code 2 rather than comparing vectors from different spaces.

See [embedding-providers.md](embedding-providers.md) for the provider matrix and setup recipes.

## Output Contract Stability

JSON command outputs include `schemaVersion`.

Contract stability is protected by:

- command-level output tests
- golden end-to-end JSON tests
- CI quality gates

## Related Docs

- [Query examples](query-examples.md)
- [Embedding providers](embedding-providers.md)
- [Operations and troubleshooting](operations.md)
- [MCP integration decision](mcp-integration-decision.md)
