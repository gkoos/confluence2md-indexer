package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	openAIBaseURL        = "https://api.openai.com/v1"
	openAIEmbedPath      = "/embeddings"
	openAIDefaultModel   = "text-embedding-3-small"
	openAIDefaultKeyEnv  = "OPENAI_API_KEY"
	openAIMaxBatch       = 2048
	openAIMaxInputTokens = 8191
	maxResponseBytes     = 128 << 20
)

// openAIModelDimensions lists vector sizes for models whose dimension is known
// without calling the API.
var openAIModelDimensions = map[string]int{
	"text-embedding-ada-002": 1536,
	"text-embedding-3-small": 1536,
	"text-embedding-3-large": 3072,
}

// openAIModelsWithCustomDimensions can be shortened through the dimensions field.
var openAIModelsWithCustomDimensions = map[string]struct{}{
	"text-embedding-3-small": {},
	"text-embedding-3-large": {},
}

// wireDefaults separates the OpenAI flavour from other OpenAI-compatible
// endpoints that share the same request and response shape.
type wireDefaults struct {
	id              string
	defaultModel    string
	defaultBaseURL  string
	defaultPath     string
	defaultKeyEnv   string
	modelDimensions map[string]int
	customDims      map[string]struct{}
	maxBatch        int
	maxInputTokens  int
	// requireDimension forces an explicit dimension for endpoints whose models
	// cannot be looked up locally.
	requireDimension bool
	// requireKey makes an API key mandatory for every request.
	requireKey bool
	// sendDimensionParam requests truncated vectors through the dimensions field.
	// Only endpoints that document that parameter may set this: for the others a
	// configured dimension is a declaration that is validated, not a request.
	sendDimensionParam bool
}

type headerPair struct {
	name  string
	value string
}

type openAIProvider struct {
	identity       string
	model          string
	dim            int
	sendDimensions bool
	baseURL        string
	path           string
	queryParams    []headerPair
	headers        []headerPair
	apiKey         string
	authHeader     string
	authScheme     string
	docPrefix      string
	queryPrefix    string
	caps           Caps
	client         *http.Client
	batch          *batchEmbedder
}

