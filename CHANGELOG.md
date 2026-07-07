# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

- Phase 4 delivery: embeddings persistence, lexical/vector retrieval, hybrid fusion, and query execution wiring.
- Phase 5 quality hardening: added query fusion tests, DB retrieval/filtering tests, and CLI query output integration tests.
- Phase 6 release readiness: coverage gate task and CI enforcement, expanded reproducible release matrix, and end-to-end golden JSON contract tests.
- Coverage uplift: added embedding, logging, and coveragecheck unit tests; raised enforced coverage baseline to 70%.
- Phase 7 retrieval UX completion: implemented `--expand` context stitching for query results, added context-range diagnostics in explain output, and added DB/query/CLI tests for expansion behavior.
- Phase 8 contract and paging: added `schemaVersion` to JSON outputs and deterministic query pagination via `--offset` and `--limit`, with updated contract tests and golden fixtures.
- Service-first completion: moved index and stats execution behind `internal/service` APIs, aligned query/index/stats on the same service-backed command pattern, and kept CLI as a thin validation/output adapter.
