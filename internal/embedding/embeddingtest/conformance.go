package embeddingtest

import (
	"context"
	"math"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/embedding"
)

// IsolateEnv removes embedding configuration from the environment so tests do
// not inherit provider settings from a developer shell or a CI runner.
func IsolateEnv() {
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, embedding.EnvPrefix) {
			_ = os.Unsetenv(name)
		}
	}
	_ = os.Unsetenv("OPENAI_API_KEY")
	_ = os.Unsetenv("OPENAI_EMBED_MODEL")
}

// Verify runs the conformance suite that every embedding.Provider
// implementation must pass. samples are distinct texts the provider can embed.
func Verify(t *testing.T, provider embedding.Provider, samples []string) {
	t.Helper()

	if provider == nil {
		t.Fatal("conformance: provider is nil")
	}
	if strings.TrimSpace(provider.Name()) == "" {
		t.Fatal("conformance: Name must not be empty")
	}
	if !provider.Caps().Enabled() {
		t.Fatal("conformance: provider reports no usable vector channel")
	}

	dim := provider.Dimension()
	if dim <= 0 {
		t.Fatalf("conformance: Dimension must be positive, got %d", dim)
	}
	if len(samples) < 2 {
		t.Fatal("conformance: at least two samples are required")
	}

	ctx := context.Background()

	empty, err := provider.Embed(ctx, embedding.KindDocument, nil)
	if err != nil {
		t.Fatalf("conformance: embedding no texts failed: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("conformance: expected no vectors for no inputs, got %d", len(empty))
	}

	documents, err := provider.Embed(ctx, embedding.KindDocument, samples)
	if err != nil {
		t.Fatalf("conformance: document embed failed: %v", err)
	}
	checkVectors(t, "document", documents, len(samples), dim)

	repeat, err := provider.Embed(ctx, embedding.KindDocument, samples)
	if err != nil {
		t.Fatalf("conformance: repeated embed failed: %v", err)
	}
	for i := range documents {
		if !sameVector(documents[i], repeat[i]) {
			t.Fatalf("conformance: embed is not deterministic at index %d", i)
		}
	}

	for i := 1; i < len(documents); i++ {
		if sameVector(documents[0], documents[i]) {
			t.Fatalf("conformance: distinct texts %q and %q produced identical vectors", samples[0], samples[i])
		}
	}

	for i, vec := range documents {
		for j, value := range vec {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				t.Fatalf("conformance: vector %d contained a non-finite value at position %d", i, j)
			}
		}
	}

	// Concurrent use must be safe: CI runs the suite with -race.
	const goroutines = 8
	var waitGroup sync.WaitGroup
	results := make([][][]float32, goroutines)
	failures := make([]error, goroutines)
	for worker := 0; worker < goroutines; worker++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			results[index], failures[index] = provider.Embed(ctx, embedding.KindQuery, samples)
		}(worker)
	}
	waitGroup.Wait()

	for worker := 0; worker < goroutines; worker++ {
		if failures[worker] != nil {
			t.Fatalf("conformance: concurrent embed failed: %v", failures[worker])
		}
		if len(results[worker]) != len(samples) {
			t.Fatalf("conformance: concurrent embed returned %d vectors for %d inputs", len(results[worker]), len(samples))
		}
		for i := range results[worker] {
			if !sameVector(results[worker][i], results[0][i]) {
				t.Fatalf("conformance: concurrent embed returned different vectors at index %d", i)
			}
		}
	}

	query, err := provider.Embed(ctx, embedding.KindQuery, samples)
	if err != nil {
		t.Fatalf("conformance: query embed failed: %v", err)
	}
	checkVectors(t, "query", query, len(samples), dim)
}

func checkVectors(t *testing.T, label string, vectors [][]float32, inputs int, dim int) {
	t.Helper()

	if len(vectors) != inputs {
		t.Fatalf("conformance: %s embed returned %d vectors for %d inputs", label, len(vectors), inputs)
	}
	for i, vec := range vectors {
		if len(vec) != dim {
			t.Fatalf("conformance: %s vector %d has dimension %d, declared %d", label, i, len(vec), dim)
		}
	}
}

func sameVector(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
