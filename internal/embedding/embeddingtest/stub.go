// Package embeddingtest provides deterministic providers and a conformance
// suite for implementers of the embedding.Provider interface.
package embeddingtest

import (
	"context"
	"hash/fnv"
	"math"

	"github.com/gkoos/confluence2md-indexer/internal/embedding"
)

// Provider is a deterministic stand-in for a semantic embedding provider.
//
// The same text always produces the same unit-length vector, so tests can seed
// an index and then query it without any network access. Vectors are
// pseudo-random with respect to text: they deliberately do not model term
// overlap, which keeps lexical and vector behaviour separable in fixtures.
type Provider struct {
	name string
	dim  int
	caps embedding.Caps
}

// New returns a deterministic semantic provider with the given dimension.
func New(dim int) *Provider {
	return &Provider{name: "stub:test", dim: dim, caps: embedding.Caps{Semantic: true}}
}

// NewWithCaps returns a provider with explicit capabilities and identity, which
// is useful for exercising capability-dependent behaviour such as asymmetric
// models or lexical-only providers.
func NewWithCaps(name string, dim int, caps embedding.Caps) *Provider {
	return &Provider{name: name, dim: dim, caps: caps}
}

// Name returns the configured identity.
func (p *Provider) Name() string { return p.name }

// Dimension returns the configured vector size.
func (p *Provider) Dimension() int { return p.dim }

// Caps returns the configured capabilities.
func (p *Provider) Caps() embedding.Caps { return p.caps }

// Embed returns deterministic vectors for texts.
func (p *Provider) Embed(_ context.Context, kind embedding.Kind, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		out = append(out, p.vector(text, kind))
	}
	return out, nil
}

func (p *Provider) vector(text string, kind embedding.Kind) []float32 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(text))
	if p.caps.Asymmetric {
		// Only asymmetric providers may differ between document and query text,
		// so symmetric fixtures stay stable across call sites.
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(kind.String()))
	}
	state := hasher.Sum64() | 1

	vec := make([]float32, p.dim)
	for i := range vec {
		state = nextRandom(state)
		vec[i] = float32(int64(state%2001)-1000) / 1000
	}
	normalize(vec)
	return vec
}

// nextRandom advances an xorshift64 generator.
func nextRandom(state uint64) uint64 {
	state ^= state << 13
	state ^= state >> 7
	state ^= state << 17
	return state
}

func normalize(vec []float32) {
	var sum float64
	for _, value := range vec {
		sum += float64(value * value)
	}
	if sum == 0 {
		return
	}

	inverse := float32(1 / math.Sqrt(sum))
	for i := range vec {
		vec[i] *= inverse
	}
}
