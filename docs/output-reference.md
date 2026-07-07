# Output Reference

This document explains command output fields for `index`, `query`, and `stats`.

It covers:

- text output (default)
- JSON output (`--json`)
- explain diagnostics (`query --explain`)

## Common Output Conventions

- `schemaVersion`: string version for machine-readable JSON contracts.
- `command`: command name (`index`, `query`, `stats`).
- `dbPath`: resolved path to the SQLite database used for the command.

## Index Output

## Text Output (`index`)

Example:

```text
index preflight passed: 29 pages, 29 markdown files validated
db path: <db-path>
run id: <run-id>
schema version: 1
runs recorded: 1
documents inserted: 29
documents updated: 0
documents skipped: 0
documents deleted: 0
chunks written: 319
embeddings written: 319 (hash-local; source=hash-fallback)
mode: full rebuild
```

Field meaning:

- `index preflight passed`: source validation completed.
- `db path`: active DB file path.
- `run id`: run-tracking identifier for this indexing run.
- `schema version`: max schema version in DB.
- `runs recorded`: total runs persisted in DB run table.
- `documents inserted/updated/skipped/deleted`: document-level write summary.
- `chunks written`: chunk rows written during this run.
- `embeddings written`: embedding rows written plus provider/source.
- `mode`: incremental default or full rebuild.

## JSON Output (`index --json`)

Top-level fields:

- `schemaVersion`
- `command`
- `status`
- `incremental`
- `rebuild`
- `dbPath`
- `embedding`
- `inputFolder`
- `metadataPath`
- `pageCount`
- `checkedFiles`
- `documents`
- `chunkWrites`
- `runId`
- `dbStats`

Nested fields:

- `embedding.provider`: embedding provider name.
- `embedding.source`: provider source resolution (for example hash fallback).
- `embedding.written`: embeddings written in this run.
- `documents.inserted|updated|skipped|deleted`: per-run document counters.
- `dbStats.schemaVersion|runs|documents|chunks|embeddings`: DB aggregate counters.

## Query Output

## Text Output (`query`)

Per result block:

- `<rank>. [<pageId>] <title> (chunk <chunkIndex>)`
- `score=<fused> lexical=<lexical> vector=<vector> fusion=<fusion>`
- optional `context-range=<start>..<end> (<count> chunks)` when `--expand > 0`
- `path=<localPath>`
- `text=<chunkText summary>`

Example:

```text
1. [<page-id>] <example-title> (chunk 7)
	score=0.8473 lexical=1.0000 vector=0.4908 fusion=weighted
	path=<example-file>.md
	text=<chunk summary text>
```

Score fields:

- `score`: final fused score used for ranking.
- `lexical`: normalized lexical channel score.
- `vector`: normalized vector channel score.
- `fusion`: effective fusion strategy for that result (`lexical`, `vector`, `weighted`, `rrf`).

## Fusion Calculation Mini-Examples

### Weighted fusion

Given:

- `alpha = 0.70`
- `lexical = 1.0000`
- `vector = 0.4908`

Calculation:

```text
fused = alpha*lexical + (1-alpha)*vector
	= 0.70*1.0000 + 0.30*0.4908
	= 0.7000 + 0.14724
	= 0.84724 -> 0.8473
```

### RRF fusion

Given:

- `rrf-k = 60`
- lexical rank = 1
- vector rank = 2

Calculation:

```text
fused = 1/(rrf-k + lexical-rank) + 1/(rrf-k + vector-rank)
	= 1/(60+1) + 1/(60+2)
	= 1/61 + 1/62
	= 0.01639 + 0.01613
	= 0.03252 -> 0.0325
```

Notes:

- In RRF, score depends on rank position, not raw lexical/vector magnitude.
- A channel contributes only if the chunk appears in that channel's candidate list.

No-results case:

```text
no results
```

## JSON Output (`query --json`)

Top-level fields:

- `schemaVersion`
- `command`
- `dbPath`
- `request`
- `count`
- `total`
- `results`
- `pagination`
- optional `explain` when `--explain` is used

Important counters:

- `count`: number of results in this page.
- `total`: total results available after ranking and scoring filters.

`pagination`:

- `offset`: zero-based start in ranked results.
- `limit`: page size (`0` means defaulted from top-k in request handling).

Result object fields:

- `rank`
- `chunkId`
- `documentId`
- `pageId`
- `title`
- `localPath`
- `spaceKey`
- `sourceUrl`
- `chunkText`
- optional `baseChunkText` when context expansion is applied
- `chunkIndex`
- optional `contextStartIndex`, `contextEndIndex`, `contextChunkCount`
- `lexicalScore`
- `vectorScore`
- `fusedScore`
- `fusion`

## Explain Output (`query --explain`)

Text explain section lines:

- `mode=<mode>`
- `fusion=<effective-fusion>`
- `alpha=<alpha>`
- `rrf-k=<rrf-k>`
- `expand=<expand>`
- `returned=<count>`
- `top chunk=...`
- optional `top weighted-components ...` only when effective fusion is `weighted`
- optional `top context-range=...` when expansion exists
- `top gap=<gap>` when at least two results exist

Note:

- `effective-fusion` reflects mode-aware behavior (`lexical` mode shows `fusion=lexical`, `vector` mode shows `fusion=vector`).

## Stats Output

## Text Output (`stats`)

Fields:

- `db path`
- `schema version`
- `runs`
- `documents`
- `chunks`
- `embeddings`

## JSON Output (`stats --json`)

Top-level fields:

- `schemaVersion`
- `command`
- `dbPath`
- `stats`

Nested `stats` fields:

- `schemaVersion`
- `runs`
- `documents`
- `chunks`
- `embeddings`

## Notes for Automation

- Use JSON mode for scripts and parsers.
- Treat `schemaVersion` as the contract gate for parsers.
- Prefer `count` and `total` over assumptions about page size.
- For explain parsing, treat explain entries as human diagnostics, not a strict machine contract.
