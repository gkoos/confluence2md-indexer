package embedding

import (
	"context"
	"math"
	"testing"
)

func TestBowProviderIdentityAndCaps(t *testing.T) {
	provider := NewBowProvider(0) // zero falls back to the default dimension
	if provider.Dimension() != DefaultBowDim {
		t.Fatalf("expected default dimension %d got %d", DefaultBowDim, provider.Dimension())
	}
	if provider.Name() != "bow-local:fnv1a@256" {
		t.Fatalf("unexpected identity %q", provider.Name())
	}

	caps := provider.Caps()
	if !caps.Lexical || caps.Semantic {
		t.Fatalf("expected a lexical-only provider, got %+v", caps)
	}
	if caps.Capability() != CapabilityLexical {
		t.Fatalf("expected lexical capability got %q", caps.Capability())
	}
	if !caps.Enabled() {
		t.Fatal("expected a usable vector channel")
	}
	if caps.Asymmetric {
		t.Fatal("term overlap is symmetric; provider must not claim asymmetry")
	}

	custom := NewBowProvider(64)
	if custom.Dimension() != 64 || custom.Name() != "bow-local:fnv1a@64" {
		t.Fatalf("unexpected custom provider identity %q dim %d", custom.Name(), custom.Dimension())
	}
}

func TestBowVectorsAreDeterministic(t *testing.T) {
	provider := NewBowProvider(128)
	text := "rotate database credentials with the vault cli"

	first, err := provider.Embed(context.Background(), KindDocument, []string{text})
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}

	// Map iteration order is randomised, so repeated calls catch any
	// accumulation that is not sorted first.
	for iteration := 0; iteration < 50; iteration++ {
		next, err := provider.Embed(context.Background(), KindDocument, []string{text})
		if err != nil {
			t.Fatalf("embed failed: %v", err)
		}
		for i := range first[0] {
			if first[0][i] != next[0][i] {
				t.Fatalf("vector changed at position %d on iteration %d", i, iteration)
			}
		}
	}
}

func TestBowVectorsAreUnitLength(t *testing.T) {
	provider := NewBowProvider(64)
	vectors, err := provider.Embed(context.Background(), KindDocument, []string{"alpha beta gamma"})
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}

	var norm float64
	for _, value := range vectors[0] {
		norm += float64(value * value)
	}
	if math.Abs(math.Sqrt(norm)-1) > 1e-6 {
		t.Fatalf("expected unit length vector, got norm %f", math.Sqrt(norm))
	}
}

func TestBowTokenlessTextProducesZeroVector(t *testing.T) {
	provider := NewBowProvider(32)

	// Stopwords, single characters and punctuation carry no signal.
	vectors, err := provider.Embed(context.Background(), KindDocument, []string{"the and of to", "a b c", "!!! ??? ..."})
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	for index, vec := range vectors {
		for position, value := range vec {
			if value != 0 {
				t.Fatalf("vector %d position %d: expected zero, got %f", index, position, value)
			}
		}
	}
}

func TestBowRanksTermOverlapAboveUnrelatedText(t *testing.T) {
	// This fixture is the one used to evaluate the previous SHA256 provider,
	// which ranked the most relevant chunk below an unrelated page.
	provider := NewBowProvider(256)
	query := "how to rotate database credentials"

	relevant := []string{
		"How to rotate database credentials: use the Vault CLI and follow the 90-day rotation policy.",
		"Database credentials are stored in HashiCorp Vault and rotated quarterly by the platform team.",
	}
	unrelated := []string{
		"Kubernetes cluster upgrade runbook: drain nodes, cordon, upgrade kubelet, uncordon.",
		"Office coffee machine maintenance schedule and descaling instructions.",
		"Q3 marketing campaign budget planning and return-on-investment review.",
		"Employee onboarding checklist for new joiners in the Berlin office.",
	}

	texts := append(append([]string{}, relevant...), unrelated...)
	vectors, err := provider.Embed(context.Background(), KindDocument, append([]string{query}, texts...))
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}

	queryVector := vectors[0]
	index := 1
	bestRelevant := math.Inf(-1)
	worstUnrelated := math.Inf(-1)
	for range relevant {
		score := testCosine(queryVector, vectors[index])
		index++
		if score > bestRelevant {
			bestRelevant = score
		}
	}
	for range unrelated {
		score := testCosine(queryVector, vectors[index])
		index++
		if score > worstUnrelated {
			worstUnrelated = score
		}
	}

	if bestRelevant <= worstUnrelated {
		t.Fatalf("expected relevant text to outrank unrelated text: relevant=%f unrelated=%f", bestRelevant, worstUnrelated)
	}
	if bestRelevant < 0.25 {
		t.Fatalf("expected a clear term-overlap signal for relevant text, got %f", bestRelevant)
	}
	if worstUnrelated > 0.1 {
		t.Fatalf("expected unrelated text to score near zero, got %f", worstUnrelated)
	}
}

func TestSplitTokenBreaksOverlongRuns(t *testing.T) {
	short := splitToken("kubernetes")
	if len(short) != 1 || short[0] != "kubernetes" {
		t.Fatalf("expected short tokens to be preserved, got %v", short)
	}

	long := splitToken("这是一个没有任何空格的中文句子以及更多内容")
	if len(long) < 2 {
		t.Fatalf("expected over-long runs to be split into n-grams, got %d", len(long))
	}
	for _, gram := range long {
		if len([]rune(gram)) != bowNgramSize {
			t.Fatalf("expected %d-rune grams, got %q", bowNgramSize, gram)
		}
	}
}

func TestTokenizeDropsStopwordsAndShortTokens(t *testing.T) {
	tokens := tokenize("The Rotation of a DB Credential")
	joined := map[string]bool{}
	for _, token := range tokens {
		joined[token] = true
	}
	if joined["the"] || joined["of"] || joined["a"] {
		t.Fatalf("expected stopwords to be dropped, got %v", tokens)
	}
	for _, expected := range []string{"rotation", "db", "credential"} {
		if !joined[expected] {
			t.Fatalf("expected %q in %v", expected, tokens)
		}
	}
}

func testCosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
