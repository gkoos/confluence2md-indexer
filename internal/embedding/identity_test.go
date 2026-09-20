package embedding

import (
	"strings"
	"testing"
	"time"
)

func TestIdentityStringFormat(t *testing.T) {
	if got := identityString("bow-local", "fnv1a", 256); got != "bow-local:fnv1a@256" {
		t.Fatalf("unexpected identity %q", got)
	}

	got := identityString("openai-compatible", "BAAI/bge-m3", 1024,
		identityPart{key: "endpoint", value: "e8a1c3d2"},
		identityPart{key: "prefixes", value: "   "},
	)
	if got != "openai-compatible:BAAI/bge-m3@1024+endpoint:e8a1c3d2" {
		t.Fatalf("expected blank parts to be skipped, got %q", got)
	}
}

func TestShortTagIsStableAndShort(t *testing.T) {
	tag := shortTag("https://api.example.test/v1", "/embeddings")
	if len(tag) != 8 {
		t.Fatalf("expected an 8 character tag, got %q", tag)
	}
	if tag != shortTag("https://api.example.test/v1", "/embeddings") {
		t.Fatal("expected a stable tag")
	}
	if tag == shortTag("https://api.example.test/v2", "/embeddings") {
		t.Fatal("expected different inputs to produce different tags")
	}
}

func TestWireIdentityTracksVectorSpaceNotOperationalSettings(t *testing.T) {
	t.Setenv("TEST_WIRE_KEY", "token")

	baseOptions := func() Options {
		return Options{
			Provider:  ProviderOpenAI,
			BaseURL:   "https://wire.example.test/v1",
			APIKeyEnv: "TEST_WIRE_KEY",
		}
	}
	identityFor := func(mutate func(*Options)) string {
		t.Helper()
		options := baseOptions()
		if mutate != nil {
			mutate(&options)
		}
		provider, err := newOpenAIProvider(options)
		if err != nil {
			t.Fatalf("build provider: %v", err)
		}
		return provider.Name()
	}

	base := identityFor(nil)
	if base != "openai:text-embedding-3-small@1536+endpoint:"+shortTag("https://wire.example.test/v1", openAIEmbedPath) {
		t.Fatalf("unexpected identity %q", base)
	}

	// Operational settings must not invalidate an existing index.
	operational := map[string]func(*Options){
		"batch size": func(o *Options) { o.BatchSize = 7 },
		"timeout":    func(o *Options) { o.Timeout = 3 * time.Second },
		"retries":    func(o *Options) { o.MaxRetries = 1 },
	}
	for name, mutate := range operational {
		if got := identityFor(mutate); got != base {
			t.Fatalf("%s changed the identity: %q vs %q", name, got, base)
		}
	}

	// Anything that changes the vector space must.
	vectorSpace := map[string]func(*Options){
		"model":     func(o *Options) { o.Model = "text-embedding-3-large" },
		"dimension": func(o *Options) { o.Dimension = 512 },
		"endpoint":  func(o *Options) { o.BaseURL = "https://other.example.test/v1" },
		"prefixes":  func(o *Options) { o.QueryPrefix = "query: " },
	}
	for name, mutate := range vectorSpace {
		if got := identityFor(mutate); got == base {
			t.Fatalf("%s did not change the identity (%q)", name, got)
		}
	}
}

func TestWireIdentityDoesNotLeakEndpointDetails(t *testing.T) {
	t.Setenv("TEST_WIRE_KEY", "token")

	provider, err := newOpenAIProvider(Options{
		Provider:  ProviderOpenAI,
		BaseURL:   "https://secret-gateway.internal.corp/v1",
		APIKeyEnv: "TEST_WIRE_KEY",
	})
	if err != nil {
		t.Fatalf("build provider: %v", err)
	}

	if strings.Contains(provider.Name(), "secret-gateway") || strings.Contains(provider.Name(), "internal.corp") {
		t.Fatalf("identity leaked endpoint details: %s", provider.Name())
	}
	if !strings.Contains(provider.Name(), "+endpoint:") {
		t.Fatalf("expected an endpoint marker for a custom endpoint, got %s", provider.Name())
	}
}
