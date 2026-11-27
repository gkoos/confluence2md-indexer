package indexerapi

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gkoos/confluence2md-indexer/internal/config"
	"github.com/gkoos/confluence2md-indexer/internal/embedding"
	"github.com/gkoos/confluence2md-indexer/internal/query"
)

type EmbeddingOptions = embedding.Options

// EmbeddingPresent marks which EmbeddingOptions fields a caller set explicitly, so an
// empty value can still override the environment and the configuration file.
type EmbeddingPresent = embedding.Present

// Settings is a loaded configuration file. A missing default file is a normal state
// and yields Settings with no values, so every accessor falls back to built-in defaults.
type Settings struct {
	// Path is the file the settings came from; empty when no file was found.
	Path string
	file config.File
}

// LoadSettings reads the configuration file the indexer CLI would read: path when
// given, otherwise the file named by CONFLUENCE2MD_CONFIG, otherwise config.yaml in
// the working directory. A path that is named but missing is an error.
func LoadSettings(path string) (*Settings, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}

	return &Settings{Path: cfg.Path, file: cfg.File}, nil
}

// DBPath returns the index path the file sets, or an empty string when it sets none.
func (s *Settings) DBPath() string {
	return s.file.DBPath()
}

// QueryDefaults returns a request holding the built-in retrieval defaults overlaid with
// the file's query section: mode, fusion, alpha, top-k, candidate-k, expand, priors and
// source weights. Text, filters and embedding settings are left empty; embedding
// settings come from ResolveEmbedding.
func (s *Settings) QueryDefaults() (QueryRequest, error) {
	req := QueryRequest{
		Mode:       query.DefaultMode,
		Fusion:     query.DefaultFusion,
		Alpha:      query.DefaultAlpha,
		RRFK:       query.DefaultRRFK,
		TopK:       query.DefaultTopK,
		CandidateK: query.DefaultCandidateK,
	}

	fileQuery := s.file.Query
	if fileQuery == nil {
		return req, nil
	}

	if fileQuery.Mode != nil {
		req.Mode = strings.TrimSpace(*fileQuery.Mode)
	}
	if fileQuery.Fusion != nil {
		req.Fusion = strings.TrimSpace(*fileQuery.Fusion)
	}
	if fileQuery.Alpha != nil {
		req.Alpha = *fileQuery.Alpha
	}
	if fileQuery.TopK != nil {
		req.TopK = *fileQuery.TopK
	}
	if fileQuery.CandidateK != nil {
		req.CandidateK = *fileQuery.CandidateK
	}
	if fileQuery.Expand != nil {
		req.Expand = *fileQuery.Expand
	}

	priors, err := query.ParsePriors(strings.Join(fileQuery.Priors, ","))
	if err != nil {
		return QueryRequest{}, fmt.Errorf("query.priors: %w", err)
	}
	req.Priors.Priors = priors

	if fileQuery.PriorStrength != nil {
		req.Priors.Strength = *fileQuery.PriorStrength
	}
	if fileQuery.RecencyHalfLife != nil {
		halfLife, err := query.ParseAge(*fileQuery.RecencyHalfLife)
		if err != nil {
			return QueryRequest{}, fmt.Errorf("query.recency_half_life: %w", err)
		}
		req.Priors.RecencyHalfLife = halfLife
	}

	weights, err := fileQuery.SourceWeightMap()
	if err != nil {
		return QueryRequest{}, fmt.Errorf("query.source_weights: %w", err)
	}
	req.SourceWeights = weights

	return req, nil
}

// ResolveEmbedding merges the embedding settings the way the CLI does: the explicit
// options (the "flag" layer, honouring present) beat CONFLUENCE2MD_EMBEDDING_*
// variables, which beat the file's embedding section, which beats built-in defaults.
// Assign the result to QueryRequest.Embedding.
func (s *Settings) ResolveEmbedding(explicit EmbeddingOptions, present EmbeddingPresent) (EmbeddingOptions, error) {
	envOptions, envPresent, err := embedding.ReadEnv()
	if err != nil {
		return EmbeddingOptions{}, err
	}

	merged := config.MergeEmbedding(
		embedding.Layer{Name: embedding.SourceFlag, Options: explicit, Present: present},
		embedding.Layer{Name: embedding.SourceEnv, Options: envOptions, Present: envPresent},
		s.file,
	)

	// The merge shares slices with the loaded file; clone them so a caller editing its
	// request cannot change what later calls on these Settings return.
	merged.Headers = slices.Clone(merged.Headers)
	merged.QueryParams = slices.Clone(merged.QueryParams)
	merged.Context = slices.Clone(merged.Context)

	return merged, nil
}
