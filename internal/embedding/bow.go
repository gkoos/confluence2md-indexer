package embedding

import (
	"context"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"unicode"
)

const (
	// bowMinTokenRunes drops tokens shorter than this, which removes most noise
	// from punctuation fragments and single letters.
	bowMinTokenRunes = 2
	// bowMaxTokenRunes is the longest run kept intact. Longer runs (CJK text
	// without spaces, long identifiers, URLs) are split into character n-grams
	// so they still carry usable term signal.
	bowMaxTokenRunes = 16
	bowNgramSize     = 3
)

// BowProvider is a dependency-free, offline fallback embedding provider.
//
// It builds a bag-of-words vector using the hashing trick: each token is hashed
// into one bucket with a signed weight, so vectors from texts that share terms
// are genuinely similar. It needs no network, no model file and no API key, and
// it is fully deterministic.
//
// The vectors are lexical, not semantic: they measure term overlap, so they are
// correlated with the BM25 channel and cannot match synonyms. Caps reports this
// so callers can label results honestly.
type BowProvider struct {
	dim int
}

// NewBowProvider returns a bag-of-words provider with the given dimension.
func NewBowProvider(dim int) *BowProvider {
	if dim <= 0 {
		dim = DefaultBowDim
	}
	return &BowProvider{dim: dim}
}

func newBowFromOptions(opts Options) (Provider, error) {
	return NewBowProvider(opts.Dimension), nil
}

func init() {
	MustRegister(ProviderBowLocal, newBowFromOptions)
}

// Name returns the canonical identity of the vector space, for example
// "bow-local:fnv1a@256".
func (p *BowProvider) Name() string {
	return identityString(ProviderBowLocal, "fnv1a", p.dim)
}

// Dimension returns the configured vector size.
func (p *BowProvider) Dimension() int { return p.dim }

// Caps reports a lexical, symmetric, unbounded provider.
func (p *BowProvider) Caps() Caps { return Caps{Lexical: true} }

// Embed converts texts into unit-length bag-of-words vectors. Query and
// document text are treated identically because term overlap is symmetric.
func (p *BowProvider) Embed(_ context.Context, _ Kind, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		out = append(out, p.vector(text))
	}
	return out, nil
}

func (p *BowProvider) vector(text string) []float32 {
	counts := make(map[string]int)
	for _, token := range tokenize(text) {
		counts[token]++
	}

	// Iterating a Go map yields a random order, so keys must be sorted before
	// accumulating: floating point addition is not associative, and unsorted
	// accumulation would make the vector non-deterministic.
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	vec := make([]float32, p.dim)
	for _, key := range keys {
		weight := float32(1 + math.Log(float64(counts[key]))) // sublinear term frequency
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(key))
		sum := hasher.Sum64()
		bucket := int(sum % uint64(p.dim))
		if sum>>63 == 1 { // signed hashing keeps collisions unbiased
			vec[bucket] += weight
		} else {
			vec[bucket] -= weight
		}
	}
	normalize(vec)
	return vec
}

// tokenize lowercases text and splits it into stopword-filtered tokens.
func tokenize(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	out := make([]string, 0, len(fields))
	for _, field := range fields {
		for _, token := range splitToken(field) {
			if len([]rune(token)) < bowMinTokenRunes {
				continue
			}
			if _, isStopword := bowStopwords[token]; isStopword {
				continue
			}
			out = append(out, token)
		}
	}
	return out
}

// splitToken breaks over-long runs into overlapping n-grams.
func splitToken(token string) []string {
	runes := []rune(token)
	if len(runes) <= bowMaxTokenRunes {
		return []string{token}
	}

	grams := make([]string, 0, len(runes)-bowNgramSize+1)
	for i := 0; i+bowNgramSize <= len(runes); i++ {
		grams = append(grams, string(runes[i:i+bowNgramSize]))
	}
	return grams
}

// bowStopwords suppresses the highest-frequency English function words, which
// would otherwise dominate every vector and drown out discriminative terms.
var bowStopwords = func() map[string]struct{} {
	const words = `a about all also an and any are as at be been but by can could did do does
done for from had has have how i if in into is it its just may me more most must no not of on
or other our out should so some such than that the their them then there these they this to us
was we were what when where which who will with would you your`

	stopwords := make(map[string]struct{})
	for _, word := range strings.Fields(words) {
		stopwords[word] = struct{}{}
	}
	return stopwords
}()
