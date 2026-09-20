# Operations and Troubleshooting

This guide covers routine operations and common issues.

For command output field definitions, see [output-reference.md](output-reference.md).

## Typical Workflow

1. Export or refresh content using `confluence2md`.
2. Run incremental indexing:

```sh
confluence2md-indexer index ./output
```

3. Execute retrieval queries:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "search terms"
```

4. Inspect index health:

```sh
confluence2md-indexer stats --db ./output/confluence2md-index.db
```

## Rebuild Strategy

Re-indexing is incremental by default: a document is skipped only when both its
content and its embedding identity match what is already stored, so changing
provider re-embeds the corpus without `--rebuild`.

Use full rebuild when:

- you want to start from an empty database
- the DB file was written by an older build whose schema predates embedding identity tracking
- index content looks inconsistent

In rebuild mode the DB file is recreated before indexing, so all pages, chunks and
embeddings are written from scratch. This is destructive to the DB file at that path.

```sh
confluence2md-indexer index ./output --rebuild
```

## JSON for Automation

Prefer `--json` for scripts and pipelines:

```sh
confluence2md-indexer index ./output --json
confluence2md-indexer query --db ./output/confluence2md-index.db --q "topic" --json
confluence2md-indexer stats --db ./output/confluence2md-index.db --json
```

## Quality Checks

Run before commits or releases:

```sh
go test ./...
go vet ./...
task coverage:check
```

## Common Issues

### Missing metadata or markdown files

Symptoms:

- index preflight failure
- missing file errors

Actions:

- confirm `metadata.json` exists in input folder
- verify every `local_path` points to a real markdown file

### Empty or poor query results

Actions:

- confirm indexing completed successfully
- run `stats` and verify documents/chunks/embeddings counts
- try `--mode lexical` for exact term matches
- increase candidate pool with `--candidate-k`
- add `--expand` for broader local context

### Date filter errors

Symptoms:

- `query --from must be YYYY-MM-DD`
- `query date range invalid: --from must be <= --to`

Actions:

- use date format `YYYY-MM-DD`
- ensure lower bound is not after upper bound

### Embedding mismatch

Symptoms:

- exit code 2 with `query: embedding mismatch: index vectors are "A" but the configured provider is "B"`

Cause:

The index was built with different embedding settings than the query is using. Vectors from different models or dimensions are not comparable, so the query refuses to run instead of returning empty results.

Actions:

- pass the same `--embedding*` flags to `query` that you used for `index`, or set `CONFLUENCE2MD_EMBEDDING_*` once so both commands agree
- inspect the index with `stats` (`embedding identity`, `vector capability`)
- re-index with the settings you want, then query with those same settings
- use `--lexical-only` when only term matching is needed

### Vector channel missing

Symptoms:

- exit code 2 with `query: the index holds no embeddings to search with "..."`.
- `stats` reports `vector ready: false`

Actions:

- the index was built with `--skip-embeddings`; re-index without it
- confirm the provider worked during indexing by checking the `embeddings written` line in index output

### Choosing offline or hosted embeddings

- The default provider is offline and needs no configuration: `bow-local`, a local bag-of-words hashing provider whose vectors measure term overlap.
- For semantic similarity use `--embedding openai`, or `--embedding openai-compatible` against a local server (Ollama, LM Studio, llama.cpp, vLLM) or a hosted OpenAI-compatible API.
- A hosted setup can be written once in `config.yaml` instead of on every command line; see [Configuration File](#configuration-file).
- Setup recipes and the flag reference are in [embedding-providers.md](embedding-providers.md).

### DB path confusion

Notes:

- `index` default DB path is created under the input folder
- `query` and `stats` default to `./confluence2md-index.db` in current directory
- `db.path` in `config.yaml` replaces those defaults whenever `--db` is not passed

Recommendation:

- pass `--db` explicitly in scripted usage
- or set `db.path` once in `config.yaml` so every subcommand agrees on the location

## Configuration File

`db` and `embedding` settings can be written once in an optional YAML file instead of on every command line.
Copy `config.example.yaml` to `config.yaml` in the directory you run from, pass `--config <file>`, or name a
file with `CONFLUENCE2MD_CONFIG`. `config.yaml` is git-ignored, so it may hold a credential; the example is
committed and must stay free of one.

Release builds resolve a setting once, in this order:

1. a flag you passed - presence counts, so a flag with an empty value still wins
2. an environment variable set to a non-empty value
3. a key present in the file
4. the built-in default

| file key | equivalent | notes |
| --- | --- | --- |
| `db.path` | `--db` | used by index, query and stats when `--db` is not passed; empty keeps the database inside the indexed folder |
| `embedding.provider` | `--embedding` / `PROVIDER` | provider id |
| `embedding.model` | `--embedding-model` / `MODEL` | model name |
| `embedding.base_url` | `--embedding-base-url` / `BASE_URL` | endpoint base URL |
| `embedding.path` | `--embedding-path` / `PATH` | path appended to the base URL |
| `embedding.dimension` | `--embedding-dim` / `DIM` | vector size; it must match the index or the mismatch guard stops the query |
| `embedding.api_key_env` | `--embedding-api-key-env` / `API_KEY_ENV` | name of the variable that holds the credential |
| `embedding.api_key` | - | literal credential; set it only when a variable is not an option |
| `embedding.auth_header`, `embedding.auth_scheme` | `AUTH_HEADER`, `AUTH_SCHEME` | auth decoration for gateways such as Azure OpenAI |
| `embedding.headers`, `embedding.query_params` | `HEADERS`, `QUERY_PARAMS` | YAML lists of `key=value` entries |
| `embedding.document_prefix`, `embedding.query_prefix` | `DOCUMENT_PREFIX`, `QUERY_PREFIX` | asymmetric prefixes for E5-style models |
| `embedding.batch_size` | `BATCH_SIZE` | inputs per request |
| `embedding.timeout` | `TIMEOUT` | duration with a unit, for example `45s` or `2m` |
| `embedding.max_retries` | `MAX_RETRIES` | retries after the first attempt (`-1` disables them) |
| `embedding.skip` | `--skip-embeddings` / `SKIP` | index text only and leave the vector channel empty |

Rules worth knowing:

- Credentials: name a variable with `api_key_env` (preferred) or store a literal `api_key`. Setting both is
  rejected, because the winner would be ambiguous. An environment variable that is set but empty counts as
  unset; to clear a value the file supplies, pass the flag with an empty value, for example
  `--embedding-document-prefix ""`.
- Format: keys are `snake_case`, durations carry a unit, and `headers` / `query_params` are YAML lists rather
  than one comma-separated string. The format follows the file extension.
- Unknown keys and malformed values fail with exit code 2 and name the offending key, so a typo cannot quietly
  fall back to a default.
- Only `db` and `embedding` are read from the file. Retrieval settings such as `--mode`, `--fusion`, `--top-k`
  and pagination remain command-line flags.
- `embedding.source` in index JSON output reports the layer that chose the provider id: `flag`, `env`, `config`
  or `default`.
- The file is optional. Tests and the offline smoke gate pin it to an empty file, so a developer's `config.yaml`
  never changes what the gates observe.

## CI and Release Notes

- CI enforces minimum coverage via `task coverage:check`
- release pipeline builds reproducible binaries across supported targets
- support targets and gates are described in [support-matrix.md](support-matrix.md)
