# Changelog

All notable changes to this project will be documented in this file.

## [0.8.0](https://github.com/gkoos/confluence2md-indexer/compare/v0.7.0...v0.8.0) (2026-10-21)


### Features

* **embedding:** opt-in context header for document embeddings ([#54](https://github.com/gkoos/confluence2md-indexer/issues/54)) ([cbab1cf](https://github.com/gkoos/confluence2md-indexer/commit/cbab1cf01872c765a9512bbab71bd2bb688aaac8)), closes [#30](https://github.com/gkoos/confluence2md-indexer/issues/30)


### Bug Fixes

* **db:** index the heading breadcrumb in chunks_fts ([#56](https://github.com/gkoos/confluence2md-indexer/issues/56)) ([c7dc9ce](https://github.com/gkoos/confluence2md-indexer/commit/c7dc9ce8120b421cc5e05469c48132726916afbf)), closes [#53](https://github.com/gkoos/confluence2md-indexer/issues/53)

## [0.7.0](https://github.com/gkoos/confluence2md-indexer/compare/v0.6.0...v0.7.0) (2026-10-21)


### Features

* **api:** expose indexerapi.Stats ([#50](https://github.com/gkoos/confluence2md-indexer/issues/50)) ([168e11d](https://github.com/gkoos/confluence2md-indexer/commit/168e11d0479ffdc453cac2f367e641998802cfff))

## [0.6.0](https://github.com/gkoos/confluence2md-indexer/compare/v0.5.3...v0.6.0) (2026-10-21)


### Features

* **query:** shape query results with parent filters, grouping and facets ([#48](https://github.com/gkoos/confluence2md-indexer/issues/48)) ([4a2f0d8](https://github.com/gkoos/confluence2md-indexer/commit/4a2f0d8c194c58e46d7cbe4bfde89c5357692212)), closes [#35](https://github.com/gkoos/confluence2md-indexer/issues/35)

## [0.5.3](https://github.com/gkoos/confluence2md-indexer/compare/v0.5.2...v0.5.3) (2026-10-21)


### Performance Improvements

* **db:** stream vector candidates and hydrate after ranking ([#46](https://github.com/gkoos/confluence2md-indexer/issues/46)) ([ba75ddf](https://github.com/gkoos/confluence2md-indexer/commit/ba75ddf07a5a2794985ee8b07d67a5bdbed05009)), closes [#27](https://github.com/gkoos/confluence2md-indexer/issues/27)

## [0.5.2](https://github.com/gkoos/confluence2md-indexer/compare/v0.5.1...v0.5.2) (2026-10-20)


### Bug Fixes

* **indexer:** require a space after # for headings ([#43](https://github.com/gkoos/confluence2md-indexer/issues/43)) ([20a26b4](https://github.com/gkoos/confluence2md-indexer/commit/20a26b4f9e8900db9f1e8f1b0a7b5f33cf53c6f2)), closes [#25](https://github.com/gkoos/confluence2md-indexer/issues/25)

## [0.5.1](https://github.com/gkoos/confluence2md-indexer/compare/v0.5.0...v0.5.1) (2026-10-20)


### Bug Fixes

* **indexer:** accept a numeric confluence_parent_id in metadata.json ([#41](https://github.com/gkoos/confluence2md-indexer/issues/41)) ([eb54649](https://github.com/gkoos/confluence2md-indexer/commit/eb546494fb1e11796a600ec24e378bb224c6b997))

## [0.5.0](https://github.com/gkoos/confluence2md-indexer/compare/v0.4.0...v0.5.0) (2026-09-20)


### Features

* **cli:** report the build through --version and on startup ([b339d54](https://github.com/gkoos/confluence2md-indexer/commit/b339d54efdeb613e56517dd952f6117f6a6c33d7))
* **metadata:** index crawler metadata and filter queries by it ([ee17211](https://github.com/gkoos/confluence2md-indexer/commit/ee172118a5dd4573609aad7fdfba0b15dc92e20b))
* **metadata:** index, filter and rank by crawler metadata ([4d681aa](https://github.com/gkoos/confluence2md-indexer/commit/4d681aa2f753dec3082b1a4605cd42ef57fa0834))
* **query:** add metadata ranking priors and a query section for retrieval defaults ([18a2f71](https://github.com/gkoos/confluence2md-indexer/commit/18a2f7172de309b1a2ca744d7381a005b9a77b2f))
* **stats:** report metadata coverage and crawl freshness ([3ebb8f4](https://github.com/gkoos/confluence2md-indexer/commit/3ebb8f4136649341526b048194df9b08887ede28))


### Bug Fixes

* **indexer:** stop reading headings inside fenced code blocks ([608a7e5](https://github.com/gkoos/confluence2md-indexer/commit/608a7e5bbc036c42c69d8daed05818e36fdc6f7e))


### Reverts

* metadata retrieval squash ([#18](https://github.com/gkoos/confluence2md-indexer/issues/18)) ([d96f5c2](https://github.com/gkoos/confluence2md-indexer/commit/d96f5c2b5419d669d9e5d7d62b0ba12fb7cb0a9a))

## [0.4.0](https://github.com/gkoos/confluence2md-indexer/compare/v0.3.0...v0.4.0) (2026-09-20)


### Features

* **config:** add an optional YAML config file for db and embedding settings ([1d063c6](https://github.com/gkoos/confluence2md-indexer/commit/1d063c6381ffd09776d77a7cac85b670892e8f5a))

## [0.3.0](https://github.com/gkoos/confluence2md-indexer/compare/v0.2.0...v0.3.0) (2026-09-20)


### Features

* **embedding:** add pluggable embedding providers, embedding flags and an offline vector smoke gate ([0600236](https://github.com/gkoos/confluence2md-indexer/commit/0600236cd32b3e60a03575308877601de167927a))

## [0.2.0](https://github.com/gkoos/confluence2md-indexer/compare/v0.1.2...v0.2.0) (2026-07-08)


### Features

* **indexerapi:** add pkg query entrypoint and route service query through it ([#4](https://github.com/gkoos/confluence2md-indexer/issues/4)) ([21ff685](https://github.com/gkoos/confluence2md-indexer/commit/21ff68570c9b63f03a9d8b7291bbef82d527263d))

## [0.1.2](https://github.com/gkoos/confluence2md-indexer/compare/v0.1.1...v0.1.2) (2026-07-07)


### Bug Fixes

* **release:** track cli entrypoint and narrow binary ignore rules ([#2](https://github.com/gkoos/confluence2md-indexer/issues/2)) ([b2bfa2c](https://github.com/gkoos/confluence2md-indexer/commit/b2bfa2c10c8b9db5520343cc70444364e1d96644))

## [0.1.1](https://github.com/gkoos/confluence2md-indexer/compare/v0.1.0...v0.1.1) (2026-07-07)


### Bug Fixes

* **ci:** align release workflows and bump go patch version ([6865c93](https://github.com/gkoos/confluence2md-indexer/commit/6865c93a5669bcc5a081ae27525df37960a35b24))
* **ci:** resolve lint failures and bump Go to 1.25.11 ([9482d50](https://github.com/gkoos/confluence2md-indexer/commit/9482d500b20ce0cdfde922b75f600b258afb09e5))
* **lint:** handle deferred rows close in chunk window query ([5354b69](https://github.com/gkoos/confluence2md-indexer/commit/5354b690b0392962cd3a63a33ecac78b45a1aef8))

## [Unreleased]

- Phase 4 delivery: embeddings persistence, lexical/vector retrieval, hybrid fusion, and query execution wiring.
- Phase 5 quality hardening: added query fusion tests, DB retrieval/filtering tests, and CLI query output integration tests.
- Phase 6 release readiness: coverage gate task and CI enforcement, expanded reproducible release matrix, and end-to-end golden JSON contract tests.
- Coverage uplift: added embedding, logging, and coveragecheck unit tests; raised enforced coverage baseline to 70%.
- Phase 7 retrieval UX completion: implemented `--expand` context stitching for query results, added context-range diagnostics in explain output, and added DB/query/CLI tests for expansion behavior.
- Phase 8 contract and paging: added `schemaVersion` to JSON outputs and deterministic query pagination via `--offset` and `--limit`, with updated contract tests and golden fixtures.
- Service-first completion: moved index and stats execution behind `internal/service` APIs, aligned query/index/stats on the same service-backed command pattern, and kept CLI as a thin validation/output adapter.
