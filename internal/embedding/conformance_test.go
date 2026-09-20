package embedding_test

import (
	"encoding/json"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/embedding"
	"github.com/gkoos/confluence2md-indexer/internal/embedding/embeddingtest"
)

// The conformance suite is the contract every provider must satisfy. New
// adapters (Cohere, Voyage, Gemini, OpenAI-compatible servers) get wired in here
// so they cannot regress batching, determinism or dimension handling.

func TestBowProviderConformance(t *testing.T) {
	embeddingtest.Verify(t, embedding.NewBowProvider(256), []string{
		"alpha beta gamma",
		"delta epsilon",
		"zeta eta theta",
	})
}

func TestStubProviderConformance(t *testing.T) {
	embeddingtest.Verify(t, embeddingtest.New(32), []string{"one", "two", "three"})
}

// conformanceServer serves deterministic, text dependent embeddings so the
// suite can exercise HTTP adapters without a network dependency.
func conformanceServer(t *testing.T, dim int) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		data := make([]map[string]any, 0, len(request.Input))
		for index, text := range request.Input {
			data = append(data, map[string]any{"embedding": conformanceVector(text, dim), "index": index})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)

	return server
}

func TestOpenAIProviderConformance(t *testing.T) {
	server := conformanceServer(t, 8)
	t.Setenv("OPENAI_API_KEY", "token")

	resolution, err := embedding.Resolve(embedding.Options{
		Provider:  embedding.ProviderOpenAI,
		BaseURL:   server.URL,
		Dimension: 8,
		APIKeyEnv: "OPENAI_API_KEY",
	})
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}

	embeddingtest.Verify(t, resolution.Provider, []string{"alpha beta", "gamma delta", "epsilon zeta"})
}

func TestOpenAICompatibleProviderConformance(t *testing.T) {
	server := conformanceServer(t, 8)

	resolution, err := embedding.Resolve(embedding.Options{
		Provider:  embedding.ProviderOpenAICompatible,
		BaseURL:   server.URL,
		Model:     "conformance-model",
		Dimension: 8,
	})
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}

	embeddingtest.Verify(t, resolution.Provider, []string{"alpha beta", "gamma delta", "epsilon zeta"})
}

// conformanceVector produces text dependent vectors without any network access.
func conformanceVector(text string, dim int) []float32 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(text))
	state := hasher.Sum64() | 1

	vec := make([]float32, dim)
	for i := range vec {
		state = state*6364136223846793005 + 1442695040888963407
		vec[i] = float32(int64(state%1000)+1) / 1000
	}
	return vec
}
