package embedding

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = rt.target.Scheme
	clone.URL.Host = rt.target.Host
	if rt.base == nil {
		return http.DefaultTransport.RoundTrip(clone)
	}
	return rt.base.RoundTrip(clone)
}

func TestNewDefaultFromEnvHashFallback(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	res := NewDefaultFromEnv()
	if res.Source != "hash-fallback" {
		t.Fatalf("expected hash-fallback source got %s", res.Source)
	}
	if res.Provider == nil || res.Provider.Name() != "hash-local" {
		t.Fatalf("expected hash-local provider")
	}
}

func TestNewDefaultFromEnvOpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "secret")
	t.Setenv("OPENAI_EMBED_MODEL", "")
	res := NewDefaultFromEnv()
	if res.Source != "openai" {
		t.Fatalf("expected openai source got %s", res.Source)
	}
	if !strings.HasPrefix(res.Provider.Name(), "openai:") {
		t.Fatalf("expected openai provider name got %s", res.Provider.Name())
	}
}

func TestHashProviderEmbedAndHash(t *testing.T) {
	p := NewHashProvider(8)
	vectors, err := p.Embed(context.Background(), []string{"alpha", "alpha", "beta"})
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	if len(vectors) != 3 {
		t.Fatalf("expected 3 vectors got %d", len(vectors))
	}
	if len(vectors[0]) != 8 {
		t.Fatalf("expected vector dimension 8 got %d", len(vectors[0]))
	}
	if HashOfVector(vectors[0]) != HashOfVector(vectors[1]) {
		t.Fatalf("expected identical text to produce identical vector hash")
	}
	if HashOfVector(vectors[0]) == HashOfVector(vectors[2]) {
		t.Fatalf("expected different text to produce different vector hash")
	}
}

func TestOpenAIProviderEmbedSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing auth header")
		}
		_, _ = io.WriteString(w, `{"data":[{"embedding":[1,0],"index":0},{"embedding":[0,1],"index":1}]}`)
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	p := NewOpenAIProvider("token", "model-x")
	p.http.Transport = rewriteTransport{target: u}

	out, err := p.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 vectors got %d", len(out))
	}
	if len(out[0]) != 2 || len(out[1]) != 2 {
		t.Fatalf("expected 2-d vectors")
	}
}

func TestOpenAIProviderEmbedErrorResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"bad request"}}`)
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	p := NewOpenAIProvider("token", "model-x")
	p.http.Transport = rewriteTransport{target: u}

	_, err = p.Embed(context.Background(), []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("expected openai error message, got: %v", err)
	}
}

func TestOpenAIProviderEmbedMissingVector(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"embedding":[1,0],"index":0}]}`)
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	p := NewOpenAIProvider("token", "model-x")
	p.http.Transport = rewriteTransport{target: u}

	_, err = p.Embed(context.Background(), []string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "missing vector") {
		t.Fatalf("expected missing vector error, got: %v", err)
	}
}

func TestNormalizeZeroVectorNoPanic(t *testing.T) {
	v := []float32{0, 0, 0}
	normalize(v)
	if v[0] != 0 || v[1] != 0 || v[2] != 0 {
		t.Fatalf("expected zero vector unchanged")
	}
}

func TestOpenAIProviderEmptyInput(t *testing.T) {
	p := NewOpenAIProvider("token", "model-x")
	out, err := p.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected no error got %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil output for empty input")
	}
}

func TestMain(m *testing.M) {
	// Ensure tests run with no ambient OpenAI env effects.
	_ = os.Unsetenv("OPENAI_API_KEY")
	_ = os.Unsetenv("OPENAI_EMBED_MODEL")
	os.Exit(m.Run())
}
