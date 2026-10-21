# Performance: the Vector Channel

What a vector query costs, where that time goes, and which levers are worth pulling.
The figures below are measured, not estimated; the command to reproduce them is at the
end. For how the query pipeline fits together, see [Architecture and data flow](architecture.md#query-command-flow).

## How a vector query runs

`SearchVector` ([internal/db/db.go](../internal/db/db.go)) is a single statement:

```sql
SELECT <19 candidate columns>, e.dimension, e.vector
FROM embeddings e
JOIN chunks c ON c.id = e.chunk_id
JOIN documents d ON d.id = c.document_id
WHERE <filters> AND e.name = ?
```

Every row of the embedding identity is fetched, decoded from BLOB to `[]float32`,
scored with cosine, appended to a slice, and only then sorted and truncated to
`candidateK` (50 by default). There is no ANN index and no `LIMIT`, so the cost is
linear in the chunks of that identity. Metadata filters are SQL predicates on the
joined rows, which is why the joins cannot simply be dropped when filters are in play.

## Measurements

Windows, AMD Ryzen 7 5800H (8C/16T), `go1.26.9 windows/amd64`, `GOMAXPROCS=16`,
`-benchtime 5x`. Synthetic corpus: 1200-byte chunk text (matching the default chunk
size), deterministic unit vectors. Treat ±5% as noise.

### End to end, `candidateK = 50`

| chunks | dim | time/query | bytes scanned | scan rate | B/op | allocs/op |
|---|---|---|---|---|---|---|
| 10k | 256 | 125 ms | 22.2 MB | 177 MB/s | 88.7 MB | 546,860 |
| 10k | 1024 | 195 ms | 53.0 MB | 271 MB/s | 181.2 MB | 554,775 |
| 100k | 256 | 2118 ms | 222.4 MB | 105 MB/s | 906.8 MB | 5,468,018 |
| 100k | 1024 | 2668 ms | 529.6 MB | 199 MB/s | 1832.2 MB | 5,547,133 |

"Bytes scanned" is `chunks × (4·dim + 1200)`: the vector BLOB plus one chunk of text.
`B/op` is what Go actually allocated, which is 3.5–4× that payload — the per-row
projection and the driver's string/int copies allocate, not the vectors.

### Components

| component | per row | per query at 100k |
|---|---|---|
| `BenchmarkCosine` (256 / 1024 dims) | 260 ns / 1160 ns | 26 ms / 116 ms |
| `BenchmarkDecodeVector` (256 / 1024 dims) | 420 ns (1 alloc, 1 KB) / 1440 ns (1 alloc, 4 KB) | 42 ms / 144 ms |
| `BenchmarkCandidateSorting` (100k rows) | 2.04 µs | 204 ms (27 MB, 4 allocs) |
| SQL fetch + projection + per-row scan (derived) | 12.4–18.5 µs | 1846 ms / 2204 ms |

### Share of a query

| corpus/dim | SQL + projection + scan | sort | decode | cosine |
|---|---|---|---|---|
| 10k/256 | ~81% | ~13% (est.) | 3.4% | 2.1% |
| 10k/1024 | 78% | ~8% (est.) | 7.4% | 5.9% |
| 100k/256 | **87%** | 9.6% | 2.0% | 1.2% |
| 100k/1024 | **83%** | 7.6% | 5.4% | 4.3% |

## What the numbers say

- **The scan dominates.** 78–87% of a vector query is fetching, projecting and scanning
  metadata columns that only the retained `candidateK` rows need. Cosine is 1–6% and
  BLOB decode 2–7%.
- **Cost is per row, not per corpus.** 14–18 µs and ~55 allocations per row at every
  size measured, so a query on a 100k-chunk corpus costs ~2.7 s on this machine.
- **Ranking every row is a secondary cost.** Copying and sorting the full result set is
  9–13% at 100k, even though only 50 rows survive.
- **Quantization/ANN is not what this data calls for yet.** The vector arithmetic is
  under a tenth of the query; the metadata projection is the larger term by an order of
  magnitude.

## Where the headroom is, in order

1. **Project fewer columns and hydrate after ranking** — scan `chunk_id, dimension,
   vector`, keep the top `candidateK`, then fetch the metadata for those rows in one
   follow-up query. Attacks the 78–87% term.
2. **Bound the ranking** — a size-`candidateK` selection instead of copying and sorting
   every row. Removes most of the 9–13% sort share with no SQL change.
3. **Reuse one decode buffer** — `decodeFloat32Vector` allocates once per row; at 100k
   rows that is 100k allocations per query.
4. **Cheaper cosine** — accumulate in `float32` or hoist the `float64` conversions;
   small, and needs a numeric-equivalence test.
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
