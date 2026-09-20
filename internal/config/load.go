package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"

	"github.com/gkoos/confluence2md-indexer/internal/embedding"
)

// Load reads the configuration file.
//
// An empty path looks for the file named by EnvFileName and then for
// DefaultFileName in the working directory; when neither exists, Load returns an
// empty Config, because running without a file is supported. A path that comes
// from the caller or from EnvFileName must exist, so a typo fails loudly.
func Load(path string) (Config, error) {
	path = strings.TrimSpace(path)
	required := path != ""

	if path == "" {
		path = strings.TrimSpace(os.Getenv(EnvFileName))
		required = path != ""
	}
	if path == "" {
		if _, err := os.Stat(DefaultFileName); err != nil {
			return Config{}, nil
		}
		path = DefaultFileName
	}

	if _, err := os.Stat(path); err != nil {
		switch {
		case !os.IsNotExist(err):
			return Config{}, fmt.Errorf("read config file %s: %w", path, err)
		case required:
			return Config{}, fmt.Errorf("config file %s does not exist", path)
		default:
			return Config{}, nil
		}
	}

	reader := viper.New()
	reader.SetConfigFile(path)
	if err := reader.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("read config file %s: %w", path, err)
	}

	// UnmarshalExact rejects unknown keys, so a typo fails loudly instead of
	// silently falling back to a default. The format follows the file extension.
	var file File
	if err := reader.UnmarshalExact(&file); err != nil {
		return Config{}, fmt.Errorf("parse config file %s: %w", path, err)
	}
	if err := file.validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config file %s: %w", path, err)
	}

	return Config{Path: absolutePath(path), File: file}, nil
}

// absolutePath resolves a path for reporting, so a relative --config still names
// the file it read.
func absolutePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// DBPath returns the index path the file sets, or an empty string when it sets
// none.
func (f File) DBPath() string {
	if f.DB == nil || f.DB.Path == nil {
		return ""
	}
	return strings.TrimSpace(*f.DB.Path)
}

// EmbeddingOptions returns the embedding values the file carries, together with
// the fields that supplied one.
func (f File) EmbeddingOptions() (embedding.Options, embedding.Present) {
	var options embedding.Options
	var present embedding.Present

	if f.Embedding == nil {
		return options, present
	}

	e := f.Embedding
	if e.Provider != nil {
		options.Provider, present.Provider = *e.Provider, true
	}
	if e.Model != nil {
		options.Model, present.Model = *e.Model, true
	}
	if e.BaseURL != nil {
		options.BaseURL, present.BaseURL = *e.BaseURL, true
	}
	if e.Path != nil {
		options.Path, present.Path = *e.Path, true
	}
	if e.Dimension != nil {
		options.Dimension, present.Dimension = *e.Dimension, true
	}
	if e.APIKeyEnv != nil {
		options.APIKeyEnv, present.APIKeyEnv = *e.APIKeyEnv, true
	}
	if e.APIKey != nil {
		options.APIKey, present.APIKey = *e.APIKey, true
	}
	if e.AuthHeader != nil {
		options.AuthHeader, present.AuthHeader = *e.AuthHeader, true
	}
	if e.AuthScheme != nil {
		options.AuthScheme, present.AuthScheme = *e.AuthScheme, true
	}
	if e.Headers != nil {
		options.Headers, present.Headers = e.Headers, true
	}
	if e.QueryParams != nil {
		options.QueryParams, present.QueryParams = e.QueryParams, true
	}
	if e.DocumentPrefix != nil {
		options.DocPrefix, present.DocPrefix = *e.DocumentPrefix, true
	}
	if e.QueryPrefix != nil {
		options.QueryPrefix, present.QueryPrefix = *e.QueryPrefix, true
	}
	if e.BatchSize != nil {
		options.BatchSize, present.BatchSize = *e.BatchSize, true
	}
	if e.Timeout != nil {
		options.Timeout, present.Timeout = *e.Timeout, true
	}
	if e.MaxRetries != nil {
		options.MaxRetries, present.MaxRetries = *e.MaxRetries, true
	}
	if e.Skip != nil {
		options.Skip, present.Skip = *e.Skip, true
	}

	return options, present
}
