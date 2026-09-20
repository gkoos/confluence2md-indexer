# Metadata

The crawler writes more than page text: every page carries authorship, hierarchy, link
and attachment information, and the crawl as a whole records when it ran. The indexer
reads that metadata, stores it, and uses it for filtering and for ranking. This
document describes what is implemented; the remaining ideas (ranking priors, coverage
reporting) are still tracked in
[metadata-search-improvements.md](metadata-search-improvements.md).

## What is read

Two sources, merged per page:

1. **`metadata.json`** (authoritative). Per page: `title`, `space_key`, `local_path`,
   `last_modified_at`, `source_url`, `canonical_url`, `host`, `version`, `depth`,
   `confluence_parent_id`, `created_at`, `crawled_at`, `created_by_name`,
   `last_modified_by_name`, `outgoing_links`, `incoming_links`, `attachments`,
   `comment_count`. At the root: `seed_page_ids`, `crawl_started_at`,
   `last_completed_crawl_completed_at`, `last_completed_crawl_mode`,
   `last_successful_crawl_completed_at`.
2. **Markdown front matter** (fills gaps). `canonical_url`, `is_seed`, `crawled_at`,
   `created_at`, `created_by`, `last_modified_by`, `confluence_parent_id`,
   `comment_count`, `attachments`. A page whose `metadata.json` record already carries
   a value keeps the JSON value.

Derived values:

- `link_in` and `link_out` are the lengths of `incoming_links` and `outgoing_links`.
- `attachment_count` and `comment_count` prefer the JSON record and fall back to the
  front matter.
- `is_seed` comes from `seed_page_ids`; when a crawler writes no seed list, the front
  matter's `is_seed` is used instead.

A crawler that omits a field leaves it at its zero value. Zero therefore means "not
reported" as often as it means "none", which is why every metadata filter is opt-in and
why `--depth-min 1` is the documented way to exclude seed pages.

## What is stored

`documents` gains one column per stored value (all `NOT NULL` with a zero default):
`canonical_url`, `host`, `created_at`, `crawled_at`, `version`, `depth`, `parent_id`,
`created_by_name`, `modified_by_name`, `is_seed`, `link_in`, `link_out`,
`attachment_count`, `comment_count`, plus `metadata_hash`. Indexes cover `space_key`,
`page_id`, `last_modified_at`, `host`, `depth`, `is_seed` and the two author columns
(case-insensitive).

`corpus_snapshot` records the crawl each run indexed: start/completion timestamps, its
mode, and the seed and page counts. `stats` reports the most recent one next to a
metadata coverage summary; `index` stays silent about it.

### Freshness and coverage

`stats` answers two questions about an index:

- **How stale is it?** The `corpus` block reports the crawl the index was built from
  (`crawlMode`, `crawlStartedAt`, `crawlCompletedAt`, `crawlSucceededAt`, `seedCount`,
  `pageCount`) beside `indexedAt`, the moment the indexing run started. A
  `crawlCompletedAt` later than `indexedAt` means the corpus moved on after the index was
  written, so re-run `index`.
- **How much metadata does it hold?** The `metadata` block counts the pages carrying each
  value: `withAuthors`, `withLinks`, `withAttachments`, `withComments`, `seeds`, `nested`
  (pages below the seed level) and the number of distinct `hosts`. Zero means "none, or
  the crawler did not report it", so compare the counts with the crawler output before
  reading anything into a filter that matches nothing.

```text
metadata: authors=3 links=0 attachments=2 comments=0 seeds=1 nested=2 hosts=2
corpus: mode=updates pages=3 seeds=1 indexed 2026-09-20T19:04:13Z crawl completed 2026-05-22T10:20:03Z
```

Both blocks are omitted when there is nothing to report, and neither appears in `index`
output: an indexing run never warns about freshness.

`chunks_fts` indexes `text`, `title` and `section`, where `section` is the heading
breadcrumb the chunk sits under (`Deployment > Rollback`). Headings inside fenced code
blocks are content, so they neither split a section nor enter the breadcrumb. Page id
and space key are **not** indexed: they are filters, and indexing them made a space key
match every page in that space.

`metadata_hash` fingerprints every stored metadata value, including the ones that also
feed the search index (title, space, URL, dates), so a run can distinguish three cases.

## How a run decides what to do

| Text (`content_hash`) | Metadata (`metadata_hash`) | Vectors | Result |
| --- | --- | --- | --- |
| changed | any | any | full rewrite: chunks, full-text rows and embeddings (`updated`, or `inserted` when new) |
| unchanged | changed | complete | metadata refresh only: the document row and the indexed title (`metadata`) |
| unchanged | unchanged | complete | nothing (`skipped`) |
| unchanged | changed or not | incomplete | full rewrite, so the missing vectors are recreated |

A metadata-only refresh costs SQL, not embedding calls, which is what makes a re-crawl
that only renamed pages or filled in authors cheap. When only the title moved, the title
copy in the full-text index is updated in the same transaction, so the new name is
searchable immediately. Index output counts those pages separately:

```sh
confluence2md-indexer index ./output --json   # documents.metadata, and "metadata refreshed: N" in text output
```

## How retrieval uses it

### The lexical query

`--q` is turned into an FTS5 expression: every whitespace-separated token becomes one
quoted term, and the terms are combined with `OR`.

- Punctuation is removed, so `release-train` becomes the phrase `"release train"`,
  `c++` becomes `"c"`, and `apple:` or `apple-banana` are ordinary searches instead of
  SQLite errors.
