package embedding

import (
	"context"
	"fmt"
	"strings"
)

// Resolution is the result of provider selection.
type Resolution struct {
	Provider Provider
	// Source reports which configuration layer selected the provider:
	// SourceFlag, SourceEnv or SourceDefault.
	Source string
}

// NoneProvider marks embeddings as explicitly disabled. It keeps Resolution
// non-nil so callers can inspect capabilities instead of guarding for a nil
// provider.
type NoneProvider struct{}

// Name returns the disabled marker used in command output.
func (NoneProvider) Name() string { return "none" }

// Dimension returns 0 because no vector space exists.
func (NoneProvider) Dimension() int { return 0 }

// Caps reports that neither a semantic nor a lexical vector channel is available.
func (NoneProvider) Caps() Caps { return Caps{} }

// Embed always fails; callers must check capabilities before embedding.
func (NoneProvider) Embed(context.Context, Kind, []string) ([][]float32, error) {
	return nil, fmt.Errorf("embeddings are disabled (skip is set)")
}

// Resolve selects and builds a provider. Unset option fields are filled from
// CONFLUENCE2MD_EMBEDDING_* variables and then from built-in defaults, so
// Resolve(Options{}) always returns a usable provider.
func Resolve(opts Options) (Resolution, error) {
	providerFromFlag := strings.TrimSpace(opts.Provider) != ""

	resolved, providerFromEnv, err := opts.withDefaults()
	if err != nil {
		return Resolution{}, err
	}

	source := SourceDefault
	switch {
	case providerFromFlag:
		source = SourceFlag
	case providerFromEnv:
		source = SourceEnv
	}

	if resolved.Skip {
		return Resolution{Provider: NoneProvider{}, Source: source}, nil
	}

	factory, ok := Lookup(resolved.Provider)
	if !ok {
		return Resolution{}, fmt.Errorf(
			"unknown embedding provider %q (available: %s)",
			resolved.Provider, strings.Join(Available(), ", "),
		)
	}

	provider, err := factory(resolved)
	if err != nil {
		return Resolution{}, fmt.Errorf("configure embedding provider %q: %w", resolved.Provider, err)
	}

	return Resolution{Provider: provider, Source: source}, nil
}
