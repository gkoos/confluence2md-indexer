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
- Setup recipes and the flag reference are in [embedding-providers.md](embedding-providers.md).

### DB path confusion

Notes:

- `index` default DB path is created under the input folder
- `query` and `stats` default to `./confluence2md-index.db` in current directory

Recommendation:

- pass `--db` explicitly in scripted usage

## CI and Release Notes

- CI enforces minimum coverage via `task coverage:check`
- release pipeline builds reproducible binaries across supported targets
- support targets and gates are described in [support-matrix.md](support-matrix.md)
