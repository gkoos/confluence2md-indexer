# Query Examples

This guide shows practical `confluence2md-indexer query` command patterns for local retrieval workflows.

For field-by-field output details, see [output-reference.md](output-reference.md).

## Prerequisites

- You already ran indexing at least once.
- You know the DB path (default when indexing folder `./output` is `./output/confluence2md-index.db`).

## Basic Query

Run hybrid retrieval with defaults:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "api key rotation"
```

## Choose Retrieval Mode

Lexical only:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "incident runbook" --mode lexical
```

Vector only:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "service outage playbook" --mode vector
```

Hybrid (default):

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "service outage playbook" --mode hybrid
```

## Control Fusion Strategy

Weighted fusion with custom alpha:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "rotation policy" --fusion weighted --alpha 0.80
```

RRF fusion:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "rotation policy" --fusion rrf --rrf-k 60
```

## Fusion Strategies (When to Use Which)

### Weighted (`--fusion weighted`)

Use weighted fusion when you want direct control over lexical vs vector influence.

- `--alpha` controls lexical weight.
- Vector weight is `1 - alpha`.

Practical defaults:

- Start with `--alpha 0.70`.
- Increase toward `0.85-0.95` for keyword-heavy queries.
- Decrease toward `0.50-0.65` for intent/semantic-heavy queries.

### RRF (`--fusion rrf`)

Use RRF when you want robust blending by rank position instead of score magnitude.

- Useful when lexical and vector score scales behave very differently.
- Often produces stable mixed top-k lists across varied query types.

Practical defaults:

- Start with `--rrf-k 60`.
- Lower `rrf-k` increases the impact of top-ranked channel hits.
- Higher `rrf-k` smooths rank contribution differences.

### Quick Comparison Commands

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "repository_dispatch" --mode hybrid --fusion weighted --alpha 0.70 --explain
confluence2md-indexer query --db ./output/confluence2md-index.db --q "repository_dispatch" --mode hybrid --fusion rrf --rrf-k 60 --explain
```

Compare `fusion=...`, top result order, and top-gap in explain output.

## Filter Results

By space key:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "secrets" --space SRE
```

By page ID:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "token" --page-id 123456
```

By date range:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "deployment" --from 2026-01-01 --to 2026-06-30
```

## Pagination

Return the first 10 ranked items:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "incident" --top-k 10
```

Return 10 items starting from rank offset 20:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "incident" --offset 20 --limit 10
```

Notes:

- `--offset` is zero-based.
- `--limit 0` means use `--top-k`.

## Context Expansion

Include one adjacent chunk on each side of ranked hits:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "certificate renewal" --expand 1
```

This preserves ranking while broadening chunk text context.

## Explain Diagnostics

Show explain diagnostics in text mode:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "certificate renewal" --explain
```

Include explain details in JSON mode:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "certificate renewal" --json --explain
```

## JSON Output for Automation

Emit stable machine-readable output:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "on-call handover" --json
```

The payload includes:

- `schemaVersion`
- `request`
- `count`
- `total`
- `pagination`
- `results`

## Common Validation Errors

- Missing query: `query requires --q`
- Invalid mode: `query --mode must be one of: hybrid, lexical, vector`
- Invalid fusion: `query --fusion must be one of: weighted, rrf`
- Invalid date format: `query --from must be YYYY-MM-DD` (same for `--to`)
- Invalid date range: `query date range invalid: --from must be <= --to`
