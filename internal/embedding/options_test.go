package embedding

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestResolveDefaultsToBowLocal(t *testing.T) {
	resolution, err := Resolve(Options{})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolution.Source != SourceDefault {
		t.Fatalf("expected default source, got %q", resolution.Source)
	}
	if resolution.Provider.Name() != "bow-local:fnv1a@256" {
		t.Fatalf("unexpected provider %q", resolution.Provider.Name())
	}
	if resolution.Provider.Caps().Capability() != CapabilityLexical {
		t.Fatalf("expected a lexical capability, got %q", resolution.Provider.Caps().Capability())
	}
}

func TestResolvePrefersFlagOverEnvironment(t *testing.T) {
	t.Setenv(EnvPrefix+"PROVIDER", ProviderBowLocal)
	t.Setenv("TEST_KEY", "token")

	resolution, err := Resolve(Options{Provider: ProviderOpenAI, APIKeyEnv: "TEST_KEY"})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolution.Source != SourceFlag {
		t.Fatalf("expected flag source, got %q", resolution.Source)
	}
	if !strings.HasPrefix(resolution.Provider.Name(), "openai:") {
		t.Fatalf("expected the flag provider to win, got %q", resolution.Provider.Name())
	}
}

func TestResolveUsesEnvironmentProvider(t *testing.T) {
	t.Setenv(EnvPrefix+"PROVIDER", " BOW-LOCAL ")

	resolution, err := Resolve(Options{})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolution.Source != SourceEnv {
		t.Fatalf("expected env source, got %q", resolution.Source)
	}
	if resolution.Provider.Name() != "bow-local:fnv1a@256" {
		t.Fatalf("unexpected provider %q", resolution.Provider.Name())
	}
}

func TestResolveReadsEnvironmentModelAndDimension(t *testing.T) {
	t.Setenv(EnvPrefix+"PROVIDER", ProviderOpenAI)
	t.Setenv(EnvPrefix+"MODEL", "text-embedding-3-large")
	t.Setenv(EnvPrefix+"DIM", "1024")
	t.Setenv("OPENAI_API_KEY", "token")

	resolution, err := Resolve(Options{})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolution.Provider.Dimension() != 1024 {
		t.Fatalf("expected environment dimension 1024, got %d", resolution.Provider.Dimension())
	}
	// The base URL and path are the OpenAI defaults, so no endpoint marker is added.
	wantIdentity := "openai:text-embedding-3-large@1024"
	if resolution.Provider.Name() != wantIdentity {
		t.Fatalf("unexpected provider identity %q (want %q)", resolution.Provider.Name(), wantIdentity)
	}
}

func TestResolveRejectsUnknownProvider(t *testing.T) {
	_, err := Resolve(Options{Provider: "not-a-provider"})
	if err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
	if !strings.Contains(err.Error(), "unknown embedding provider") || !strings.Contains(err.Error(), "available:") {
		t.Fatalf("expected an actionable error, got %v", err)
	}
}

func TestResolveSkipReturnsDisabledProvider(t *testing.T) {
	resolution, err := Resolve(Options{Skip: true})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolution.Provider.Caps().Enabled() {
		t.Fatal("expected capabilities to be disabled")
	}
	if resolution.Provider.Caps().Capability() != CapabilityNone {
		t.Fatalf("expected none capability, got %q", resolution.Provider.Caps().Capability())
	}
	if _, err := resolution.Provider.Embed(context.Background(), KindDocument, []string{"x"}); err == nil {
		t.Fatal("expected embed to fail when embeddings are disabled")
	}
}

func TestResolveReadsSkipFromEnvironment(t *testing.T) {
	t.Setenv(EnvPrefix+"SKIP", "true")

	resolution, err := Resolve(Options{})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolution.Provider.Caps().Enabled() {
		t.Fatal("expected the environment to disable embeddings")
	}
}

