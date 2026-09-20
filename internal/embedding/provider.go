package embedding

import (
	"context"
	"math"
)

// Provider generates embedding vectors for text.
//
// Name is the canonical identity of the vector space produced by the provider.
// It doubles as the fingerprint stored with every vector, so it must change
// whenever the vector space changes (model, dimension, endpoint, text prefixes)
// and must stay stable for operational settings such as batch size, timeout or
// retry count.
type Provider interface {
	// Name returns the canonical identity, for example
	// "openai:text-embedding-3-small@1536".
	Name() string
	// Dimension returns the vector size produced by Embed.
	Dimension() int
	// Caps describes retrieval capability and request limits.
	Caps() Caps
	// Embed converts texts in input order. kind lets asymmetric models apply the
	// correct transformation for indexing versus querying.
	Embed(ctx context.Context, kind Kind, texts []string) ([][]float32, error)
}

// normalize scales vec to unit length in place, leaving zero vectors untouched.
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
