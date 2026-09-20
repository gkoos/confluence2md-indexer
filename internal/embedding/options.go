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
	ProviderBowLocal = "bow-local"
	ProviderOpenAI   = "openai"
)

// Resolution sources, reported in command output so users can see which layer
// selected the provider.
const (
	SourceFlag    = "flag"
	SourceEnv     = "env"
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

// Options carries embedding configuration from flags, environment and defaults.
//
// Flag-provided values take precedence over environment variables, which take
// precedence over built-in defaults. Secrets are never passed in Options
// directly: APIKeyEnv names the environment variable that holds the key.
type Options struct {
	Provider    string
	Model       string
	BaseURL     string
	Path        string
	Dimension   int
	APIKeyEnv   string
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
}

// envOptions holds values read from the environment.
type envOptions struct {
	provider    string
	model       string
	baseURL     string
	path        string
	dimension   int
	apiKeyEnv   string
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
	env, err := readEnvOptions()
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

// readEnvOptions parses CONFLUENCE2MD_EMBEDDING_* variables. Malformed numeric
// or duration values are reported instead of silently ignored, because a typo
// in DIM would otherwise index a corpus with an unexpected vector size.
func readEnvOptions() (envOptions, error) {
	var env envOptions

	env.provider = envString("PROVIDER")
	env.model = envString("MODEL")
	env.baseURL = envString("BASE_URL")
	env.path = envString("PATH")
	env.apiKeyEnv = envString("API_KEY_ENV")
	env.authHeader = envString("AUTH_HEADER")
	env.authScheme = envString("AUTH_SCHEME")
	env.docPrefix = os.Getenv(EnvPrefix + "DOCUMENT_PREFIX")
	env.queryPrefix = os.Getenv(EnvPrefix + "QUERY_PREFIX")
	env.headers = envList("HEADERS")
	env.queryParams = envList("QUERY_PARAMS")

	dimension, err := envInt("DIM")
	if err != nil {
		return env, err
	}
	env.dimension = dimension

	batchSize, err := envInt("BATCH_SIZE")
	if err != nil {
		return env, err
	}
	env.batchSize = batchSize

	maxRetries, err := envInt("MAX_RETRIES")
	if err != nil {
		return env, err
	}
	env.maxRetries = maxRetries

	timeout, err := envDuration("TIMEOUT")
	if err != nil {
		return env, err
	}
	env.timeout = timeout

	env.skip = envBool("SKIP")

	return env, nil
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
