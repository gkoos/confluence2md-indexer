package embedding

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// EnvPrefix namespaces every embedding environment variable.
const EnvPrefix = "CONFLUENCE2MD_EMBEDDING_"

// Built-in provider identifiers.
const (
	ProviderBowLocal         = "bow-local"
	ProviderOpenAI           = "openai"
	ProviderOpenAICompatible = "openai-compatible"
)

// Resolution sources, reported in command output so users can see which layer
// selected the provider.
const (
	SourceFlag    = "flag"
	SourceEnv     = "env"
	SourceConfig  = "config"
	SourceDefault = "default"
)

// Defaults applied when neither flags nor environment configure a value.
const (
	DefaultProvider   = ProviderBowLocal
	DefaultBowDim     = 256
	DefaultBatchSize  = 64
	DefaultTimeout    = 45 * time.Second
	DefaultMaxRetries = 3
)

// Options carries embedding configuration from flags, the configuration file,
// the environment and built-in defaults.
//
// Precedence, highest first: a flag that was passed, an environment variable
// that is set, a configuration key that is present, then built-in defaults. Merge
// implements that order; Resolve applies environment and defaults to options that
// a caller built by hand.
//
// Secrets need not appear here: APIKeyEnv names the environment variable that
// holds the key, while APIKey holds a literal value that only a configuration
// file should supply.
type Options struct {
	Provider  string
	Model     string
	BaseURL   string
	Path      string
	Dimension int
	APIKeyEnv string
	// APIKey is a literal credential. Naming a variable through APIKeyEnv is
	// preferred, because a value here ends up in whichever file supplied it.
	APIKey      string
	AuthHeader  string
	AuthScheme  string
	Headers     []string
	QueryParams []string
	DocPrefix   string
	QueryPrefix string
	BatchSize   int
	Timeout     time.Duration
	// MaxRetries counts retries after the first attempt. Zero means "unset" and
	// resolves to DefaultMaxRetries; a negative value disables retries.
	MaxRetries int
	// Skip disables the vector channel entirely when true.
	Skip bool
	// Source records which configuration layer supplied the provider id. It is
	// informational and surfaces as embedding.source in command output.
	Source string
}

// Present records which fields a configuration layer actually supplied, so that
// layers merge exactly: a value that is present but empty is still a decision,
// while an omitted value is not.
type Present struct {
	Provider    bool
	Model       bool
	BaseURL     bool
	Path        bool
	Dimension   bool
	APIKeyEnv   bool
	APIKey      bool
	AuthHeader  bool
	AuthScheme  bool
	Headers     bool
	QueryParams bool
	DocPrefix   bool
	QueryPrefix bool
	BatchSize   bool
	Timeout     bool
	MaxRetries  bool
	Skip        bool
}

// ReadEnv returns environment configuration together with the fields that
// supplied a value, so callers can merge it as a configuration layer.
//
// An environment variable that is unset or empty counts as absent, which keeps
// the previous behaviour of this package. To clear a value that a configuration
// file sets, pass the matching flag with an empty value instead.
func ReadEnv() (Options, Present, error) {
	env, present, err := readEnvOptions()
	if err != nil {
		return Options{}, Present{}, err
	}
	return env.options(), present, nil
}

// options converts environment values into Options.
func (env envOptions) options() Options {
	return Options{
		Provider:    env.provider,
		Model:       env.model,
		BaseURL:     env.baseURL,
		Path:        env.path,
		Dimension:   env.dimension,
		APIKeyEnv:   env.apiKeyEnv,
		APIKey:      env.apiKey,
		AuthHeader:  env.authHeader,
		AuthScheme:  env.authScheme,
		Headers:     env.headers,
		QueryParams: env.queryParams,
		DocPrefix:   env.docPrefix,
		QueryPrefix: env.queryPrefix,
		BatchSize:   env.batchSize,
		Timeout:     env.timeout,
		MaxRetries:  env.maxRetries,
		Skip:        env.skip,
	}
}

// envOptions holds values read from the environment.
type envOptions struct {
	provider    string
	model       string
	baseURL     string
	path        string
	dimension   int
	apiKeyEnv   string
	apiKey      string
	authHeader  string
	authScheme  string
	headers     []string
	queryParams []string
	docPrefix   string
	queryPrefix string
	batchSize   int
	timeout     time.Duration
	maxRetries  int
	skip        bool
}

