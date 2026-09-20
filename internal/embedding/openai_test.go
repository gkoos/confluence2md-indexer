package embedding

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recorder collects values observed by the stub server. Handlers run on the
// server goroutine, so every access is mutex guarded to stay race free under
// `go test -race` in CI.
type recorder struct {
	mu      sync.Mutex
	strings map[string]string
	lists   map[string][]string
}

func newRecorder() *recorder {
	return &recorder{strings: map[string]string{}, lists: map[string][]string{}}
}

func (r *recorder) setString(key, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.strings[key] = value
}

func (r *recorder) string(key string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.strings[key]
}

func (r *recorder) setList(key string, values []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lists[key] = append([]string(nil), values...)
}

func (r *recorder) list(key string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lists[key]...)
}

// recordDimensions stores the optional dimensions field as text: "none" when the
// provider omitted it.
func (r *recorder) recordDimensions(dimensions *int) {
	if dimensions == nil {
		r.setString("dimensions", "none")
		return
	}
	r.setString("dimensions", strconv.Itoa(*dimensions))
}

// wireRequest mirrors the request body the provider sends.
type wireRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format"`
	Dimensions     *int     `json:"dimensions"`
}

// fakeVector produces deterministic, text dependent vectors for stub servers.
func fakeVector(text string, dim int) []float32 {
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

// newWireTestServer starts an embeddings endpoint at the default path. handler
// may write its own response and return true to take over handling from the
// default success path.
func newWireTestServer(t *testing.T, dim int, handler func(w http.ResponseWriter, r *http.Request, request wireRequest) bool) *httptest.Server {
	t.Helper()
	return newWireTestServerAt(t, openAIEmbedPath, dim, handler)
}

// newWireTestServerAt starts an embeddings endpoint at wantPath, which lets
// deployment-scoped URLs such as Azure OpenAI's be exercised. An empty wantPath
// accepts any path.
func newWireTestServerAt(t *testing.T, wantPath string, dim int, handler func(w http.ResponseWriter, r *http.Request, request wireRequest) bool) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantPath != "" && r.URL.Path != wantPath {
			t.Errorf("unexpected request path %q, want %q", r.URL.Path, wantPath)
		}

		var request wireRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if handler != nil && handler(w, r, request) {
			return
		}

		data := make([]map[string]any, 0, len(request.Input))
		for index, text := range request.Input {
			data = append(data, map[string]any{"embedding": fakeVector(text, dim), "index": index})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)

	return server
}

// newWireTestProvider builds the OpenAI flavour against a stub server.
func newWireTestProvider(t *testing.T, baseURL string, mutate func(*Options)) *openAIProvider {
	t.Helper()
	t.Setenv("OPENAI_API_KEY", "token")

	options := Options{Provider: ProviderOpenAI, BaseURL: baseURL, Dimension: 4}
	if mutate != nil {
		mutate(&options)
	}

	provider, err := newOpenAIProvider(options)
	if err != nil {
		t.Fatalf("build provider: %v", err)
	}

	wire, ok := provider.(*openAIProvider)
	if !ok {
		t.Fatalf("expected *openAIProvider, got %T", provider)
	}
	return wire
}

func TestWireProviderSendsAuthAndEncodingFormat(t *testing.T) {
	rec := newRecorder()

	server := newWireTestServer(t, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		rec.setString("auth", r.Header.Get("Authorization"))
		rec.setString("contentType", r.Header.Get("Content-Type"))
		rec.setString("encoding", request.EncodingFormat)
		rec.setString("model", request.Model)
		rec.recordDimensions(request.Dimensions)
		return false
	})
	provider := newWireTestProvider(t, server.URL, nil)

	vectors, err := provider.Embed(context.Background(), KindDocument, []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	if len(vectors) != 2 || len(vectors[0]) != 4 {
		t.Fatalf("unexpected vector shape: %d vectors", len(vectors))
	}
	if got := rec.string("auth"); got != "Bearer token" {
		t.Fatalf("unexpected authorization header %q", got)
	}
	if got := rec.string("contentType"); got != "application/json" {
		t.Fatalf("unexpected content type %q", got)
	}
	if got := rec.string("encoding"); got != "float" {
		t.Fatalf("expected float encoding, got %q", got)
	}
	if got := rec.string("model"); got != openAIDefaultModel {
		t.Fatalf("unexpected model %q", got)
	}
	if got := rec.string("dimensions"); got != "4" {
		t.Fatalf("expected the dimensions override to be sent, got %q", got)
	}

	var squaredNorm float64
	for _, value := range vectors[0] {
		squaredNorm += float64(value * value)
	}
	if squaredNorm < 0.999 || squaredNorm > 1.001 {
		t.Fatalf("expected normalised vectors, got squared norm %f", squaredNorm)
	}
}

