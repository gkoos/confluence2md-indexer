package config

import "github.com/gkoos/confluence2md-indexer/internal/embedding"

// MergeEmbedding resolves the embedding options for one command run from the
// three configuration layers, highest precedence first: flags the user passed,
// values the environment supplies, then keys the file carries. Fields that no
// layer sets fall back to the built-in defaults, and the layer that selected the
// provider is recorded in Options.Source.
//
// Validation of the result belongs to the provider that consumes it, because a
// required field such as base_url only matters for the provider that was actually
// selected.
func MergeEmbedding(flagLayer, envLayer embedding.Layer, file File) embedding.Options {
	fileOptions, filePresent := file.EmbeddingOptions()
	return embedding.Merge(
		flagLayer,
		envLayer,
		embedding.Layer{Name: embedding.SourceConfig, Options: fileOptions, Present: filePresent},
	)
}
