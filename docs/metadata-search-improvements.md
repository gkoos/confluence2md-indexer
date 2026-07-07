# Metadata-Driven Search Improvements

Last updated: 2026-07-07

## Purpose

This document defines how metadata.json should be treated as a first-class retrieval signal.

Goal:
- Persist richer metadata from crawl output.
- Use metadata in filtering and ranking.
- Keep service-first architecture and current CLI contract style.

Out of scope:
- MCP integration
- External hosted/vector backends
- Changes to crawler behavior or crawler output format

## Current State

### Metadata fields currently consumed

From metadata.json pages entries, the indexer currently consumes:
- id (page id)
- local_path (markdown file location)
- title
- space_key
- source_url
- last_modified_at

Current code touchpoints:
- Metadata contract and page map loading: internal/indexer/indexer.go
- Ingestion mapping into indexed documents: internal/indexer/ingest.go
- Persistence and search filter paths: internal/db/db.go
- Query fusion and ranking pipeline: internal/query/query.go

### How search uses metadata today

Current retrieval behavior:
- Lexical: FTS over chunk text and selected document metadata fields.
- Vector: cosine similarity against stored embeddings.
- Hybrid: weighted or RRF fusion.

Current metadata-aware filters:
- space key
- page id
- date range (based on last_modified_at)

No explicit metadata priors are applied in ranking today.

## Gap Analysis

The crawl metadata contains additional high-value fields that are not fully used for retrieval.

High-value candidates:
- canonical_url
- created_at
- crawled_at
- created_by_name
- last_modified_by_name
- depth
- confluence_parent_id
- seed membership
- outgoing_links and incoming_links
- attachments and attachment_signature
- comments_last_fetched and related freshness signals

Missing outcomes caused by this gap:
- Cannot filter by author or content structure depth.
- Cannot prioritize seed pages or hub pages.
- Cannot boost fresh pages without over-relying on text match.
- Cannot expose metadata-aware explain diagnostics.

## Proposed Data Model Upgrades

## Phase 1: Documents table enrichment

Add or ensure persistence for:
- canonical_url
- created_at
- crawled_at
- created_by_name
- last_modified_by_name
- depth
- confluence_parent_id
- is_seed
- attachment_count
- outgoing_links_count
- incoming_links_count

Notes:
- attachment_count and link counts can be derived during ingestion.
- Timestamps should be normalized to one parse/format strategy.

## Phase 2: Optional secondary tables

If query requirements grow, add:
- document_authors table for normalized author dimensions
- document_links table for explicit graph edges
- document_labels table if labels become available in metadata output

This phase is optional and should only be added if query plans need it.

## Proposed Retrieval Upgrades

### New filters

Add query filter support for:
- author: created_by_name or last_modified_by_name
- depth range: min depth and max depth
- seed-only
- has-attachments
- updated-since

### Ranking priors (opt-in, explainable)

Introduce optional score adjustments:
- Recency prior from last_modified_at (time-decayed boost)
- Seed prior for seed pages
- Structural prior from depth and in-degree
- Attachment/comment density prior for documentation-rich pages

Design constraints:
- Keep deterministic ordering under ties.
- Keep lexical/vector/hybrid semantics intact.
- Make priors additive or multiplicative in a bounded range.

### Explain output updates

When metadata priors or metadata filters are applied, explain output should show:
- which metadata conditions were active
- which priors were active
- per-result contribution summary for metadata components

## Rollout and Compatibility

1. Add schema migration for new fields.
2. Keep old DBs readable; apply migration on open.
3. Require reindex for full metadata population.
4. Preserve existing defaults so current commands continue to work unchanged.
5. Guard new filters and priors behind explicit flags.

Behavioral compatibility:
- Existing command usage remains valid.
- Existing JSON structure remains stable, with additive fields only where needed.

## Implementation Order

1. Schema migration and DB read/write support.
2. Ingestion struct expansion and field mapping.
3. One vertical slice: author filter end-to-end.
4. Add recency prior with explain output.
5. Add remaining filters and priors.
6. Docs and examples updates.

## Test Plan

### Database and migration tests

- Migration applies cleanly from current schema.
- New columns are present and writable.
- Old DB open path remains stable.

### Ingestion tests

- Additional metadata fields map correctly from metadata.json.
- Nil and missing-field handling is safe.
- Derived counts are computed correctly.

### Query tests

- New filters narrow results as expected.
- Combined filters work with lexical, vector, and hybrid modes.
- Metadata priors change ordering in expected fixtures.
- Explain output reports active metadata factors.

### Regression checks

- Existing query behavior remains unchanged when new flags are not set.
- Stable JSON contracts remain valid.

## Scope Guardrails

Included:
- metadata.json ingestion expansion
- schema and query filter upgrades
- metadata-aware ranking priors and explainability
- tests and docs for new behavior

Excluded:
- crawler contract changes
- MCP endpoints or hosted services
- non-SQLite storage backends

## Success Criteria

This initiative is complete when:
- rich metadata fields are persisted during indexing
- at least one new metadata filter and one metadata prior are shipped
- explain output shows metadata effects
- migration, ingestion, and query tests pass
- README links to this design document