func TestWireProviderOmitsDimensionsForModelDefault(t *testing.T) {
	rec := newRecorder()

	server := newWireTestServer(t, 1536, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		rec.recordDimensions(request.Dimensions)
		return false
	})
	provider := newWireTestProvider(t, server.URL, func(options *Options) { options.Dimension = 0 })

	if provider.Dimension() != 1536 {
		t.Fatalf("expected the known model dimension, got %d", provider.Dimension())
	}
	if _, err := provider.Embed(context.Background(), KindDocument, []string{"alpha"}); err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	if got := rec.string("dimensions"); got != "none" {
		t.Fatalf("expected no dimensions field for a default model size, got %q", got)
	}
}

func TestWireProviderHonoursResponseIndex(t *testing.T) {
	server := newWireTestServer(t, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		// Reply in reverse order: the provider must reassemble by index.
		data := make([]map[string]any, 0, len(request.Input))
		for index := len(request.Input) - 1; index >= 0; index-- {
			data = append(data, map[string]any{"embedding": fakeVector(request.Input[index], 4), "index": index})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		return true
	})
	provider := newWireTestProvider(t, server.URL, nil)

	vectors, err := provider.Embed(context.Background(), KindDocument, []string{"first", "second"})
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}

	want := fakeVector("first", 4)
	normalize(want)
	for i := range want {
		if vectors[0][i] != want[i] {
			t.Fatalf("first vector was reassembled incorrectly at position %d", i)
		}
	}
}

func TestWireProviderBatchesInputs(t *testing.T) {
	var requests int32

	server := newWireTestServer(t, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		atomic.AddInt32(&requests, 1)
		if len(request.Input) > 2 {
			t.Errorf("batch exceeded the configured size: %d", len(request.Input))
		}
		return false
	})
	provider := newWireTestProvider(t, server.URL, func(options *Options) { options.BatchSize = 2 })

	vectors, err := provider.Embed(context.Background(), KindDocument, []string{"a", "b", "c", "d", "e"})
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	if len(vectors) != 5 {
		t.Fatalf("expected 5 vectors, got %d", len(vectors))
	}
	if got := atomic.LoadInt32(&requests); got != 3 {
		t.Fatalf("expected 3 batched requests, got %d", got)
	}
}

func TestWireProviderRetriesRateLimitsWithRetryAfter(t *testing.T) {
	var attempts int32
	var slept []time.Duration

	server := newWireTestServer(t, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"slow down"}}`)
			return true
		}
		return false
	})
	provider := newWireTestProvider(t, server.URL, nil)
	provider.batch.sleep = func(context.Context, time.Duration) error {
		slept = append(slept, 2*time.Second)
		return nil
	}

	vectors, err := provider.Embed(context.Background(), KindDocument, []string{"alpha"})
	if err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
	if len(vectors) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vectors))
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
	if len(slept) != 1 {
		t.Fatalf("expected one backoff sleep, got %v", slept)
	}
}

func TestWireProviderDoesNotRetryClientErrors(t *testing.T) {
	var attempts int32

	server := newWireTestServer(t, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"bad model"}}`)
		return true
	})
	provider := newWireTestProvider(t, server.URL, nil)
	provider.batch.sleep = func(context.Context, time.Duration) error {
		t.Error("unexpected retry for a client error")
		return nil
	}

	_, err := provider.Embed(context.Background(), KindDocument, []string{"alpha"})
	if err == nil || !strings.Contains(err.Error(), "bad model") || !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected an actionable client error, got %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected a single attempt, got %d", got)
	}
}

