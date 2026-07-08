# `confluence2md-indexer` - Local Hybrid Search for `confluence2md` Exports

[![CI](https://github.com/gkoos/confluence2md-indexer/actions/workflows/ci.yml/badge.svg)](https://github.com/gkoos/confluence2md-indexer/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/gkoos/confluence2md-indexer)](https://github.com/gkoos/confluence2md-indexer/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/gkoos/confluence2md-indexer/blob/main/LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/gkoos/confluence2md-indexer)](https://github.com/gkoos/confluence2md-indexer/blob/main/go.mod)

Build a local SQLite index from [confluence2md](https://github.com/gkoos/confluence2md) output and run fast lexical, vector, and hybrid retrieval from the command line.

Use it to:

- run local RAG retrieval against exported Confluence pages
- search docs offline with deterministic output contracts
- filter by space, page, and date range
- run automated retrieval tests with stable JSON output
- power in-process integrations through a public Query API

What you get:

- one local SQLite database file containing documents, chunks, embeddings, and FTS data
- incremental indexing by default, full rebuild on demand
- query modes for lexical, vector, and hybrid retrieval
- optional context expansion for neighboring chunks
- explain diagnostics and stable JSON output (`schemaVersion`)
- service-first internals where CLI handlers are thin adapters

## What It Does

- Validates the `confluence2md` output contract (`metadata.json` + markdown files).
- Ingests markdown pages into normalized chunks.
- Persists document and chunk records to SQLite.
- Stores embeddings for changed chunks.
- Executes lexical, vector, or hybrid retrieval with configurable fusion.
- Supports deterministic pagination using `--offset` and `--limit`.
- Emits human-readable and machine-readable output for index/query/stats commands.

## Download

Pre-built binaries are available on the [Releases](https://github.com/gkoos/confluence2md-indexer/releases) page.

1. Download the archive for your platform.
2. Extract the binary (`confluence2md-indexer` or `confluence2md-indexer.exe`).
3. Run from your chosen working directory.

## How To Use

### Requirements

- Go 1.25+ (for local builds)
- [Task](https://taskfile.dev) (recommended for local workflow)
- Existing `confluence2md` output folder with `metadata.json`

### Quickstart

Build:

```sh
task build
```

Index a corpus:

```sh
confluence2md-indexer index ./output
```

Run a hybrid query:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "how to rotate secrets" --mode hybrid
```

Inspect index stats:

```sh
confluence2md-indexer stats --db ./output/confluence2md-index.db --json
```

### CLI Usage

```text
confluence2md-indexer index [folder] [--db path] [--rebuild] [--json]
confluence2md-indexer query --q text [--db path] [--mode hybrid|lexical|vector] [--fusion weighted|rrf] [--offset N] [--limit N] [--json] [--explain]
confluence2md-indexer stats [--db path] [--json]
```

See [docs/query-examples.md](docs/query-examples.md) for practical command patterns.

### In-process Query API (for MCP)

You can import and call the Query path directly without spawning the CLI process:

```go
import (
	"context"

	"github.com/gkoos/confluence2md-indexer/pkg/indexerapi"
)

resp, err := indexerapi.Query(context.Background(), "./output/confluence2md-index.db", indexerapi.QueryRequest{
	Text: "how to rotate secrets",
	Mode: "hybrid",
	TopK: 10,
})
if err != nil {
	// handle error
}

_ = resp.Results
```

## How It Works

### Input Contract

The indexer reads:

- `metadata.json` produced by `confluence2md`
- markdown files referenced by `metadata.pages[*].local_path`

### Indexing Flow

1. Run preflight checks on metadata and markdown paths.
2. Open or create SQLite database.
3. Apply migrations and ensure schema compatibility.
4. Convert pages into chunk records.
5. Upsert changed documents/chunks.
6. Generate embeddings for changed chunks only.
7. Remove stale records no longer present in source metadata.
8. Record run metadata and emit index summary.

### Query Flow

1. Parse query request, filters, and pagination options.
2. Run lexical search (FTS5), vector search, or both.
3. Fuse candidate scores (weighted or RRF).
4. Apply deterministic paging and optional context expansion.
5. Return results as text output or JSON contract.

## Output Contracts

- JSON outputs include `schemaVersion` for machine-readability and contract stability.
- Query JSON includes `count`, `total`, and `pagination` fields.
- Golden tests validate index/query/stats JSON contracts end to end.
- Field-by-field output reference is documented in [docs/output-reference.md](docs/output-reference.md).

## Limitations and Sizing Guidance

This tool is optimized for local developer workflows, not large multi-tenant serving.

Current practical limits depend mostly on chunk count and embedding dimension.

- Hash fallback embeddings (`OPENAI_API_KEY` unset) use 256 dimensions.
- OpenAI embeddings can use larger dimensions and increase DB size accordingly.

Approximate DB size planning:

- Embedding storage per chunk is roughly `dimension * 4 bytes` before SQLite overhead.
- At 256 dimensions, that is about 1 KB per chunk for vectors alone.
- FTS and chunk text typically dominate total size for text-heavy corpora.

Rule-of-thumb ranges for local usage (depends on average chunk text length):

- ~10,000 chunks: usually tens to low hundreds of MB.
- ~100,000 chunks: usually hundreds of MB to low single-digit GB.
- ~1,000,000 chunks: often many GB and noticeably slower rebuild/query operations on typical laptops.

Estimate corpus size before indexing:

- Default chunk sizing uses 1200 chars with 200 overlap, so effective step is about 1000 chars.
- Quick estimate: `estimated_chunks ~= total_markdown_chars / 1000`.

PowerShell snippet:

```powershell
$root = "C:\path\to\confluence2md\output"
$bytes = (Get-ChildItem -Path $root -Recurse -Filter *.md | Measure-Object -Property Length -Sum).Sum
$chars = [math]::Round($bytes * 0.95) # rough bytes->chars approximation for UTF-8 text
$estimatedChunks = [math]::Ceiling($chars / 1000)
"Markdown bytes: $bytes"
"Approx chars:   $chars"
"Est. chunks:    $estimatedChunks"
```

bash snippet:

```bash
ROOT="/path/to/confluence2md/output"
BYTES=$(find "$ROOT" -type f -name '*.md' -print0 | xargs -0 cat | wc -c)
CHARS=$(( BYTES * 95 / 100 ))
EST_CHUNKS=$(( (CHARS + 999) / 1000 ))
echo "Markdown bytes: $BYTES"
echo "Approx chars:   $CHARS"
echo "Est. chunks:    $EST_CHUNKS"
```

Use `Est. chunks` with the sizing ranges above to choose incremental vs rebuild cadence and to anticipate DB growth.

Operational limitations:

- Rebuild mode recreates the DB file (destructive to prior DB content at that path).
- SQLite write concurrency is limited; avoid parallel writers to the same DB file.
- Query latency grows with corpus size, filter breadth, and candidate counts.
- Vector quality and ranking behavior depend on embedding provider/model and corpus quality.

## Build, Test, and Quality Gates

```sh
task test
task coverage:check
task lint
```

Release and CI behavior:

- coverage gate enforced in CI (`COVERAGE_MIN`, default 70)
- reproducible release builds across linux/windows/darwin on amd64 and arm64
- contract tests for JSON command outputs

## Internals Documentation

- [Query examples](docs/query-examples.md)
- [Output reference](docs/output-reference.md)
- [Architecture and data flow](docs/architecture.md)
- [Operations and troubleshooting](docs/operations.md)
- [Metadata-driven search improvements](docs/metadata-search-improvements.md)
- [Support matrix](docs/support-matrix.md)
- [MCP integration decision](docs/mcp-integration-decision.md)

## Project Structure

```text
cmd/                 CLI entrypoint
internal/            internal packages (cli, service, db, indexer, query, embedding, ...)
migrations/          SQL migrations
docs/                user and design documentation
.github/workflows/   CI and release workflows
```
