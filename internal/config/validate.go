package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gkoos/confluence2md-indexer/internal/query"
)

// validate rejects values that are wrong in any context. Checks that depend on
// other configuration layers, such as a provider id that only the environment
// supplies, happen after merging, inside the provider that consumes them.
//
// An empty value is not an error: an empty db.path or provider id means "keep the
// value from a lower layer", which is what a template with every key spelled out
// relies on.
func (f File) validate() error {
	if err := f.validateQuery(); err != nil {
		return err
	}
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

// validateQuery rejects retrieval defaults that could not be applied: a value the
// query command would reject anyway should fail at load, with the key named.
func (f File) validateQuery() error {
	q := f.Query
	if q == nil {
		return nil
	}

	if q.Priors != nil {
		if _, err := query.ParsePriors(strings.Join(q.Priors, ",")); err != nil {
			return fmt.Errorf("query.priors: %w", err)
		}
	}
	if q.PriorStrength != nil && (*q.PriorStrength < 0 || *q.PriorStrength > 1) {
		return fmt.Errorf("query.prior_strength must be between 0 and 1, got %v", *q.PriorStrength)
	}
	if q.RecencyHalfLife != nil {
		halfLife, err := query.ParseAge(*q.RecencyHalfLife)
		if err != nil {
			return fmt.Errorf("query.recency_half_life %w", err)
		}
		if halfLife < 0 {
			return fmt.Errorf("query.recency_half_life must not be negative, got %s", *q.RecencyHalfLife)
		}
	}

	if err := validateChoice("query.mode", q.Mode, query.Modes); err != nil {
		return err
	}
	if err := validateChoice("query.fusion", q.Fusion, query.Fusions); err != nil {
		return err
	}

	if q.Alpha != nil && (*q.Alpha < 0 || *q.Alpha > 1) {
		return fmt.Errorf("query.alpha must be between 0 and 1, got %v", *q.Alpha)
	}
	if q.TopK != nil && *q.TopK <= 0 {
		return fmt.Errorf("query.top_k must be greater than zero, got %d", *q.TopK)
	}
	if q.CandidateK != nil && *q.CandidateK <= 0 {
		return fmt.Errorf("query.candidate_k must be greater than zero, got %d", *q.CandidateK)
	}
	if q.Expand != nil && *q.Expand < 0 {
		return fmt.Errorf("query.expand must not be negative, got %d", *q.Expand)
	}

	return nil
}

// validateChoice checks a value against the alternatives a setting accepts. The value
// has to match exactly, so a file value cannot mean something subtly different from the
// flag it defaults.
func validateChoice(key string, value *string, allowed []string) error {
	if value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*value)
	for _, candidate := range allowed {
		if trimmed == candidate {
			return nil
		}
	}

	return fmt.Errorf("%s must be one of: %s (got %q)", key, strings.Join(allowed, ", "), *value)
}

// hasValue reports whether an optional string holds something other than
// whitespace.
func hasValue(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}
