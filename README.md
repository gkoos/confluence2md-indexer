# `confluence2md-indexer` - Local Hybrid Search for `confluence2md` Exports

[![CI](https://github.com/gkoos/confluence2md-indexer/actions/workflows/ci.yml/badge.svg)](https://github.com/gkoos/confluence2md-indexer/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/gkoos/confluence2md-indexer)](https://github.com/gkoos/confluence2md-indexer/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/gkoos/confluence2md-indexer/blob/main/LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/gkoos/confluence2md-indexer)](https://github.com/gkoos/confluence2md-indexer/blob/main/go.mod)

Build a local SQLite index from [confluence2md](https://github.com/gkoos/confluence2md) output and run fast lexical, vector, and hybrid retrieval from the command line.

## Part of the `confluence2md` Platform

`confluence2md-indexer` is the second step in a three-tool local Confluence knowledge pipeline. Feed it the output of [`confluence2md`](https://github.com/gkoos/confluence2md), then connect [`confluence2md-mcp`](https://github.com/gkoos/confluence2md-mcp) to query the index from any AI client. See [docs/platform.md](docs/platform.md) for the full architecture.

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
- Records the embedding identity with every vector, so switching provider re-embeds the corpus instead of mixing vector spaces.
- Refuses to run a vector or hybrid query against mismatched vectors instead of returning empty results.
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

- Go 1.26+ (for local builds)
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
confluence2md-indexer index [folder] [--db path] [--config file] [--rebuild] [--json] [--skip-embeddings]
confluence2md-indexer query --q text
  [--db path] [--config file]
  [--mode hybrid|lexical|vector]
  [--fusion weighted|rrf] [--alpha 0..1] [--rrf-k N]
  [--top-k N] [--candidate-k N]
  [--offset N] [--limit N]
  [--space key] [--host host] [--page-id id]
  [--author name] [--created-by name] [--modified-by name]
  [--depth-min N] [--depth-max N] [--seed-only] [--has-attachments]
  [--from YYYY-MM-DD] [--to YYYY-MM-DD] [--updated-since 30d]
  [--priors recency,authority,seed,depth,richness] [--prior-strength 0..1] [--recency-half-life 180d]
  [--expand N]
  [--json] [--explain] [--lexical-only]
  [--embedding id] [--embedding-model name] [--embedding-base-url url]
  [--embedding-dim N] [--embedding-api-key-env VAR] [--embedding-header k=v]
  [--embedding-query-param k=v] [--embedding-document-prefix text]
  [--embedding-query-prefix text] [--embedding-batch-size N]
  [--embedding-timeout dur] [--embedding-max-retries N]
  [--embedding-auth-header name] [--embedding-auth-scheme scheme] [--embedding-path path]
confluence2md-indexer stats [--db path] [--config file] [--json]
confluence2md-indexer --version
```

Use `--embedding list` to print the available providers. Index and query must be
given the same embedding settings; see [docs/embedding-providers.md](docs/embedding-providers.md).

The crawler metadata is indexed too, so a query can be narrowed by what it describes:
`--author`, `--created-by`, `--modified-by`, `--host`, repeated `--space`, `--depth-min`
and `--depth-max`, `--seed-only`, `--has-attachments` and `--updated-since 30d`. All of
them are opt-in, and all of them apply to every retrieval mode. Ranking can be nudged by
the same metadata with `--priors recency,authority,seed,depth,richness`, which is off by
default and bounded by `--prior-strength`. Retrieval defaults — mode, fusion, alpha,
`top_k`, `candidate_k`, `expand` and the priors — can live in the `query` section of
`config.yaml`; a flag you pass still wins. Details and examples:
[docs/metadata.md](docs/metadata.md).

See [docs/query-examples.md](docs/query-examples.md) for practical command patterns.

### Configuration File

`db` and `embedding` settings can also live in an optional YAML file, which keeps long command lines and
credentials out of your shell history. Copy [`config.example.yaml`](config.example.yaml) to `config.yaml` next
to where you run the tool and edit it; `config.yaml` is git-ignored, so a credential never lands in the
repository.

```sh
cp config.example.yaml config.yaml
confluence2md-indexer index ./output                        # reads ./config.yaml
confluence2md-indexer index ./output --config ~/c2md.yaml   # or a file of your choosing
```

```yaml
db:
  path: ""                # empty keeps the database inside the indexed folder
embedding:
  provider: "openai-compatible"
  model: "Qwen/Qwen3-Embedding-0.6B"
  base_url: "https://api.siliconflow.com/v1"
  dimension: 1024
  api_key_env: "SILICONFLOW_API_KEY"   # or api_key: "sk-..."; set only one of the two
```

Precedence, highest first: **flag passed** > **environment variable set** > **key present in the file** >
**built-in default**. Presence decides, not emptiness, so `--embedding-document-prefix ""` clears a value that
the file supplies.

| setting | flag | environment | file key |
| --- | --- | --- | --- |
| provider id | `--embedding` | `CONFLUENCE2MD_EMBEDDING_PROVIDER` | `embedding.provider` |
| base URL | `--embedding-base-url` | `CONFLUENCE2MD_EMBEDDING_BASE_URL` | `embedding.base_url` |
| index location | `--db` | - | `db.path` |

- Without a file the tool behaves exactly as before: flags, environment variables and built-in defaults.
  A missing `config.yaml` in the working directory is not an error, while a `--config` path that does not
  exist is, because it names a file on purpose.
- Unknown keys are rejected, so a typo never silently falls back to a default.
- `embedding.source` in index output reports the layer that decided: `flag`, `env`, `config` or `default`.
- `CONFLUENCE2MD_CONFIG=/etc/c2md/config.yaml` moves the file out of the working directory; `--config` still
  wins over both.

The complete key reference, the credential rules and the list-versus-comma caveats are in
[docs/operations.md](docs/operations.md#configuration-file).

### In-process Query API (for MCP)

You can import and call the Query path directly without spawning the CLI process:

```go
import (
	"context"

	"github.com/gkoos/confluence2md-indexer/pkg/indexerapi"
	"github.com/gkoos/confluence2md-indexer/internal/db"
)

resp, err := indexerapi.Query(context.Background(), "./output/confluence2md-index.db", indexerapi.QueryRequest{
	Text:       "how to rotate secrets",
	Mode:       "hybrid",   // hybrid | lexical | vector
	Fusion:     "weighted", // weighted | rrf
	Alpha:      0.70,       // lexical weight for weighted fusion (0..1)
	RRFK:       60,         // rank constant for rrf fusion
	TopK:       10,         // final result count
	CandidateK: 50,         // candidates per retrieval channel before fusion
	Offset:     0,          // zero-based offset into ranked results
	Limit:      0,          // page size; 0 defaults to TopK
	Expand:     0,          // adjacent chunks to include around each hit
	Filters: db.SearchFilters{
		SpaceKey: "",         // filter by space_key
		PageID:   "",         // filter by page_id
		FromDate: "",         // lower bound for last_modified_at (YYYY-MM-DD)
		ToDate:   "",         // upper bound for last_modified_at (YYYY-MM-DD)
	},
})
if err != nil {
	// handle error
}

_ = resp.Results // []indexerapi.QueryResult
_ = resp.Total   // total ranked results before pagination
```

`indexerapi.Query` opens and closes a SQLite connection per call. For occasional use this is fine. If you are issuing many queries in a single session (typical for MCP), open the database once and call `query.Run` directly:

```go
import (
	"context"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/embedding"
	"github.com/gkoos/confluence2md-indexer/internal/query"
)

database, err := db.Open("./output/confluence2md-index.db")
if err != nil {
	// handle error
}
defer database.Close()

resolution, err := embedding.Resolve(embedding.Options{})
if err != nil {
	// handle error
}
provider := resolution.Provider

results, total, err := query.Run(ctx, database, provider, query.Request{
	Text: "how to rotate secrets",
	Mode: "hybrid",
	TopK: 10,
})
```

## How It Works

### Input Contract

The indexer reads:

- `metadata.json` produced by `confluence2md`
- markdown files referenced by `metadata.pages[*].local_path`

### Indexing Flow

1. Run preflight checks on metadata and markdown paths.
2. Open or create the SQLite database and create the schema when it is absent.
3. Resolve the embedding provider from flags, environment and defaults.
4. Convert pages into chunk records.
5. Upsert documents whose content or embedding identity changed.
6. Generate embeddings for the changed chunks, in batches.
7. Remove stale records and vectors belonging to other identities.
8. Record the run, including its embedding identity, and emit the index summary.

### Query Flow

1. Parse query request, filters, and pagination options.
2. For vector and hybrid modes, resolve the embedding provider and verify it matches the identities stored in the index.
3. Run lexical search (FTS5), vector search, or both.
4. Fuse candidate scores (weighted or RRF).
5. Apply deterministic paging and optional context expansion.
6. Return results as text output or JSON contract.

## Output Contracts

- JSON outputs include `schemaVersion` for machine-readability and contract stability.
- Query JSON includes `count`, `total`, and `pagination` fields.
- Golden tests validate index/query/stats JSON contracts end to end.
- Field-by-field output reference is documented in [docs/output-reference.md](docs/output-reference.md).

## Limitations and Sizing Guidance

This tool is optimized for local developer workflows, not large multi-tenant serving.

### Embedding Provider Setup

Embedding providers are pluggable and are selected explicitly or from the environment:

- **`bow-local`** (default, offline): a local bag-of-words hashing provider. No network, no key, no cost, fully deterministic, 256 dimensions. Its vectors measure term overlap rather than meaning, so they complement BM25 instead of replacing it.
- **`openai`**: the OpenAI embeddings API, selected with `--embedding openai` and `OPENAI_API_KEY` (default model `text-embedding-3-small`). Token costs apply.
- **`openai-compatible`**: any endpoint speaking the OpenAI embeddings protocol, including Azure OpenAI, Ollama, LM Studio, llama.cpp server, vLLM and hosted providers such as SiliconFlow. Requires `--embedding-base-url`, `--embedding-model` and `--embedding-dim`.

Providers are resolved from flags first, then `CONFLUENCE2MD_EMBEDDING_*` environment variables, then the optional `embedding` section of `config.yaml`, then the offline default; `OPENAI_API_KEY` no longer selects OpenAI on its own. `--embedding list` prints the registered providers.

Index and query must resolve to the same **identity**, because vectors from different models or dimensions are not comparable. A mismatch is reported with exit code 2 instead of returning empty results, and re-indexing with a different provider re-embeds the corpus automatically.

Setup recipes, the complete flag reference and a troubleshooting table are in [docs/embedding-providers.md](docs/embedding-providers.md).

Current practical limits depend mostly on chunk count and embedding dimension.

- 256 dimensions (the offline default) cost about 1 KB per chunk.
- Larger models (1024-3072 dimensions) increase DB size accordingly, at roughly 4-12 KB per chunk.

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
- Index and query must use the same embedding identity; a mismatch is reported instead of returning empty results.
- SQLite write concurrency is limited; avoid parallel writers to the same DB file.
- Query latency grows with corpus size, filter breadth, and candidate counts.
- Vector quality and ranking behavior depend on embedding provider/model and corpus quality.

## Build, Test, and Quality Gates

```sh
task test
task coverage:check
task smoke:vector
task lint
```

Release and CI behavior:

- coverage gate enforced in CI (`COVERAGE_MIN`, default 70)
- offline vector smoke gate (`task smoke:vector`) runs in the test job
- reproducible release builds across linux/windows/darwin on amd64 and arm64
- contract tests for JSON command outputs

## Internals Documentation

- [Query examples](docs/query-examples.md)
- [Embedding providers](docs/embedding-providers.md)
- [Output reference](docs/output-reference.md)
- [Architecture and data flow](docs/architecture.md)
- [Operations and troubleshooting](docs/operations.md)
- [Metadata fields, filters and indexing](docs/metadata.md)
- [Support matrix](docs/support-matrix.md)
- [MCP integration decision](docs/mcp-integration-decision.md)

## Project Structure

```text
cmd/                 CLI entrypoint
internal/            internal packages (cli, service, db, indexer, query, embedding, ...)
docs/                user and design documentation
.github/workflows/   CI and release workflows
```
