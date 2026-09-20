// Package config reads the optional YAML configuration file.
//
// The file is optional by design: every setting it can hold also has a flag, an
// environment variable and a built-in default, so the tool runs with zero
// configuration. When a file is present, presence decides precedence: a key the
// file carries beats the built-in default, an environment variable that is set
// beats the file, and a flag that was passed beats both.
package config

import "time"

// DefaultFileName is the file that is read from the working directory when
// --config is not passed.
const DefaultFileName = "config.yaml"

// EnvFileName names the environment variable that points at a configuration
// file, so a deployment can keep the file out of the working directory. It takes
// precedence over DefaultFileName.
const EnvFileName = "CONFLUENCE2MD_CONFIG"

// Config is the result of Load.
type Config struct {
	// Path is the file that supplied the settings. It is empty when no file was
	// found, which is a normal and fully supported state.
	Path string
	// File holds the settings themselves.
	File File
}

// File mirrors the configuration file. Every field is a pointer so that a key
// that is present stays distinguishable from a key that is absent, which is what
// presence-exact precedence needs.
type File struct {
	DB        *DB        `mapstructure:"db"`
	Embedding *Embedding `mapstructure:"embedding"`
}

// DB holds index storage settings.
type DB struct {
	// Path overrides where the index lives. The default is a database file inside
	// the folder being indexed.
	Path *string `mapstructure:"path"`
}

// Embedding holds vector embedding settings. Its keys mirror the --embedding-*
// flags and the CONFLUENCE2MD_EMBEDDING_* environment variables.
type Embedding struct {
	Provider       *string        `mapstructure:"provider"`
	Model          *string        `mapstructure:"model"`
	BaseURL        *string        `mapstructure:"base_url"`
	Path           *string        `mapstructure:"path"`
	Dimension      *int           `mapstructure:"dimension"`
	APIKey         *string        `mapstructure:"api_key"`
	APIKeyEnv      *string        `mapstructure:"api_key_env"`
	AuthHeader     *string        `mapstructure:"auth_header"`
	AuthScheme     *string        `mapstructure:"auth_scheme"`
	Headers        []string       `mapstructure:"headers"`
	QueryParams    []string       `mapstructure:"query_params"`
	DocumentPrefix *string        `mapstructure:"document_prefix"`
	QueryPrefix    *string        `mapstructure:"query_prefix"`
	BatchSize      *int           `mapstructure:"batch_size"`
	Timeout        *time.Duration `mapstructure:"timeout"`
	MaxRetries     *int           `mapstructure:"max_retries"`
	Skip           *bool          `mapstructure:"skip"`
}
