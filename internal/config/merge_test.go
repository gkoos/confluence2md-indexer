package config

import (
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/embedding"
)

func stringPointer(value string) *string { return &value }

func intPointer(value int) *int { return &value }

// flagLayer and envLayer build layers with the given values marked present, in the
// way the CLI does when a flag was passed or an environment variable was set.
func flagLayer(options embedding.Options, present embedding.Present) embedding.Layer {
	return embedding.Layer{Name: embedding.SourceFlag, Options: options, Present: present}
}

func envLayer(options embedding.Options, present embedding.Present) embedding.Layer {
	return embedding.Layer{Name: embedding.SourceEnv, Options: options, Present: present}
}

func TestMergeEmbeddingWithoutLayersUsesDefaults(t *testing.T) {
	got := MergeEmbedding(flagLayer(embedding.Options{}, embedding.Present{}), envLayer(embedding.Options{}, embedding.Present{}), File{})

	if got.Provider != embedding.DefaultProvider {
		t.Fatalf("provider = %q, want %q", got.Provider, embedding.DefaultProvider)
	}
	if got.Source != embedding.SourceDefault {
		t.Fatalf("source = %q, want %q", got.Source, embedding.SourceDefault)
	}
	if got.BatchSize != embedding.DefaultBatchSize {
		t.Fatalf("batch size = %d, want %d", got.BatchSize, embedding.DefaultBatchSize)
	}
	if got.Timeout != embedding.DefaultTimeout {
		t.Fatalf("timeout = %s, want %s", got.Timeout, embedding.DefaultTimeout)
	}
	if got.MaxRetries != embedding.DefaultMaxRetries {
		t.Fatalf("max retries = %d, want %d", got.MaxRetries, embedding.DefaultMaxRetries)
	}
}

func TestMergeEmbeddingTakesEachFieldFromItsHighestLayer(t *testing.T) {
	file := File{Embedding: &Embedding{
		Provider:  stringPointer("openai-compatible"),
		Model:     stringPointer("file-model"),
		BaseURL:   stringPointer("https://file.test/v1"),
		Dimension: intPointer(1024),
	}}

	got := MergeEmbedding(
		flagLayer(embedding.Options{}, embedding.Present{}),
		envLayer(embedding.Options{Model: "env-model", Dimension: 512}, embedding.Present{Model: true, Dimension: true}),
		file,
	)

	if got.Provider != "openai-compatible" || got.Source != embedding.SourceConfig {
		t.Fatalf("provider = %q (source %q), want the file to decide", got.Provider, got.Source)
	}
	if got.Model != "env-model" {
		t.Fatalf("model = %q, want the environment to win over the file", got.Model)
	}
	if got.Dimension != 512 {
		t.Fatalf("dimension = %d, want the environment to win over the file", got.Dimension)
	}
	if got.BaseURL != "https://file.test/v1" {
		t.Fatalf("base url = %q, want the file value to survive", got.BaseURL)
	}
}

func TestMergeEmbeddingFlagBeatsEnvironmentAndFile(t *testing.T) {
	file := File{Embedding: &Embedding{Provider: stringPointer("bow-local")}}

	got := MergeEmbedding(
		flagLayer(embedding.Options{Provider: "openai"}, embedding.Present{Provider: true}),
		envLayer(embedding.Options{Provider: "env-provider"}, embedding.Present{Provider: true}),
		file,
	)

	if got.Provider != "openai" {
		t.Fatalf("provider = %q, want the flag to win", got.Provider)
	}
	if got.Source != embedding.SourceFlag {
		t.Fatalf("source = %q, want %q", got.Source, embedding.SourceFlag)
	}
}

func TestMergeEmbeddingTakesProviderSourceFromTheWinningLayer(t *testing.T) {
	file := File{Embedding: &Embedding{Model: stringPointer("file-model")}}

	got := MergeEmbedding(
		flagLayer(embedding.Options{}, embedding.Present{}),
		envLayer(embedding.Options{Provider: "openai-compatible"}, embedding.Present{Provider: true}),
		file,
	)

	if got.Source != embedding.SourceEnv {
		t.Fatalf("source = %q, want %q", got.Source, embedding.SourceEnv)
	}
	if got.Model != "file-model" {
		t.Fatalf("model = %q, want the file value", got.Model)
	}
}

func TestMergeEmbeddingPresenceBeatsAnEmptyValue(t *testing.T) {
	file := File{Embedding: &Embedding{
		Provider: stringPointer("bow-local"),
		Model:    stringPointer("file-model"),
	}}

	// A flag that sets an empty model clears the configured one.
	got := MergeEmbedding(
		flagLayer(embedding.Options{}, embedding.Present{Model: true}),
		envLayer(embedding.Options{}, embedding.Present{}),
		file,
	)

	if got.Model != "" {
		t.Fatalf("model = %q, want the empty flag to clear the file value", got.Model)
	}
	if got.Provider != "bow-local" {
		t.Fatalf("provider = %q, want the untouched file value", got.Provider)
	}
}

func TestMergeEmbeddingNormalizesProvider(t *testing.T) {
	file := File{Embedding: &Embedding{Provider: stringPointer("  BOW-Local  ")}}

	got := MergeEmbedding(
		flagLayer(embedding.Options{}, embedding.Present{}),
		envLayer(embedding.Options{}, embedding.Present{}),
		file,
	)

	if got.Provider != embedding.ProviderBowLocal {
		t.Fatalf("provider = %q, want %q", got.Provider, embedding.ProviderBowLocal)
	}
}

func TestMergeEmbeddingCarriesListsFromTheFile(t *testing.T) {
	file := File{Embedding: &Embedding{
		Headers:     []string{"X-Tenant=docs"},
		QueryParams: []string{"api-version=2024-05"},
	}}

	got := MergeEmbedding(
		flagLayer(embedding.Options{Headers: []string{"X-Flag=1"}}, embedding.Present{Headers: true}),
		envLayer(embedding.Options{QueryParams: []string{"env=1"}}, embedding.Present{QueryParams: true}),
		file,
	)

	if len(got.Headers) != 1 || got.Headers[0] != "X-Flag=1" {
		t.Fatalf("headers = %v, want the flag value", got.Headers)
	}
	if len(got.QueryParams) != 1 || got.QueryParams[0] != "env=1" {
		t.Fatalf("query params = %v, want the environment value", got.QueryParams)
	}
}