// newOpenAIWireProvider builds a provider that speaks the OpenAI embeddings
// wire protocol. The flavour is selected by defaults, which lets the same
// implementation serve both the OpenAI API and self-hosted compatible servers.
func newOpenAIWireProvider(opts Options, defaults wireDefaults) (*openAIProvider, error) {
	model := firstNonEmpty(opts.Model, defaults.defaultModel)
	if model == "" {
		return nil, fmt.Errorf("%s requires a model name", defaults.id)
	}

	baseURL := strings.TrimRight(firstNonEmpty(opts.BaseURL, defaults.defaultBaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("%s requires a base URL", defaults.id)
	}

	path := firstNonEmpty(opts.Path, defaults.defaultPath)
	if path == "" {
		path = openAIEmbedPath
	}

	dim, dimOverride, err := resolveWireDimension(opts, defaults, model)
	if err != nil {
		return nil, err
	}
	if !defaults.sendDimensionParam {
		// The endpoint does not document a dimensions parameter, so the configured
		// dimension only declares what to expect from responses.
		dimOverride = false
	}

	headers, err := parsePairList(opts.Headers, "header")
	if err != nil {
		return nil, err
	}
	queryParams, err := parsePairList(opts.QueryParams, "query param")
	if err != nil {
		return nil, err
	}

	apiKeyEnv := firstNonEmpty(opts.APIKeyEnv, defaults.defaultKeyEnv)
	apiKey := ""
	if apiKeyEnv != "" {
		apiKey = strings.TrimSpace(os.Getenv(apiKeyEnv))
		if apiKey == "" {
			return nil, fmt.Errorf(
				"%s requires an API key in %s (set it, or name another variable with --embedding-api-key-env)",
				defaults.id, apiKeyEnv,
			)
		}
	}
	if apiKey == "" && defaults.requireKey {
		return nil, fmt.Errorf("%s requires an API key", defaults.id)
	}

	authHeader := firstNonEmpty(opts.AuthHeader, "Authorization")
	authScheme := "Bearer"
	switch scheme := strings.TrimSpace(opts.AuthScheme); strings.ToLower(scheme) {
	case "":
		// Keep the Bearer default.
	case "none":
		// Some gateways, notably Azure OpenAI, send the key with no scheme.
		authScheme = ""
	default:
		authScheme = scheme
	}
	if apiKey == "" {
		// Keyless endpoints (local servers) must not receive an auth header.
		authHeader = ""
		authScheme = ""
	}

	batchSize := opts.BatchSize
	if defaults.maxBatch > 0 {
		batchSize = min(batchSize, defaults.maxBatch)
	}

	provider := &openAIProvider{
		model:          model,
		dim:            dim,
		sendDimensions: dimOverride,
		baseURL:        baseURL,
		path:           path,
		queryParams:    queryParams,
		headers:        headers,
		apiKey:         apiKey,
		authHeader:     authHeader,
		authScheme:     authScheme,
		docPrefix:      opts.DocPrefix,
		queryPrefix:    opts.QueryPrefix,
		caps: Caps{
			Semantic:       true,
			Asymmetric:     strings.TrimSpace(opts.DocPrefix) != "" || strings.TrimSpace(opts.QueryPrefix) != "",
			MaxBatch:       defaults.maxBatch,
			MaxInputTokens: defaults.maxInputTokens,
		},
		client: &http.Client{Timeout: opts.Timeout},
	}

	provider.identity = identityString(defaults.id, model, dim, provider.identityParts(defaults)...)
	provider.batch = newBatchEmbedder(batchSize, opts.MaxRetries, provider.doRequest)

	return provider, nil
}

// resolveWireDimension picks the vector size from explicit configuration or from
// the known model table, and rejects dimensions the model cannot produce.
func resolveWireDimension(opts Options, defaults wireDefaults, model string) (int, bool, error) {
	known := defaults.modelDimensions[model]

	if opts.Dimension > 0 {
		if known > 0 && opts.Dimension != known {
			if _, supports := defaults.customDims[model]; !supports {
				return 0, false, fmt.Errorf("model %q does not support a custom dimension (default %d)", model, known)
			}
		}
		return opts.Dimension, true, nil
	}

	if known > 0 {
		return known, false, nil
	}

	if defaults.requireDimension {
		return 0, false, fmt.Errorf(
			"%s requires --embedding-dim for model %q because its dimension is not known locally",
			defaults.id, model,
		)
	}
	return 0, false, fmt.Errorf("unknown dimension for model %q: pass --embedding-dim", model)
}

// parsePairList validates repeated "key=value" settings.
func parsePairList(values []string, label string) ([]headerPair, error) {
	if len(values) == 0 {
		return nil, nil
	}

	out := make([]headerPair, 0, len(values))
	for _, value := range values {
		name, setting, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("embedding %s %q must be in key=value form", label, value)
		}
		out = append(out, headerPair{name: strings.TrimSpace(name), value: strings.TrimSpace(setting)})
	}
	return out, nil
}

// identityParts adds markers for configuration that changes the vector space:
// a non-default endpoint or configured text prefixes.
func (p *openAIProvider) identityParts(defaults wireDefaults) []identityPart {
	parts := make([]identityPart, 0, 2)

	defaultPath := firstNonEmpty(defaults.defaultPath, openAIEmbedPath)
	if p.baseURL != strings.TrimRight(defaults.defaultBaseURL, "/") || p.path != defaultPath {
		parts = append(parts, identityPart{key: "endpoint", value: shortTag(p.baseURL, p.path)})
	}
	if p.docPrefix != "" || p.queryPrefix != "" {
		parts = append(parts, identityPart{key: "prefixes", value: shortTag(p.docPrefix, p.queryPrefix)})
	}

	return parts
}

// Name returns the canonical identity, for example
// "openai:text-embedding-3-small@1536".
func (p *openAIProvider) Name() string { return p.identity }

// Dimension returns the configured vector size.
func (p *openAIProvider) Dimension() int { return p.dim }

// Caps reports a semantic provider with the endpoint's request limits.
func (p *openAIProvider) Caps() Caps { return p.caps }

// Embed converts texts, applying the kind-specific prefix when one is configured.
func (p *openAIProvider) Embed(ctx context.Context, kind Kind, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	return p.batch.embed(ctx, p.applyPrefix(kind, texts))
}

func (p *openAIProvider) applyPrefix(kind Kind, texts []string) []string {
	prefix := p.docPrefix
	if kind == KindQuery {
		prefix = p.queryPrefix
	}
	if prefix == "" {
		return texts
	}

	out := make([]string, len(texts))
	for i, text := range texts {
		out[i] = prefix + text
	}
	return out
}