// withDefaults fills unset fields from the environment and then from built-in
// defaults. It reports whether the provider id came from the environment.
func (opts Options) withDefaults() (Options, bool, error) {
	env, _, err := readEnvOptions()
	if err != nil {
		return opts, false, err
	}

	providerFromEnv := false
	if strings.TrimSpace(opts.Provider) == "" {
		opts.Provider = env.provider
		providerFromEnv = env.provider != ""
	}

	opts.Model = firstNonEmpty(opts.Model, env.model)
	opts.BaseURL = firstNonEmpty(opts.BaseURL, env.baseURL)
	opts.Path = firstNonEmpty(opts.Path, env.path)
	opts.APIKeyEnv = firstNonEmpty(opts.APIKeyEnv, env.apiKeyEnv)
	opts.AuthHeader = firstNonEmpty(opts.AuthHeader, env.authHeader)
	opts.AuthScheme = firstNonEmpty(opts.AuthScheme, env.authScheme)
	// Prefixes keep their exact whitespace: models such as E5 and nomic expect
	// trailing spaces before the text.
	if opts.DocPrefix == "" {
		opts.DocPrefix = env.docPrefix
	}
	if opts.QueryPrefix == "" {
		opts.QueryPrefix = env.queryPrefix
	}
	opts.Headers = append(opts.Headers, env.headers...)
	opts.QueryParams = append(opts.QueryParams, env.queryParams...)

	if opts.Dimension == 0 {
		opts.Dimension = env.dimension
	}
	if opts.BatchSize == 0 {
		opts.BatchSize = env.batchSize
	}
	if opts.Timeout == 0 {
		opts.Timeout = env.timeout
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = env.maxRetries
	}
	if !opts.Skip {
		opts.Skip = env.skip
	}

	if opts.BatchSize == 0 {
		opts.BatchSize = DefaultBatchSize
	}
	if opts.Timeout == 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = DefaultMaxRetries
	}
	if strings.TrimSpace(opts.Provider) == "" {
		opts.Provider = DefaultProvider
	}
	opts.Provider = strings.ToLower(strings.TrimSpace(opts.Provider))

	return opts, providerFromEnv, nil
}

// readEnvOptions parses CONFLUENCE2MD_EMBEDDING_* variables and reports which of
// them supplied a value, so the environment can be merged as a configuration
// layer. Malformed numeric or duration values are reported instead of silently
// ignored, because a typo in DIM would otherwise index a corpus with an
// unexpected vector size.
func readEnvOptions() (envOptions, Present, error) {
	var env envOptions
	var present Present

	if value := envString("PROVIDER"); value != "" {
		env.provider = value
		present.Provider = true
	}
	if value := envString("MODEL"); value != "" {
		env.model = value
		present.Model = true
	}
	if value := envString("BASE_URL"); value != "" {
		env.baseURL = value
		present.BaseURL = true
	}
	if value := envString("PATH"); value != "" {
		env.path = value
		present.Path = true
	}
	if value := envString("API_KEY_ENV"); value != "" {
		env.apiKeyEnv = value
		present.APIKeyEnv = true
	}
	if value := envString("API_KEY"); value != "" {
		env.apiKey = value
		present.APIKey = true
	}
	if value := envString("AUTH_HEADER"); value != "" {
		env.authHeader = value
		present.AuthHeader = true
	}
	if value := envString("AUTH_SCHEME"); value != "" {
		env.authScheme = value
		present.AuthScheme = true
	}
	// Prefixes keep their exact whitespace: models such as E5 and nomic expect
	// trailing spaces before the text.
	if value := os.Getenv(EnvPrefix + "DOCUMENT_PREFIX"); value != "" {
		env.docPrefix = value
		present.DocPrefix = true
	}
	if value := os.Getenv(EnvPrefix + "QUERY_PREFIX"); value != "" {
		env.queryPrefix = value
		present.QueryPrefix = true
	}
	if envString("HEADERS") != "" {
		env.headers = envList("HEADERS")
		present.Headers = true
	}
	if envString("QUERY_PARAMS") != "" {
		env.queryParams = envList("QUERY_PARAMS")
		present.QueryParams = true
	}

	if envString("DIM") != "" {
		dimension, err := envInt("DIM")
		if err != nil {
			return env, present, err
		}
		env.dimension = dimension
		present.Dimension = true
	}
	if envString("BATCH_SIZE") != "" {
		batchSize, err := envInt("BATCH_SIZE")
		if err != nil {
			return env, present, err
		}
		env.batchSize = batchSize
		present.BatchSize = true
	}
	if envString("MAX_RETRIES") != "" {
		maxRetries, err := envInt("MAX_RETRIES")
		if err != nil {
			return env, present, err
		}
		env.maxRetries = maxRetries
		present.MaxRetries = true
	}
	if envString("TIMEOUT") != "" {
		timeout, err := envDuration("TIMEOUT")
		if err != nil {
			return env, present, err
		}
		env.timeout = timeout
		present.Timeout = true
	}
	if envString("SKIP") != "" {
		env.skip = envBool("SKIP")
		present.Skip = true
	}

	return env, present, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func envString(suffix string) string {
	return strings.TrimSpace(os.Getenv(EnvPrefix + suffix))
}

func envInt(suffix string) (int, error) {
	raw := envString(suffix)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s%s must be an integer, got %q", EnvPrefix, suffix, raw)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s%s must not be negative, got %q", EnvPrefix, suffix, raw)
	}
	return value, nil
}

func envDuration(suffix string) (time.Duration, error) {
	raw := envString(suffix)
	if raw == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s%s must be a duration such as 30s or 2m, got %q", EnvPrefix, suffix, raw)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s%s must be positive, got %q", EnvPrefix, suffix, raw)
	}
	return value, nil
}

func envBool(suffix string) bool {
	switch strings.ToLower(envString(suffix)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// envList reads a semicolon separated list. Semicolons are used rather than
// commas because header values may legitimately contain commas.
func envList(suffix string) []string {
	raw := envString(suffix)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
