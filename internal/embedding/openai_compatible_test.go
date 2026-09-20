package embedding

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// newCompatibleTestProvider builds the openai-compatible flavour against a stub
// server, defaulting to a keyless local-server style configuration.
func newCompatibleTestProvider(t *testing.T, baseURL string, mutate func(*Options)) *openAIProvider {
	t.Helper()

	options := Options{
		Provider:  ProviderOpenAICompatible,
		BaseURL:   baseURL,
		Model:     "test-embed",
		Dimension: 4,
	}
	if mutate != nil {
		mutate(&options)
	}

	provider, err := newOpenAICompatibleProvider(options)
	if err != nil {
		t.Fatalf("build provider: %v", err)
	}

	wire, ok := provider.(*openAIProvider)
	if !ok {
		t.Fatalf("expected *openAIProvider, got %T", provider)
	}
	return wire
}

func TestOpenAICompatibleRequiresConfiguration(t *testing.T) {
	cases := map[string]struct {
		options Options
		want    string
	}{
		"missing base url": {
			options: Options{Model: "some-model", Dimension: 4},
			want:    "requires a base URL",
		},
		"missing model": {
			options: Options{BaseURL: "http://127.0.0.1:11434/v1", Dimension: 4},
			want:    "requires a model name",
		},
		"missing dimension": {
			options: Options{BaseURL: "http://127.0.0.1:11434/v1", Model: "some-model"},
			want:    "requires --embedding-dim",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			options := testCase.options
			options.Provider = ProviderOpenAICompatible

			_, err := newOpenAICompatibleProvider(options)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("expected error containing %q, got %v", testCase.want, err)
			}
		})
	}
}

func TestOpenAICompatibleServesKeylessServer(t *testing.T) {
	rec := newRecorder()

	server := newWireTestServer(t, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		rec.setString("auth", r.Header.Get("Authorization"))
		rec.setString("apiKey", r.Header.Get("api-key"))
		return false
	})
	provider := newCompatibleTestProvider(t, server.URL, nil)

	vectors, err := provider.Embed(context.Background(), KindDocument, []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vectors))
	}
	if got := rec.string("auth"); got != "" {
		t.Fatalf("expected no authorization header for a keyless server, got %q", got)
	}
	if got := rec.string("apiKey"); got != "" {
		t.Fatalf("expected no api-key header for a keyless server, got %q", got)
	}
}

func TestOpenAICompatibleSupportsAzureStyleDeployment(t *testing.T) {
	rec := newRecorder()
	t.Setenv("AZURE_TEST_KEY", "secret")

	const deploymentPath = "/openai/deployments/text-embedding/embeddings"
	server := newWireTestServerAt(t, deploymentPath, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		rec.setString("apiKey", r.Header.Get("api-key"))
		rec.setString("auth", r.Header.Get("Authorization"))
		rec.setString("version", r.URL.Query().Get("api-version"))
		return false
	})

	provider := newCompatibleTestProvider(t, server.URL+"/openai/deployments/text-embedding", func(options *Options) {
		// Azure sends a raw key in api-key and requires an api-version parameter.
		options.AuthHeader = "api-key"
		options.AuthScheme = "none"
		options.APIKeyEnv = "AZURE_TEST_KEY"
		options.QueryParams = []string{"api-version=2024-02-01"}
	})

	if _, err := provider.Embed(context.Background(), KindQuery, []string{"alpha"}); err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	if got := rec.string("apiKey"); got != "secret" {
		t.Fatalf("expected the raw key in api-key, got %q", got)
	}
	if got := rec.string("auth"); got != "" {
		t.Fatalf("expected no bearer authorization header, got %q", got)
	}
	if got := rec.string("version"); got != "2024-02-01" {
		t.Fatalf("expected the api-version query parameter, got %q", got)
	}
}

func TestOpenAICompatibleIdentityAndCaps(t *testing.T) {
	server := newWireTestServer(t, 4, nil)
	provider := newCompatibleTestProvider(t, server.URL, nil)

	want := "openai-compatible:test-embed@4+endpoint:" + shortTag(server.URL, openAIEmbedPath)
	if provider.Name() != want {
		t.Fatalf("unexpected identity %q (want %q)", provider.Name(), want)
	}

	caps := provider.Caps()
	if !caps.Semantic || caps.Lexical {
		t.Fatalf("unexpected capabilities %+v", caps)
	}
	if caps.Capability() != CapabilitySemantic {
		t.Fatalf("expected semantic capability, got %q", caps.Capability())
	}
	if caps.MaxBatch != 0 || caps.MaxInputTokens != 0 {
		t.Fatalf("expected unbounded request limits for a self-hosted endpoint, got %+v", caps)
	}
	if provider.Dimension() != 4 {
		t.Fatalf("expected dimension 4, got %d", provider.Dimension())
	}
}

func TestOpenAICompatibleDeclaresDimensionWithoutRequestingIt(t *testing.T) {
	rec := newRecorder()

	server := newWireTestServer(t, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		rec.recordDimensions(request.Dimensions)
		return false
	})
	provider := newCompatibleTestProvider(t, server.URL, nil)

	if _, err := provider.Embed(context.Background(), KindDocument, []string{"alpha"}); err != nil {
		t.Fatalf("embed failed: %v", err)
	}

	// A configured dimension declares what to expect from responses; it must not
	// be sent as a truncation request to an endpoint that may not support it.
	if got := rec.string("dimensions"); got != "none" {
		t.Fatalf("expected no dimensions field for a compatible endpoint, got %q", got)
	}
	if provider.Dimension() != 4 {
		t.Fatalf("expected the declared dimension to be kept, got %d", provider.Dimension())
	}
}

func TestOpenAICompatibleIsRegistered(t *testing.T) {
	if !containsString(Available(), ProviderOpenAICompatible) {
		t.Fatalf("expected %q in %v", ProviderOpenAICompatible, Available())
	}

	server := newWireTestServer(t, 4, nil)
	resolution, err := Resolve(Options{
		Provider:  ProviderOpenAICompatible,
		BaseURL:   server.URL,
		Model:     "bge-m3",
		Dimension: 4,
	})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolution.Source != SourceFlag {
		t.Fatalf("expected flag source, got %q", resolution.Source)
	}
	if !strings.HasPrefix(resolution.Provider.Name(), "openai-compatible:bge-m3@4+endpoint:") {
		t.Fatalf("unexpected identity %q", resolution.Provider.Name())
	}
}