func TestWireProviderGivesUpAfterMaxRetries(t *testing.T) {
	var attempts int32

	server := newWireTestServer(t, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"message":"unavailable"}`)
		return true
	})
	provider := newWireTestProvider(t, server.URL, func(options *Options) { options.MaxRetries = 2 })
	provider.batch.sleep = func(context.Context, time.Duration) error { return nil }

	_, err := provider.Embed(context.Background(), KindDocument, []string{"alpha"})
	if err == nil || !strings.Contains(err.Error(), "after 3 attempt") {
		t.Fatalf("expected an attempt count in the error, got %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestWireProviderRejectsDimensionMismatch(t *testing.T) {
	var attempts int32

	server := newWireTestServer(t, 8, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		atomic.AddInt32(&attempts, 1)
		return false // server returns 8 dims, provider expects 4
	})
	provider := newWireTestProvider(t, server.URL, nil)
	provider.batch.sleep = func(context.Context, time.Duration) error {
		t.Error("unexpected retry for a malformed response")
		return nil
	}

	_, err := provider.Embed(context.Background(), KindDocument, []string{"alpha"})
	if err == nil || !strings.Contains(err.Error(), "does not match the configured dimension") {
		t.Fatalf("expected a dimension mismatch error, got %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected a single attempt for a malformed response, got %d", got)
	}
}

func TestWireProviderRejectsMissingVectors(t *testing.T) {
	server := newWireTestServer(t, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": fakeVector("only", 4), "index": 0}},
		})
		return true
	})
	provider := newWireTestProvider(t, server.URL, nil)

	_, err := provider.Embed(context.Background(), KindDocument, []string{"alpha", "beta"})
	if err == nil || !strings.Contains(err.Error(), "contained 1 vectors for 2 inputs") {
		t.Fatalf("expected a vector count error, got %v", err)
	}
}

func TestWireProviderRejectsEmptyVector(t *testing.T) {
	server := newWireTestServer(t, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float32{}, "index": 0}},
		})
		return true
	})
	provider := newWireTestProvider(t, server.URL, nil)

	_, err := provider.Embed(context.Background(), KindDocument, []string{"alpha"})
	if err == nil || !strings.Contains(err.Error(), "empty vector") {
		t.Fatalf("expected an empty vector error, got %v", err)
	}
}

func TestWireProviderAppliesKindPrefixes(t *testing.T) {
	rec := newRecorder()

	server := newWireTestServer(t, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		rec.setList("input", request.Input)
		return false
	})
	provider := newWireTestProvider(t, server.URL, func(options *Options) {
		options.DocPrefix = "passage: "
		options.QueryPrefix = "query: "
	})

	if !provider.Caps().Asymmetric {
		t.Fatal("expected configured prefixes to mark the provider asymmetric")
	}
	if !strings.Contains(provider.Name(), "+prefixes:") {
		t.Fatalf("expected a prefix marker in the identity, got %s", provider.Name())
	}

	if _, err := provider.Embed(context.Background(), KindDocument, []string{"alpha"}); err != nil {
		t.Fatalf("document embed failed: %v", err)
	}
	if got := rec.list("input"); len(got) != 1 || got[0] != "passage: alpha" {
		t.Fatalf("unexpected document input %v", got)
	}

	if _, err := provider.Embed(context.Background(), KindQuery, []string{"alpha"}); err != nil {
		t.Fatalf("query embed failed: %v", err)
	}
	if got := rec.list("input"); len(got) != 1 || got[0] != "query: alpha" {
		t.Fatalf("unexpected query input %v", got)
	}

	// A document-only prefix must leave query text untouched.
	provider.queryPrefix = ""
	if _, err := provider.Embed(context.Background(), KindQuery, []string{"alpha"}); err != nil {
		t.Fatalf("query embed failed: %v", err)
	}
	if got := rec.list("input"); len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("expected unprefixed query input, got %v", got)
	}
}

func TestWireProviderSendsCustomHeadersAndQueryParams(t *testing.T) {
	rec := newRecorder()

	server := newWireTestServer(t, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		rec.setString("header", r.Header.Get("X-Tenant"))
		rec.setString("param", r.URL.Query().Get("api-version"))
		return false
	})
	provider := newWireTestProvider(t, server.URL, func(options *Options) {
		options.Headers = []string{"X-Tenant=acme"}
		options.QueryParams = []string{"api-version=2024-02-01"}
	})

	if _, err := provider.Embed(context.Background(), KindDocument, []string{"alpha"}); err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	if got := rec.string("header"); got != "acme" {
		t.Fatalf("unexpected custom header %q", got)
	}
	if got := rec.string("param"); got != "2024-02-01" {
		t.Fatalf("unexpected query parameter %q", got)
	}
	if !strings.Contains(provider.Name(), "+endpoint:") {
		t.Fatalf("expected a custom endpoint marker, got %s", provider.Name())
	}
}

func TestWireProviderEmptyInputMakesNoRequest(t *testing.T) {
	server := newWireTestServer(t, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		t.Error("expected no request for empty input")
		return true
	})
	provider := newWireTestProvider(t, server.URL, nil)

	vectors, err := provider.Embed(context.Background(), KindDocument, nil)
	if err != nil || vectors != nil {
		t.Fatalf("expected nil result and no error, got %v / %v", vectors, err)
	}
}

func TestWireProviderRejectsInvalidConfiguration(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "token")

	cases := map[string]struct {
		options Options
		want    string
	}{
		"unknown model without dimension": {
			options: Options{Model: "mystery-model"},
			want:    "pass --embedding-dim",
		},
		"custom dimension unsupported by model": {
			options: Options{Model: "text-embedding-ada-002", Dimension: 512},
			want:    "does not support a custom dimension",
		},
		"malformed header": {
			options: Options{Headers: []string{"nope"}},
			want:    "must be in key=value form",
		},
		"malformed query param": {
			options: Options{QueryParams: []string{"=value"}},
			want:    "must be in key=value form",
		},
		"missing api key variable": {
			options: Options{APIKeyEnv: "DEFINITELY_NOT_SET_VAR"},
			want:    "DEFINITELY_NOT_SET_VAR",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			options := testCase.options
			options.Provider = ProviderOpenAI
			options.BaseURL = "https://wire.example.test/v1"
			if options.APIKeyEnv == "" {
				options.APIKeyEnv = "OPENAI_API_KEY"
			}

			_, err := newOpenAIProvider(options)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("expected error containing %q, got %v", testCase.want, err)
			}
		})
	}
}

func TestWireProviderCapsAndIdentity(t *testing.T) {
	server := newWireTestServer(t, 4, nil)
	provider := newWireTestProvider(t, server.URL, nil)

	caps := provider.Caps()
	if !caps.Semantic || caps.Lexical {
		t.Fatalf("unexpected capabilities %+v", caps)
	}
	if caps.Capability() != CapabilitySemantic {
		t.Fatalf("expected semantic capability, got %q", caps.Capability())
	}
	if caps.MaxBatch != openAIMaxBatch || caps.MaxInputTokens != openAIMaxInputTokens {
		t.Fatalf("unexpected request limits %+v", caps)
	}
	if provider.Dimension() != 4 {
		t.Fatalf("expected dimension 4, got %d", provider.Dimension())
	}

	want := "openai:" + openAIDefaultModel + "@4+endpoint:" + shortTag(server.URL, openAIEmbedPath)
	if provider.Name() != want {
		t.Fatalf("unexpected identity %q (want %q)", provider.Name(), want)
	}
}
