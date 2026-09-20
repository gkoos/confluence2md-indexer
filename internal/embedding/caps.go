package embedding

// Retrieval capability names reported in command output and explain diagnostics.
const (
	CapabilityNone     = "none"
	CapabilityLexical  = "lexical"
	CapabilitySemantic = "semantic"
)

// Caps describes what a provider can do. It drives fusion and batching decisions.
type Caps struct {
	// Semantic is true for providers backed by trained embedding models, whose
	// similarity reflects meaning rather than surface form.
	Semantic bool
	// Lexical is true for providers that only measure term overlap, such as the
	// local bag-of-words fallback. Those vectors are useful for offline vector
	// search but are correlated with the BM25 channel.
	Lexical bool
	// Asymmetric is true when queries and documents must be transformed
	// differently before embedding.
	Asymmetric bool
	// MaxBatch is the maximum number of inputs accepted per request. Zero means
	// unbounded (local providers).
	MaxBatch int
	// MaxInputTokens is the maximum token count accepted per input. Zero means
	// unknown or unbounded.
	MaxInputTokens int
}

// Capability returns the strongest retrieval capability the provider offers.
func (c Caps) Capability() string {
	switch {
	case c.Semantic:
		return CapabilitySemantic
	case c.Lexical:
		return CapabilityLexical
	default:
		return CapabilityNone
	}
}

// Enabled reports whether the provider produces a usable vector channel.
func (c Caps) Enabled() bool {
	return c.Capability() != CapabilityNone
}
