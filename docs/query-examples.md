# Query Examples

This guide shows practical `confluence2md-indexer query` command patterns for local retrieval workflows.

For field-by-field output details, see [output-reference.md](output-reference.md).

## Prerequisites

- You already ran indexing at least once.
- You know the DB path (default when indexing folder `./output` is `./output/confluence2md-index.db`).

## Embedding Providers (Index and Query Must Match)

Indexing records which embedding provider produced each vector, and querying checks the configured provider against that record. Both commands must therefore be given the same embedding settings. See [embedding-providers.md](embedding-providers.md).

Offline default, requiring no configuration:

```sh
confluence2md-indexer index ./output
confluence2md-indexer query --db ./output/confluence2md-index.db --q "rotate secrets"
```

A larger offline vector, which queries must then match:

```sh
confluence2md-indexer index ./output --embedding-dim 512
confluence2md-indexer query --db ./output/confluence2md-index.db --q "rotate secrets" --embedding-dim 512
```

OpenAI:

```sh
confluence2md-indexer index ./output --embedding openai
confluence2md-indexer query --db ./output/confluence2md-index.db --q "rotate secrets" --embedding openai
```

A local OpenAI-compatible server:

```sh
confluence2md-indexer index ./output \
  --embedding openai-compatible \
  --embedding-base-url http://127.0.0.1:11434/v1 \
  --embedding-model bge-m3 \
  --embedding-dim 1024
```

Term matching only, needing no provider at all:

```sh
confluence2md-indexer query --db ./output/confluence2md-index.db --q "rotate secrets" --lexical-only
```

A mismatch is reported instead of silently returning nothing:

```text
query: embedding mismatch: index vectors are "bow-local:fnv1a@256" but the configured provider
is "bow-local:fnv1a@512"; re-index with --rebuild, select the stored provider, or query with --mode lexical
```

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
- Conflicting mode: `query --lexical-only conflicts with --mode vector`
- Unknown provider: `unknown embedding provider "..." (available: bow-local, openai, openai-compatible)`
- Index and query disagree: `embedding mismatch: index vectors are "..." but the configured provider is "..."`
- Nothing to search: `the index holds no embeddings to search with "..."`
# Metadata filter examples

## Search only what a person wrote

```sh
confluence2md-indexer query --db ./confluence2md-index.db --q "retention policy" --author "Ada Lovelace"
confluence2md-indexer query --db ./confluence2md-index.db --q "retention policy" --created-by "Ada Lovelace"
confluence2md-indexer query --db ./confluence2md-index.db --q "retention policy" --modified-by "Grace Hopper"
```

`--author` matches either role, case-insensitively; `--created-by` and `--modified-by`
match one role each.

## Walk the crawl structure

```sh
# seeds only
confluence2md-indexer query --db ./confluence2md-index.db --q "onboarding" --seed-only

# one level below the seeds
confluence2md-indexer query --db ./confluence2md-index.db --q "onboarding" --depth-min 1 --depth-max 1
```

## Narrow by content shape and freshness

```sh
confluence2md-indexer query --db ./confluence2md-index.db --q "design" --has-attachments
confluence2md-indexer query --db ./confluence2md-index.db --q "design" --updated-since 90d
confluence2md-indexer query --db ./confluence2md-index.db --q "design" --updated-since 2w --json
```

## Corpora that span sites

```sh
confluence2md-indexer query --db ./confluence2md-index.db --q "release notes" --host docs.example.com
confluence2md-indexer query --db ./confluence2md-index.db --q "release notes" --space OPS --space ENG
```

## Combine filters with retrieval modes

```sh
confluence2md-indexer query --db ./confluence2md-index.db --q "rotate secrets" --mode hybrid --fusion rrf \
  --author "Ada Lovelace" --depth-max 2 --top-k 5 --json --explain
```

The JSON payload echoes the active filters under `request.filters`, which makes a
scripted search self-describing.


## Nudge the ranking with metadata

```sh
# prefer recently touched pages when scores are close
confluence2md-indexer query --db ./confluence2md-index.db --q "retention policy" --priors recency

# prefer the crawl's entry points and hubs, and show what moved
confluence2md-indexer query --db ./confluence2md-index.db --q "retention policy" \
  --priors seed,authority --explain

# a stronger nudge, with a shorter recency memory
confluence2md-indexer query --db ./confluence2md-index.db --q "retention policy" \
  --priors recency --prior-strength 0.3 --recency-half-life 30d
```

Priors never replace text relevance: they scale the fused score by at most
`--prior-strength` (default 0.15), so two clearly different text matches keep their
order. `--explain` lists the active priors and the factors behind the top result, and
JSON output carries `metadataBoost` and `metadataFactors` per result when priors are on.

