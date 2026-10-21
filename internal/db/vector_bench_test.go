package db

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"math/rand/v2"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Benchmarks for the vector scan behind SearchVector (issue #26). They seed a
// temporary database directly, so they are offline and deterministic: no provider,
// no API key, no network, and every vector comes from a fixed PRNG seed.
//
// The 100k shapes carry roughly 0.5 GB of BLOBs, so `go test -short` skips them.

const (
	benchChunksPerDocument = 100
	benchChunkTextSize     = 1200 // matches indexer.DefaultChunkSize
	benchCandidateK        = 50
	benchTimestamp         = "2026-01-01T00:00:00Z"
)

// vectorBenchmarkCase is one corpus shape from the issue.
type vectorBenchmarkCase struct {
	name   string
	chunks int
	dim    int
}

func vectorBenchmarkCases() []vectorBenchmarkCase {
	return []vectorBenchmarkCase{
		{name: "10k/256", chunks: 10_000, dim: 256},
		{name: "10k/1024", chunks: 10_000, dim: 1024},
		{name: "100k/256", chunks: 100_000, dim: 256},
		{name: "100k/1024", chunks: 100_000, dim: 1024},
	}
}

func benchEmbeddingName(dim int) string {
	return fmt.Sprintf("bench:unit@%d", dim)
}

// benchVectorBytes reports how many bytes one query pulls out of SQLite: the vector
// BLOB plus the chunk text that candidateColumns projects.
func benchVectorBytes(chunks, dim int) int64 {
	return int64(chunks) * int64(dim*4+benchChunkTextSize)
}

// seedVectorBenchmark builds a database of chunks unit vectors and returns it with a
// query vector of the same dimension.
//
// Rows are inserted directly instead of through UpsertDocumentWithChunks: tokenising
// 100k chunks into chunks_fts dominated seeding by an order of magnitude, and the
// vector channel never reads the full-text table. Everything SearchVector touches —
// documents, chunks, embeddings — is written exactly as the writer writes it.
func seedVectorBenchmark(b *testing.B, chunks, dim int) (*sql.DB, []float32) {
	b.Helper()

	ctx := context.Background()
	database, err := Open(filepath.Join(b.TempDir(), "vector-bench.db"))
	if err != nil {
		b.Fatalf("open db: %v", err)
	}
	b.Cleanup(func() { _ = database.Close() })

	if err := Migrate(ctx, database); err != nil {
		b.Fatalf("migrate: %v", err)
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		b.Fatalf("begin seed tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The same statements UpsertDocumentWithChunks and UpsertEmbeddings run, minus the
	// full-text table.
	insertDocument := `INSERT INTO documents(id, ` + documentColumns + `, content_hash, metadata_hash, updated_at) VALUES(` + questionMarks(24) + `)`
	const insertChunk = `INSERT INTO chunks(id, document_id, chunk_index, text, chunk_hash, updated_at) VALUES(?, ?, ?, ?, ?, ?)`
	const insertEmbedding = `INSERT INTO embeddings(chunk_id, name, dimension, vector, updated_at) VALUES(?, ?, ?, ?, ?)`

	rng := rand.New(rand.NewPCG(0x5eed, 0xbeef))
	text := strings.Repeat("vector scan benchmark paragraph ", benchChunkTextSize/31)
	name := benchEmbeddingName(dim)

	for doc := 0; doc < chunks/benchChunksPerDocument; doc++ {
		documentID := fmt.Sprintf("bench.test/%d", doc)
		record := DocumentRecord{
			ID:             documentID,
			PageID:         documentID,
			Title:          fmt.Sprintf("Benchmark document %d", doc),
			LocalPath:      fmt.Sprintf("bench-%d.md", doc),
			SpaceKey:       []string{"SFD", "OPS"}[doc%2],
			SourceURL:      "https://bench.test/wiki/spaces/SFD/pages/" + documentID,
			CanonicalURL:   "https://bench.test/wiki/spaces/SFD/pages/" + documentID,
			Host:           "bench.test",
			LastModifiedAt: benchTimestamp,
			CreatedAt:      benchTimestamp,
			CrawledAt:      benchTimestamp,
			ContentHash:    fmt.Sprintf("bench-content-%d", doc),
			MetadataHash:   fmt.Sprintf("bench-metadata-%d", doc),
			Version:        1,
			Depth:          doc % 4,
			CreatedByName:  []string{"Ada Lovelace", "Grace Hopper"}[doc%2],
			ModifiedByName: []string{"Grace Hopper", "Ada Lovelace"}[doc%2],
			IsSeed:         doc%10 == 0,
			LinkIn:         doc % 7,
			LinkOut:        doc % 5,
			Attachments:    doc % 3,
			Comments:       doc % 2,
		}

		args := append([]any{record.ID}, record.documentValues()...)
		args = append(args, record.ContentHash, record.MetadataHash, benchTimestamp)
		if _, err := tx.ExecContext(ctx, insertDocument, args...); err != nil {
			b.Fatalf("insert document: %v", err)
		}

		for index := 0; index < benchChunksPerDocument; index++ {
			chunkID := fmt.Sprintf("%s:%06d", documentID, index)
			if _, err := tx.ExecContext(ctx, insertChunk, chunkID, documentID, index, text, "bench-hash", benchTimestamp); err != nil {
				b.Fatalf("insert chunk %s: %v", chunkID, err)
			}
			vector := encodeFloat32Vector(benchUnitVector(rng, dim))
			if _, err := tx.ExecContext(ctx, insertEmbedding, chunkID, name, dim, vector, benchTimestamp); err != nil {
				b.Fatalf("insert embedding %s: %v", chunkID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		b.Fatalf("commit seed tx: %v", err)
	}

	return database, benchUnitVector(rand.New(rand.NewPCG(0xcafe, 0xf00d)), dim)
}

// benchUnitVector returns a deterministic unit-length vector, so cosine similarity
// stays in [-1, 1] and no row is skipped for a zero norm.
func benchUnitVector(rng *rand.Rand, dim int) []float32 {
	values := make([]float32, dim)
	norm := 0.0

	for i := range values {
		value := rng.Float64()*2 - 1
		values[i] = float32(value)
		norm += value * value
	}

	norm = math.Sqrt(norm)
	for i := range values {
		values[i] = float32(float64(values[i]) / norm)
	}

	return values
}

// Sinks keep the compiler from eliminating the work under measurement.
var (
	benchmarkScoreSink float64
	benchmarkSizeSink  int
)

// BenchmarkSearchVector measures the whole vector channel: SQL, projection, decode,
// cosine, ranking and truncation. SetBytes reports the bytes one query pulls out of
// SQLite, so MB/s is the scan rate.
func BenchmarkSearchVector(b *testing.B) {
	for _, test := range vectorBenchmarkCases() {
		b.Run(test.name, func(b *testing.B) {
			if testing.Short() && test.chunks >= 100_000 {
				b.Skip("large corpus skipped in -short mode")
			}

			database, queryVector := seedVectorBenchmark(b, test.chunks, test.dim)
			filters := SearchFilters{Candidate: benchCandidateK, EmbeddingName: benchEmbeddingName(test.dim)}
			ctx := context.Background()

			b.ReportAllocs()
			b.SetBytes(benchVectorBytes(test.chunks, test.dim))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				hits, err := SearchVector(ctx, database, queryVector, filters)
				if err != nil {
					b.Fatalf("search vector: %v", err)
				}
				if len(hits) != benchCandidateK {
					b.Fatalf("hits = %d, want %d", len(hits), benchCandidateK)
				}
				benchmarkSizeSink = len(hits)
			}
		})
	}
}

// BenchmarkCosine isolates the arithmetic, so the scan share of BenchmarkSearchVector
// can be derived as end-to-end minus decode minus cosine.
func BenchmarkCosine(b *testing.B) {
	for _, dim := range []int{256, 1024} {
		b.Run(fmt.Sprintf("%d", dim), func(b *testing.B) {
			rng := rand.New(rand.NewPCG(0x1234, 0x5678))
			query := benchUnitVector(rng, dim)
			candidate := benchUnitVector(rng, dim)

			b.ReportAllocs()
			b.SetBytes(int64(dim * 4))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchmarkScoreSink = cosine(query, candidate)
			}
		})
	}
}

// BenchmarkDecodeVector isolates BLOB to []float32 conversion.
func BenchmarkDecodeVector(b *testing.B) {
	for _, dim := range []int{256, 1024} {
		b.Run(fmt.Sprintf("%d", dim), func(b *testing.B) {
			blob := encodeFloat32Vector(benchUnitVector(rand.New(rand.NewPCG(0x9abc, 0xdef0)), dim))

			b.ReportAllocs()
			b.SetBytes(int64(len(blob)))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchmarkSizeSink = len(decodeFloat32Vector(blob, dim))
			}
		})
	}
}

// BenchmarkCandidateSorting measures the ranking step on its own: SearchVector copies
// every scanned row into a slice and sorts the whole slice before truncating to
// candidateK, so this is the O(n log n) share of a query.
func BenchmarkCandidateSorting(b *testing.B) {
	const rows = 100_000

	rng := rand.New(rand.NewPCG(0x2468, 0x1357))
	scanned := make([]Candidate, rows)
	for i := range scanned {
		scanned[i] = Candidate{
			ChunkID:        fmt.Sprintf("bench.test/%d:%06d", i/benchChunksPerDocument, i%benchChunksPerDocument),
			VectorScoreRaw: rng.Float64(),
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ranked := make([]Candidate, len(scanned))
		copy(ranked, scanned)
		sort.SliceStable(ranked, func(i, j int) bool {
			if ranked[i].VectorScoreRaw == ranked[j].VectorScoreRaw {
				return ranked[i].ChunkID < ranked[j].ChunkID
			}
			return ranked[i].VectorScoreRaw > ranked[j].VectorScoreRaw
		})
		benchmarkSizeSink = len(ranked)
	}
}
