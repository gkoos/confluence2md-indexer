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

Use full rebuild when:

- schema assumptions changed
- index content looks inconsistent
- you intentionally want a full reset

In rebuild mode, the DB file is recreated before indexing so all pages/chunks/embeddings are written from scratch.

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