func TestResolveRejectsMalformedEnvironment(t *testing.T) {
	cases := map[string]struct {
		suffix string
		value  string
		want   string
	}{
		"dimension is not a number": {suffix: "DIM", value: "wide", want: "must be an integer"},
		"negative dimension":        {suffix: "DIM", value: "-8", want: "must not be negative"},
		"timeout is not a duration": {suffix: "TIMEOUT", value: "soon", want: "must be a duration"},
		"zero timeout":              {suffix: "TIMEOUT", value: "0s", want: "must be positive"},
		"negative batch size":       {suffix: "BATCH_SIZE", value: "-4", want: "must not be negative"},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(EnvPrefix+testCase.suffix, testCase.value)
			_, err := Resolve(Options{})
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("expected error containing %q, got %v", testCase.want, err)
			}
		})
	}
}

func TestWithDefaultsReadsEnvironmentOperationalSettings(t *testing.T) {
	t.Setenv(EnvPrefix+"BATCH_SIZE", "9")
	t.Setenv(EnvPrefix+"MAX_RETRIES", "5")
	t.Setenv(EnvPrefix+"TIMEOUT", "12s")
	t.Setenv(EnvPrefix+"HEADERS", "X-A=1; X-B=2 ;")
	t.Setenv(EnvPrefix+"QUERY_PARAMS", "api-version=2024-02-01")

	resolved, _, err := Options{}.withDefaults()
	if err != nil {
		t.Fatalf("apply defaults: %v", err)
	}

	if resolved.BatchSize != 9 || resolved.MaxRetries != 5 || resolved.Timeout != 12*time.Second {
		t.Fatalf("unexpected operational settings: %+v", resolved)
	}
	if len(resolved.Headers) != 2 || resolved.Headers[0] != "X-A=1" || resolved.Headers[1] != "X-B=2" {
		t.Fatalf("unexpected headers %v", resolved.Headers)
	}
	if len(resolved.QueryParams) != 1 || resolved.QueryParams[0] != "api-version=2024-02-01" {
		t.Fatalf("unexpected query params %v", resolved.QueryParams)
	}
}

func TestWithDefaultsPreservesPrefixWhitespace(t *testing.T) {
	t.Setenv(EnvPrefix+"DOCUMENT_PREFIX", "passage: ")
	t.Setenv(EnvPrefix+"QUERY_PREFIX", "query: ")

	resolved, _, err := Options{}.withDefaults()
	if err != nil {
		t.Fatalf("apply defaults: %v", err)
	}
	if resolved.DocPrefix != "passage: " || resolved.QueryPrefix != "query: " {
		t.Fatalf("prefix whitespace must be preserved, got %q and %q", resolved.DocPrefix, resolved.QueryPrefix)
	}
}

func TestWithDefaultsAppliesFallbacks(t *testing.T) {
	resolved, providerFromEnv, err := Options{MaxRetries: -1}.withDefaults()
	if err != nil {
		t.Fatalf("apply defaults: %v", err)
	}
	if providerFromEnv {
		t.Fatal("expected no provider from the environment")
	}
	if resolved.Provider != DefaultProvider {
		t.Fatalf("expected default provider, got %q", resolved.Provider)
	}
	if resolved.BatchSize != DefaultBatchSize || resolved.Timeout != DefaultTimeout {
		t.Fatalf("unexpected defaults: %+v", resolved)
	}
	if resolved.MaxRetries != -1 {
		t.Fatalf("expected an explicit negative retry count to be preserved, got %d", resolved.MaxRetries)
	}
}

func TestNoneProviderReportsDisabled(t *testing.T) {
	provider := NoneProvider{}
	if provider.Name() != "none" || provider.Dimension() != 0 {
		t.Fatalf("unexpected disabled provider identity: %q dim %d", provider.Name(), provider.Dimension())
	}
	if provider.Caps().Enabled() {
		t.Fatal("expected the disabled provider to report no capability")
	}
}
