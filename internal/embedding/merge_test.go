package embedding

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// clearEmbeddingEnvironment unsets every embedding variable, so a developer's
// shell cannot influence these tests.
func clearEmbeddingEnvironment(t *testing.T) {
	t.Helper()

	names := []string{
		"PROVIDER", "MODEL", "BASE_URL", "PATH", "DIM", "API_KEY", "API_KEY_ENV",
		"AUTH_HEADER", "AUTH_SCHEME", "HEADERS", "QUERY_PARAMS", "DOCUMENT_PREFIX",
		"QUERY_PREFIX", "BATCH_SIZE", "TIMEOUT", "MAX_RETRIES", "SKIP",
	}
	for _, name := range names {
		t.Setenv(EnvPrefix+name, "")
	}
}

func TestMergeTakesValuesFromTheHighestLayer(t *testing.T) {
	got := Merge(
		Layer{Name: SourceFlag, Options: Options{Model: "flag-model"}, Present: Present{Model: true}},
		Layer{Name: SourceEnv, Options: Options{Provider: "openai"}, Present: Present{Provider: true}},
		Layer{Name: SourceConfig, Options: Options{Dimension: 1024}, Present: Present{Dimension: true}},
	)

	if got.Model != "flag-model" {
		t.Fatalf("model = %q, want the flag value", got.Model)
	}
	if got.Provider != ProviderOpenAI {
		t.Fatalf("provider = %q, want the environment value", got.Provider)
	}
	if got.Dimension != 1024 {
		t.Fatalf("dimension = %d, want the file value", got.Dimension)
	}
	if got.Source != SourceEnv {
		t.Fatalf("source = %q, want %q", got.Source, SourceEnv)
	}
}

func TestMergeAppliesDefaultsAndReportsTheirSource(t *testing.T) {
	got := Merge(Layer{Name: SourceFlag}, Layer{Name: SourceEnv}, Layer{Name: SourceConfig})

	if got.Provider != DefaultProvider || got.Source != SourceDefault {
		t.Fatalf("provider = %q (source %q), want %q from %q", got.Provider, got.Source, DefaultProvider, SourceDefault)
	}
	if got.BatchSize != DefaultBatchSize {
		t.Fatalf("batch size = %d, want %d", got.BatchSize, DefaultBatchSize)
	}
	if got.Timeout != DefaultTimeout {
		t.Fatalf("timeout = %s, want %s", got.Timeout, DefaultTimeout)
	}
	if got.MaxRetries != DefaultMaxRetries {
		t.Fatalf("max retries = %d, want %d", got.MaxRetries, DefaultMaxRetries)
	}
}

func TestMergeSkipsAnEmptyProviderID(t *testing.T) {
	got := Merge(
		Layer{Name: SourceFlag, Present: Present{Provider: true}},
		Layer{Name: SourceEnv, Options: Options{Provider: "openai"}, Present: Present{Provider: true}},
	)

	if got.Provider != ProviderOpenAI {
		t.Fatalf("provider = %q, want the layer that named one", got.Provider)
	}
	if got.Source != SourceEnv {
		t.Fatalf("source = %q, want %q", got.Source, SourceEnv)
	}
}

func TestMergeKeepsAnExplicitlyEmptyValue(t *testing.T) {
	got := Merge(
		Layer{Name: SourceFlag, Present: Present{DocPrefix: true}},
		Layer{Name: SourceEnv, Options: Options{DocPrefix: "passage: "}, Present: Present{DocPrefix: true}},
	)

	if got.DocPrefix != "" {
		t.Fatalf("doc prefix = %q, want the empty flag to win", got.DocPrefix)
	}
}

func TestReadEnvReportsValuesAndPresence(t *testing.T) {
	clearEmbeddingEnvironment(t)
	t.Setenv(EnvPrefix+"PROVIDER", "openai-compatible")
	t.Setenv(EnvPrefix+"DIM", "1024")
	t.Setenv(EnvPrefix+"TIMEOUT", "5s")
	t.Setenv(EnvPrefix+"DOCUMENT_PREFIX", "passage: ")

	options, present, err := ReadEnv()
	if err != nil {
		t.Fatalf("read environment: %v", err)
	}

	if options.Provider != ProviderOpenAICompatible {
		t.Fatalf("provider = %q, want %q", options.Provider, ProviderOpenAICompatible)
	}
	if options.Dimension != 1024 {
		t.Fatalf("dimension = %d, want 1024", options.Dimension)
	}
	if options.Timeout != 5*time.Second {
		t.Fatalf("timeout = %s, want 5s", options.Timeout)
	}
	if options.DocPrefix != "passage: " {
		t.Fatalf("doc prefix = %q, want its whitespace preserved", options.DocPrefix)
	}

	want := Present{Provider: true, Dimension: true, Timeout: true, DocPrefix: true}
	if present != want {
		t.Fatalf("present = %+v, want %+v", present, want)
	}
}

func TestReadEnvIgnoresEmptyVariables(t *testing.T) {
	clearEmbeddingEnvironment(t)

	options, present, err := ReadEnv()
	if err != nil {
		t.Fatalf("read environment: %v", err)
	}
	if options.Provider != "" || options.Model != "" || options.Dimension != 0 || options.Timeout != 0 {
		t.Fatalf("options = %+v, want an empty environment to supply nothing", options)
	}
	if present != (Present{}) {
		t.Fatalf("present = %+v, want all false", present)
	}
}

func TestReadEnvRejectsMalformedNumbers(t *testing.T) {
	clearEmbeddingEnvironment(t)
	t.Setenv(EnvPrefix+"DIM", "not-a-number")

	if _, _, err := ReadEnv(); err == nil {
		t.Fatal("expected an error for a malformed DIM")
	}
}

func TestOpenAIProviderUsesLiteralAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	server := newWireTestServer(t, 4, func(w http.ResponseWriter, r *http.Request, request wireRequest) bool {
		if got := r.Header.Get("Authorization"); got != "Bearer literal-key" {
			t.Errorf("authorization header = %q, want %q", got, "Bearer literal-key")
		}
		return false
	})

	provider, err := newOpenAIProvider(Options{
		Provider:  ProviderOpenAI,
		BaseURL:   server.URL,
		Dimension: 4,
		APIKey:    "literal-key",
	})
	if err != nil {
		t.Fatalf("a literal key must be accepted: %v", err)
	}

	if _, err := provider.Embed(context.Background(), KindDocument, []string{"alpha"}); err != nil {
		t.Fatalf("embed: %v", err)
	}
}

func TestOpenAIProviderStillRequiresAKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	_, err := newOpenAIProvider(Options{Provider: ProviderOpenAI, BaseURL: "https://api.example.test/v1", Dimension: 4})
	if err == nil {
		t.Fatal("expected an error when neither a literal key nor a variable is configured")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("error = %v, want it to name the expected variable", err)
	}
}