func (p *openAIProvider) endpointURL() string {
	endpoint := p.baseURL + "/" + strings.TrimLeft(p.path, "/")
	if len(p.queryParams) == 0 {
		return endpoint
	}

	values := make(url.Values, len(p.queryParams))
	for _, param := range p.queryParams {
		values.Set(param.name, param.value)
	}
	return endpoint + "?" + values.Encode()
}

func (p *openAIProvider) doRequest(ctx context.Context, texts []string) ([][]float32, time.Duration, error) {
	body := map[string]any{
		"model":           p.model,
		"input":           texts,
		"encoding_format": "float",
	}
	if p.sendDimensions {
		body["dimensions"] = p.dim
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, permanent(fmt.Errorf("encode embeddings request: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpointURL(), bytes.NewReader(payload))
	if err != nil {
		return nil, 0, permanent(fmt.Errorf("create embeddings request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		value := p.apiKey
		if p.authScheme != "" {
			value = p.authScheme + " " + p.apiKey
		}
		req.Header.Set(p.authHeader, value)
	}
	for _, header := range p.headers {
		req.Header.Set(header.name, header.value)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("embeddings request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("read embeddings response: %w", err)
	}

	if resp.StatusCode >= 300 {
		return nil, parseRetryAfter(resp.Header.Get("Retry-After")), &httpStatusError{
			StatusCode: resp.StatusCode,
			Message:    wireErrorMessage(raw),
		}
	}

	// Response shape problems are permanent: retrying the same request would
	// produce the same malformed payload.
	vectors, _, err := p.decodeVectors(raw, len(texts))
	if err != nil {
		return nil, 0, permanent(err)
	}
	return vectors, 0, nil
}

func (p *openAIProvider) decodeVectors(raw []byte, expected int) ([][]float32, time.Duration, error) {
	var decoded struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, 0, fmt.Errorf("decode embeddings response: %w", err)
	}
	if len(decoded.Data) != expected {
		return nil, 0, fmt.Errorf("embeddings response contained %d vectors for %d inputs", len(decoded.Data), expected)
	}

	out := make([][]float32, expected)
	for position, item := range decoded.Data {
		index := item.Index
		if index < 0 || index >= expected {
			// Some gateways omit or mishandle index; fall back to array order.
			index = position
		}
		if len(item.Embedding) == 0 {
			return nil, 0, fmt.Errorf("embeddings response had an empty vector at index %d", index)
		}
		if len(item.Embedding) != p.dim {
			return nil, 0, fmt.Errorf(
				"embeddings response dimension %d does not match the configured dimension %d",
				len(item.Embedding), p.dim,
			)
		}
		vec := make([]float32, len(item.Embedding))
		copy(vec, item.Embedding)
		normalize(vec)
		out[index] = vec
	}

	for index, vec := range out {
		if vec == nil {
			return nil, 0, fmt.Errorf("embeddings response missing vector at index %d", index)
		}
	}
	return out, 0, nil
}

// wireErrorMessage extracts a useful message from an error body. Gateways differ:
// OpenAI nests it under "error.message", others return a bare "message".
func wireErrorMessage(raw []byte) string {
	var envelope struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil {
		if envelope.Error != nil && strings.TrimSpace(envelope.Error.Message) != "" {
			return strings.TrimSpace(envelope.Error.Message)
		}
		if strings.TrimSpace(envelope.Message) != "" {
			return strings.TrimSpace(envelope.Message)
		}
	}

	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) > 200 {
		trimmed = trimmed[:200] + "..."
	}
	return trimmed
}

// newOpenAIProvider registers the OpenAI flavour of the wire protocol.
func newOpenAIProvider(opts Options) (Provider, error) {
	return newOpenAIWireProvider(opts, wireDefaults{
		id:              ProviderOpenAI,
		defaultModel:    openAIDefaultModel,
		defaultBaseURL:  openAIBaseURL,
		defaultPath:     openAIEmbedPath,
		defaultKeyEnv:   openAIDefaultKeyEnv,
		modelDimensions: openAIModelDimensions,
		customDims:      openAIModelsWithCustomDimensions,
		maxBatch:        openAIMaxBatch,
		maxInputTokens:  openAIMaxInputTokens,
		requireKey:      true,
		// OpenAI's v3 models accept a dimensions field for shortened vectors.
		sendDimensionParam: true,
	})
}

func init() {
	MustRegister(ProviderOpenAI, newOpenAIProvider)
}