- A quoted phrase stays a phrase: `"rollback procedure"` matches the words next to each
  other.
- A trailing `*` makes a prefix search: `deploy*`.
- Words that FTS5 reserves (`AND`, `OR`, `NOT`, `NEAR`) are ordinary terms, so
  `apple OR` searches for *apple* or *or* rather than failing.
- At most 32 terms are used.

Ranking uses weighted BM25: body text counts once, the heading breadcrumb twice, and
the page title four times. A document that matches more terms still ranks above one that
matches fewer.

### Filters

All of these narrow both the lexical and the vector channel, and a filter that is not
passed changes nothing:

| flag | JSON key | meaning |
| --- | --- | --- |
| `--space <key>` (repeatable) | `Spaces` | any of these `space_key` values |
| `--page-id <id>` | `PageID` | one page |
| `--host <host>` | `Host` | one crawled site; matters when a corpus spans hosts, where space keys can repeat |
| `--author <name>` | `Author` | creator *or* last modifier, case-insensitive |
| `--created-by <name>` | `CreatedBy` | creator only |
| `--modified-by <name>` | `ModifiedBy` | last modifier only |
| `--depth-min N` | `DepthMin` | crawl depth at least N; `1` excludes seed pages |
| `--depth-max N` | `DepthMax` | crawl depth at most N |
| `--seed-only` | `SeedOnly` | only the pages the crawl started from |
| `--has-attachments` | `HasAttachments` | pages that carry at least one attachment |
| `--from` / `--to` | `FromDate` / `ToDate` | `last_modified_at` bounds (`YYYY-MM-DD`) |
| `--updated-since 30d` | `UpdatedSince` | `last_modified_at` within an age: `30d`, `2w`, `12h`, `90m`; resolved to a timestamp before the query runs |

Filters are applied in SQL, so they narrow the candidate set before ranking, and they
appear in the echoed `request.filters` block of JSON output.

### Result ranking

Both channels are min-max normalised and then combined (weighted `--alpha`, or
`--fusion rrf`). A channel is only consulted when it produced a positive score: rows
with no similarity at all are dropped, while the weakest *real* match is kept, because
normalisation maps it to exactly zero.

### Ranking priors

Text relevance and fusion decide the ranking on their own. An optional set of metadata
priors can nudge that ordering:

```sh
confluence2md-indexer query --db ./confluence2md-index.db --q "rotate secrets" \
  --priors recency,authority --prior-strength 0.15 --recency-half-life 180d --explain
```

| prior | value | meaning |
| --- | --- | --- |
| `recency` | `1 / (1 + age / half-life)` over `last_modified_at` | a page touched recently ranks above an equally relevant stale one; `--recency-half-life` (default `180d`) is the age at which the value halves |
| `authority` | `log1p(link_in) / log1p(10)`, capped at 1 | often-linked pages are hubs |
| `seed` | `1` for a page in `seed_page_ids`, else `0` | the crawl's entry points |
| `depth` | `1 / (1 + depth)` | shallower pages sit closer to an entry point |
| `richness` | `min(1, (attachments + comments) / 5)` | pages with more material attached or discussed |

How they act:

- Off by default: ranking is unchanged until `--priors` names them.
- The same settings can live in the `query` section of `config.yaml` (`priors`,
  `prior_strength`, `recency_half_life`); a flag you pass still wins, and
  `--priors ""` clears a configured list for one query.
- Applied after fusion and before truncation, as a **multiplicative** adjustment,
  `score × (1 + strength × mean(prior values))`. The mean keeps adding a prior from
  inflating the boost, and the multiplication keeps the effect proportional, so a
  weighted score near 1.0 and an RRF score near 0.03 behave the same way.
- Bounded by `--prior-strength` (default 0.15, maximum 1), so a prior can reorder
  near-ties but cannot outrank a clearly better text match.
- A missing field carries no signal: a page without a timestamp, links or attachments
  contributes 0 for that prior. Recency decays asymptotically, so a very old page keeps
  a sliver rather than dropping to nothing.
- Deterministic: equal scores with equal prior values still fall back to the chunk id,
  and the same inputs always produce the same order.
- `--explain` reports the active priors, the resolved strength and half-life, and the
  factors behind the top result. JSON results carry `metadataBoost` and
  `metadataFactors` when priors are on, and omit both when they are off.

## Schema versioning and rebuilds

The schema is versioned through `PRAGMA user_version`, and this build writes version 1.
A database stamped with any other version is refused rather than upgraded in place:

```text
query: database schema 0 is not the schema this build writes (1); rebuild the index with: confluence2md-indexer index <folder> --rebuild
```

That is deliberate: a rebuild from the crawler output is deterministic, and it is the
only path that fills the metadata columns for pages indexed before those columns
existed. `index --rebuild` deletes the database file, so a rebuild always starts from
the current schema. Running `index` without `--rebuild` against an older file reports
the same message.

## Troubleshooting

- **A metadata filter returns nothing.** Check that the crawler actually wrote the
  field: `depth`, `host`, `version` and the link lists come only from `metadata.json`,
  and an older crawler may not write them. Compare with the same query without filters.
- **`--depth-min 1` hides my main pages.** Depth 0 is the seed level, so those pages are
  excluded on purpose.
- **`--author` finds nothing although the page lists an author.** Matching is
  case-insensitive but exact otherwise, including punctuation, and it compares display
  names: a crawl that failed name resolution keeps account ids instead.
- **A query returns more results than it used to.** Multi-word queries are `OR`-joined
  now instead of requiring the words to be adjacent; use `"two words"` when adjacency is
  what you mean.

