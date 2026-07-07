package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

type Provider interface {
	Name() string
	Dimension() int
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type FactoryResult struct {
	Provider Provider
	Source   string
}

func NewDefaultFromEnv() FactoryResult {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return FactoryResult{Provider: NewHashProvider(256), Source: "hash-fallback"}
	}
	model := strings.TrimSpace(os.Getenv("OPENAI_EMBED_MODEL"))
	if model == "" {
		model = "text-embedding-3-small"
	}
	return FactoryResult{Provider: NewOpenAIProvider(apiKey, model), Source: "openai"}
}

type HashProvider struct {
	dim int
}

func NewHashProvider(dim int) *HashProvider {
	if dim <= 0 {
		dim = 256
	}
	return &HashProvider{dim: dim}
}

func (p *HashProvider) Name() string   { return "hash-local" }
func (p *HashProvider) Dimension() int { return p.dim }

func (p *HashProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, txt := range texts {
		h := sha256.Sum256([]byte(txt))
		vec := make([]float32, p.dim)
		for i := 0; i < p.dim; i++ {
			b := h[i%len(h)]
			vec[i] = (float32(b)/255.0)*2 - 1
		}
		normalize(vec)
		out = append(out, vec)
	}
	return out, nil
}

type OpenAIProvider struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewOpenAIProvider(apiKey, model string) *OpenAIProvider {
	return &OpenAIProvider{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 45 * time.Second},
	}
}

func (p *OpenAIProvider) Name() string   { return "openai:" + p.model }
func (p *OpenAIProvider) Dimension() int { return 0 }

func (p *OpenAIProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body := map[string]any{"model": p.model, "input": texts}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/embeddings", strings.NewReader(string(b)))
	if err != nil {
		return nil, fmt.Errorf("create openai embeddings request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embeddings request failed: %w", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode openai embeddings response: %w", err)
	}

	if resp.StatusCode >= 300 {
		msg := resp.Status
		if payload.Error != nil && payload.Error.Message != "" {
			msg = payload.Error.Message
		}
		return nil, fmt.Errorf("openai embeddings error: %s", msg)
	}

	out := make([][]float32, len(texts))
	for _, item := range payload.Data {
		if item.Index < 0 || item.Index >= len(texts) {
			continue
		}
		vec := item.Embedding
		normalize(vec)
		out[item.Index] = vec
	}

	for i := range out {
		if out[i] == nil {
			return nil, fmt.Errorf("openai embeddings response missing vector at index %d", i)
		}
	}

	return out, nil
}

func normalize(vec []float32) {
	var n float64
	for _, v := range vec {
		n += float64(v * v)
	}
	if n == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(n))
	for i := range vec {
		vec[i] *= inv
	}
}

func HashOfVector(vector []float32) string {
	b := make([]byte, len(vector)*4)
	for i, v := range vector {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:])
}
