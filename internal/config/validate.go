package config

import (
	"errors"
	"fmt"
	"strings"
)

// validate rejects values that are wrong in any context. Checks that depend on
// other configuration layers, such as a provider id that only the environment
// supplies, happen after merging, inside the provider that consumes them.
//
// An empty value is not an error: an empty db.path or provider id means "keep the
// value from a lower layer", which is what a template with every key spelled out
// relies on.
func (f File) validate() error {
	if f.Embedding == nil {
		return nil
	}

	e := f.Embedding
	if hasValue(e.APIKey) && hasValue(e.APIKeyEnv) {
		return errors.New("set only one of embedding.api_key and embedding.api_key_env")
	}
	if e.Dimension != nil && *e.Dimension < 0 {
		return fmt.Errorf("embedding.dimension must not be negative, got %d", *e.Dimension)
	}
	if e.BatchSize != nil && *e.BatchSize < 0 {
		return fmt.Errorf("embedding.batch_size must not be negative, got %d", *e.BatchSize)
	}
	if e.Timeout != nil && *e.Timeout < 0 {
		return fmt.Errorf("embedding.timeout must not be negative, got %s", *e.Timeout)
	}
	if e.MaxRetries != nil && *e.MaxRetries < -1 {
		return fmt.Errorf("embedding.max_retries must be -1 or greater, got %d", *e.MaxRetries)
	}

	return nil
}

// hasValue reports whether an optional string holds something other than
// whitespace.
func hasValue(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}
