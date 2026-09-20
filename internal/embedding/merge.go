package embedding

import "strings"

// Layer is one source of embedding configuration. Layers are passed to Merge in
// precedence order, highest first: a flag layer, then the environment, then the
// configuration file.
type Layer struct {
	// Name labels the layer ("flag", "env", "config") and becomes the reported
	// provider source when this layer supplied the provider id.
	Name string
	// Options holds the values this layer supplies.
	Options Options
	// Present marks which of those values the layer actually set.
	Present Present
}

// Merge combines configuration layers and applies built-in defaults to whatever
// no layer supplied.
//
// A value comes from the first layer that has it present, so a flag that was
// passed beats an environment variable, which beats a key in the configuration
// file. Presence is exact: a layer that sets a field to an empty value still
// decides, which is how a flag clears a value from the file.
//
// Validation of the combined result belongs to the provider constructors, which
// report missing or malformed settings together with the flag or variable to set.
func Merge(layers ...Layer) Options {
	var merged Options
	var present Present
	source := ""

	for _, layer := range layers {
		// An empty provider id means "keep looking", so a template that spells out
		// every key never hides the layer that actually named a provider.
		if !present.Provider && layer.Present.Provider && strings.TrimSpace(layer.Options.Provider) != "" {
			merged.Provider, present.Provider, source = layer.Options.Provider, true, layer.Name
		}
		if !present.Model && layer.Present.Model {
			merged.Model, present.Model = layer.Options.Model, true
		}
		if !present.BaseURL && layer.Present.BaseURL {
			merged.BaseURL, present.BaseURL = layer.Options.BaseURL, true
		}
		if !present.Path && layer.Present.Path {
			merged.Path, present.Path = layer.Options.Path, true
		}
		if !present.Dimension && layer.Present.Dimension {
			merged.Dimension, present.Dimension = layer.Options.Dimension, true
		}
		if !present.APIKeyEnv && layer.Present.APIKeyEnv {
			merged.APIKeyEnv, present.APIKeyEnv = layer.Options.APIKeyEnv, true
		}
		if !present.APIKey && layer.Present.APIKey {
			merged.APIKey, present.APIKey = layer.Options.APIKey, true
		}
		if !present.AuthHeader && layer.Present.AuthHeader {
			merged.AuthHeader, present.AuthHeader = layer.Options.AuthHeader, true
		}
		if !present.AuthScheme && layer.Present.AuthScheme {
			merged.AuthScheme, present.AuthScheme = layer.Options.AuthScheme, true
		}
		if !present.Headers && layer.Present.Headers {
			merged.Headers, present.Headers = layer.Options.Headers, true
		}
		if !present.QueryParams && layer.Present.QueryParams {
			merged.QueryParams, present.QueryParams = layer.Options.QueryParams, true
		}
		if !present.DocPrefix && layer.Present.DocPrefix {
			merged.DocPrefix, present.DocPrefix = layer.Options.DocPrefix, true
		}
		if !present.QueryPrefix && layer.Present.QueryPrefix {
			merged.QueryPrefix, present.QueryPrefix = layer.Options.QueryPrefix, true
		}
		if !present.BatchSize && layer.Present.BatchSize {
			merged.BatchSize, present.BatchSize = layer.Options.BatchSize, true
		}
		if !present.Timeout && layer.Present.Timeout {
			merged.Timeout, present.Timeout = layer.Options.Timeout, true
		}
		if !present.MaxRetries && layer.Present.MaxRetries {
			merged.MaxRetries, present.MaxRetries = layer.Options.MaxRetries, true
		}
		if !present.Skip && layer.Present.Skip {
			merged.Skip, present.Skip = layer.Options.Skip, true
		}
	}

	if merged.BatchSize == 0 {
		merged.BatchSize = DefaultBatchSize
	}
	if merged.Timeout == 0 {
		merged.Timeout = DefaultTimeout
	}
	if merged.MaxRetries == 0 {
		merged.MaxRetries = DefaultMaxRetries
	}
	if strings.TrimSpace(merged.Provider) == "" {
		merged.Provider = DefaultProvider
	}
	merged.Provider = strings.ToLower(strings.TrimSpace(merged.Provider))

	if source == "" {
		source = SourceDefault
	}
	merged.Source = source

	return merged
}
