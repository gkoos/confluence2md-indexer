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
mode, and the seed and page counts. Reporting those values in `stats` is still pending.

`chunks_fts` indexes `text`, `title` and `section`, where `section` is the heading
breadcrumb the chunk sits under (`Deployment > Rollback`). Page id and space key are
**not** indexed: they are filters, and indexing them made a space key match every page
in that space.

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

