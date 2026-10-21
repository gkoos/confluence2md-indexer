# Performance: the Vector Channel

What a vector query costs, where that time goes, and which levers are worth pulling.
The figures below are measured, not estimated; the command to reproduce them is at the
end. For how the query pipeline fits together, see [Architecture and data flow](architecture.md#query-command-flow).

## How a vector query runs

`SearchVector` ([internal/db/db.go](../internal/db/db.go)) runs in two phases.

**Scan** — ids and vectors only:

```sql
SELECT e.chunk_id, e.dimension, e.vector
FROM embeddings e
JOIN chunks c ON c.id = e.chunk_id
JOIN documents d ON d.id = c.document_id
WHERE <filters> AND e.name = ?
```

Every row of the embedding identity is fetched, decoded from BLOB to `[]float32` and
scored with cosine. The joins stay because metadata filters are SQL predicates on the
joined rows; what the scan does *not* do is project the 19 candidate columns, which is
what used to dominate a query (see the after table below). There is no ANN index and no
`LIMIT`, so the scan is linear in the chunks of that identity.

**Hydrate** — one follow-up query for the survivors:

```sql
SELECT <19 candidate columns> FROM chunks c JOIN documents d ON d.id = c.document_id
WHERE c.id IN (?, …)
```

The retained `candidateK` chunks (50 by default, batched at 500 ids) are hydrated through
the same `candidateColumns`/`scanCandidate` pair the lexical channel uses, so priors and
fusion see exactly the fields they expect.

## Measurements

Windows, AMD Ryzen 7 5800H (8C/16T), `go1.26.9 windows/amd64`, `GOMAXPROCS=16`,
`-benchtime 5x`. Synthetic corpus: 1200-byte chunk text (matching the default chunk
size), deterministic unit vectors. Treat ±5% as noise, and note that absolute values
move with machine load — compare numbers measured back to back, not across sessions.

### End to end, `candidateK = 50`

| chunks | dim | time/query | bytes scanned | scan rate | B/op | allocs/op |
|---|---|---|---|---|---|---|
| 10k | 256 | 125 ms | 22.2 MB | 177 MB/s | 88.7 MB | 546,860 |
| 10k | 1024 | 194 ms | 53.0 MB | 271 MB/s | 181.2 MB | 554,776 |
| 100k | 256 | 1304 ms | 222.4 MB | 105 MB/s | 906.8 MB | 5,468,019 |
| 100k | 1024 | 1927 ms | 529.6 MB | 199 MB/s | 1832.2 MB | 5,547,135 |

"Bytes scanned" is `chunks × (4·dim + 1200)`: the vector BLOB plus one chunk of text.
`B/op` is what Go actually allocated, which was 3.5–4× that payload when the scan still
projected every candidate column.

### Components

| component | per row | per query at 100k |
|---|---|---|
| `BenchmarkCosine` (256 / 1024 dims) | 260 ns / 1160 ns | 26 ms / 116 ms |
| `BenchmarkDecodeVector` (256 / 1024 dims) | 420 ns (1 alloc, 1 KB) / 1440 ns (1 alloc, 4 KB) | 42 ms / 144 ms |
| `BenchmarkCandidateSorting` (100k full candidates) | 2.0 µs | ~200 ms (27 MB, 5 allocs) |
| SQL fetch + projection + per-row scan (derived) | 12.4–18.5 µs | 1846 ms / 2204 ms |

`BenchmarkCandidateSorting` measures the pre-two-phase ranking shape (copy and sort
100k complete `Candidate` values). Production now sorts 100k lightweight
`(chunk_id, score)` pairs before hydrating, so that row is a historical upper bound,
not the current cost.

## After: the two-phase scan

Same session, same command, before and after moving the projection into a hydration
query (the state described above):

| chunks | dim | before | after | change | B/op | allocs/op |
|---|---|---|---|---|---|---|
| 10k | 256 | 125 ms | 70 ms | −44% | 88.7 → 33.5 MB | 546,860 → 129,328 |
| 10k | 1024 | 194 ms | 148 ms | −24% | 181.2 → 126.1 MB | 554,776 → 137,237 |
| 100k | 256 | 1304 ms | 790 ms | −39% | 906.8 → 336.4 MB | 5,468,019 → 1,270,496 |
| 100k | 1024 | 1927 ms | 1691 ms | −12% | 1832.2 → 1261.9 MB | 5,547,135 → 1,349,669 |

Allocations fall by ~76% at both dimensions and allocated bytes by 31–63%, but the time
win depends on dimension: at 256 dims the removed text and metadata were a large share
of the scan, while at 1024 dims what remains is dominated by the per-row vector BLOB —
4 KB to fetch, decode (144 ms at 100k) and run through cosine (116 ms at 100k).

## What the numbers say

- **The projection was the dominant term** and is gone: 78–87% of a query before, with
  ~55 allocations per row; hydration now costs 50 rows per query instead of 100k.
- **Dimension decides what is left.** At 256 dims the two-phase scan is ~1.65× faster;
  at 1024 dims only ~1.14×, because the vector bytes themselves dominate.
- **Quantization/ANN is still not the next step.** At 100k/1024 the remaining ~1.7 s is
  mostly fetching and decoding 410 MB of vectors, not arithmetic; cheaper decode and
  cosine come first.

## Where the headroom is, in order

1. ~~Project fewer columns and hydrate after ranking~~ — done; see the after table.
2. **Reuse one decode buffer** — `decodeFloat32Vector` still allocates once per row, so
   at 100k rows that is 100k allocations and 410 MB of `[]float32` per query at 1024
   dims. The largest remaining per-row allocation.
3. **Cheaper cosine** — accumulate in `float32` or hoist the `float64` conversions;
   ~116 ms per 100k rows at 1024 dims, and needs a numeric-equivalence test.
4. **Bound the ranking** — production now sorts 100k lightweight pairs; a size-`candidateK`
   selection would remove that too, but it is no longer a large share.
5. **Index `embeddings(name)`** — only pays off when several identities coexist in one
   database; with a single provider the predicate matches every row anyway.

## Reproducing

```sh
go test ./internal/db -run '^$' \
  -bench 'BenchmarkSearchVector|BenchmarkCosine|BenchmarkDecodeVector|BenchmarkCandidateSorting' \
  -benchmem -benchtime 5x
```

`-short` skips the 100k shapes, which need about 0.5 GB of temporary disk. The fixture
writes documents, chunks and embeddings directly and leaves `chunks_fts` empty: the
vector channel never reads the full-text table, and indexing 120 MB of text into FTS5
dominated setup by an order of magnitude.

Benchmarks do not run in CI; they describe a single machine and are meant for relative
comparison, not as absolute guarantees.

