package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/gkoos/confluence2md-indexer/internal/config"
	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/embedding"
	"github.com/gkoos/confluence2md-indexer/internal/query"
	"github.com/gkoos/confluence2md-indexer/internal/service"
)

const (
	exitCodeOK           = 0
	exitCodeInvalidUsage = 2

	defaultDBFileName = "confluence2md-index.db"
	outputSchemaV1    = service.OutputSchemaVersion
)

type App struct{}

func NewApp() *App {
	return &App{}
}

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		a.printUsage(os.Stderr)
		return exitCodeInvalidUsage
	}

	switch args[0] {
	case "index":
		return a.runIndex(args[1:])
	case "query":
		return a.runQuery(args[1:])
	case "stats":
		return a.runStats(args[1:])
	case "help", "-h", "--help":
		a.printUsage(os.Stdout)
		return exitCodeOK
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", args[0])
		a.printUsage(os.Stderr)
		return exitCodeInvalidUsage
	}
}

func (a *App) runIndex(args []string) int {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	dbPathFlag := fs.String("db", "", "Path to the SQLite database file (defaults to the input folder)")
	configPath := registerConfigFlag(fs)
	rebuild := fs.Bool("rebuild", false, "Recreate the database file before indexing")
	jsonOutput := fs.Bool("json", false, "Emit machine-readable JSON output")
	skipEmbeddings := fs.Bool("skip-embeddings", false, "Store no embeddings and leave the vector channel empty")
	embeddingValues := &embeddingFlags{}
	registerEmbeddingFlags(fs, embeddingValues)

	folders, err := parseInterspersed(fs, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "index: %v\n", err)
		return exitCodeInvalidUsage
	}
	if len(folders) > 1 {
		fmt.Fprintln(os.Stderr, "index accepts at most one folder argument")
		return exitCodeInvalidUsage
	}

	folder := "."
	if len(folders) == 1 {
		folder = folders[0]
	}

	flagOptions, err := embeddingValues.options()
	if err != nil {
		fmt.Fprintf(os.Stderr, "index: %v\n", err)
		return exitCodeInvalidUsage
	}
	if isProviderListRequest(flagOptions.Provider) {
		printProviderList(os.Stdout)
		return exitCodeOK
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "index: %v\n", err)
		return exitCodeInvalidUsage
	}
	options, err := resolveEmbeddingOptions(flagOptions, embeddingValues.present(fs), cfg.File)
	if err != nil {
		fmt.Fprintf(os.Stderr, "index: %v\n", err)
		return exitCodeInvalidUsage
	}
	options.Skip = options.Skip || *skipEmbeddings

	dbPath, err := resolveDBPath(folder, *dbPathFlag, cfg.File.DBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "index: %v\n", err)
		return exitCodeInvalidUsage
	}

	ctx := context.Background()
	indexResp, err := service.Index(ctx, service.IndexRequest{
		Folder:    folder,
		DBPath:    dbPath,
		Rebuild:   *rebuild,
		Embedding: options,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "index: %v\n", err)
		return exitCodeInvalidUsage
	}

	if *jsonOutput {
		payload := map[string]any{
			"schemaVersion": outputSchemaV1,
			"command":       "index",
			"status":        indexResp.Status,
			"incremental":   indexResp.Incremental,
			"rebuild":       indexResp.Rebuild,
			"dbPath":        indexResp.DBPath,
			"embedding": map[string]any{
				"provider":   indexResp.EmbeddingName,
				"source":     indexResp.EmbeddingSource,
				"written":    indexResp.EmbeddingWrites,
				"pruned":     indexResp.EmbeddingPruned,
				"dimension":  indexResp.EmbeddingDimension,
				"capability": indexResp.EmbeddingCapability,
			},
			"inputFolder":  indexResp.InputFolder,
			"metadataPath": indexResp.MetadataPath,
			"pageCount":    indexResp.PageCount,
			"checkedFiles": indexResp.CheckedFiles,
			"documents": map[string]any{
				"inserted": indexResp.Inserted,
				"updated":  indexResp.Updated,
				"metadata": indexResp.MetadataUpdates,
				"skipped":  indexResp.Skipped,
				"deleted":  indexResp.Deleted,
			},
			"chunkWrites": indexResp.ChunkWrites,
			"runId":       indexResp.RunID,
			"dbStats":     indexResp.DBStats,
		}
		b, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("index preflight passed: %d pages, %d markdown files validated\n", indexResp.PageCount, indexResp.CheckedFiles)
		fmt.Printf("db path: %s\n", indexResp.DBPath)
		fmt.Printf("run id: %s\n", indexResp.RunID)
		fmt.Printf("runs recorded: %d\n", indexResp.DBStats.Runs)
		fmt.Printf("documents inserted: %d\n", indexResp.Inserted)
		fmt.Printf("documents updated: %d\n", indexResp.Updated)
		fmt.Printf("documents skipped: %d\n", indexResp.Skipped)
		fmt.Printf("documents deleted: %d\n", indexResp.Deleted)
		fmt.Printf("chunks written: %d\n", indexResp.ChunkWrites)
		fmt.Printf("embeddings written: %d (%s; source=%s, capability=%s, dim=%d)\n",
			indexResp.EmbeddingWrites, indexResp.EmbeddingName, indexResp.EmbeddingSource,
			indexResp.EmbeddingCapability, indexResp.EmbeddingDimension)
		if indexResp.EmbeddingPruned != 0 {
			fmt.Printf("embeddings pruned: %d\n", indexResp.EmbeddingPruned)
		}
		if indexResp.MetadataUpdates != 0 {
			fmt.Printf("metadata refreshed: %d\n", indexResp.MetadataUpdates)
		}
		if *rebuild {
			fmt.Println("mode: full rebuild")
		} else {
			fmt.Println("mode: incremental (default)")
		}
	}

	return exitCodeOK
}

// embeddingFlags collects the shared embedding flag surface. Every flag maps to
// one embedding.Options field; environment variables and defaults are applied by
// embedding.Resolve.
type embeddingFlags struct {
	provider    string
	model       string
	baseURL     string
	path        string
	dimension   int
	apiKeyEnv   string
	authHeader  string
	authScheme  string
	headers     stringListFlag
	queryParams stringListFlag
	docPrefix   string
	queryPrefix string
	batchSize   int
	timeout     time.Duration
	maxRetries  int
}

// stringListFlag collects repeatable flag values.
type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ";") }

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func registerEmbeddingFlags(fs *flag.FlagSet, values *embeddingFlags) {
	fs.StringVar(&values.provider, "embedding", "", "Embedding provider id; use \"list\" to enumerate providers")
	fs.StringVar(&values.model, "embedding-model", "", "Provider model name")
	fs.StringVar(&values.baseURL, "embedding-base-url", "", "Base URL for HTTP providers")
	fs.StringVar(&values.path, "embedding-path", "", "Embeddings path appended to the base URL")
	fs.IntVar(&values.dimension, "embedding-dim", 0, "Vector dimension when the model is not known locally")
	fs.StringVar(&values.apiKeyEnv, "embedding-api-key-env", "", "Name of the environment variable holding the API key")
	fs.StringVar(&values.authHeader, "embedding-auth-header", "", "Authentication header name (default Authorization)")
	fs.StringVar(&values.authScheme, "embedding-auth-scheme", "", "Authentication scheme prefix (default Bearer; use \"none\" for a raw key)")
	fs.Var(&values.headers, "embedding-header", "Extra request header in key=value form; repeatable")
	fs.Var(&values.queryParams, "embedding-query-param", "Extra query parameter in key=value form; repeatable")
	fs.StringVar(&values.docPrefix, "embedding-document-prefix", "", "Text prefix applied to indexed text")
	fs.StringVar(&values.queryPrefix, "embedding-query-prefix", "", "Text prefix applied to query text")
	fs.IntVar(&values.batchSize, "embedding-batch-size", 0, "Inputs per embedding request")
	fs.DurationVar(&values.timeout, "embedding-timeout", 0, "Per-request timeout, for example 30s")
	fs.IntVar(&values.maxRetries, "embedding-max-retries", 0, "Retries after the first attempt; -1 disables retries")
}

// options converts parsed flags into embedding options.
func (values *embeddingFlags) options() (embedding.Options, error) {
	switch {
	case values.dimension < 0:
		return embedding.Options{}, errors.New("--embedding-dim must not be negative")
	case values.batchSize < 0:
		return embedding.Options{}, errors.New("--embedding-batch-size must not be negative")
	case values.timeout < 0:
		return embedding.Options{}, errors.New("--embedding-timeout must not be negative")
	case values.maxRetries < -1:
		return embedding.Options{}, errors.New("--embedding-max-retries must be -1, 0 or greater")
	}

	return embedding.Options{
		Provider:    values.provider,
		Model:       values.model,
		BaseURL:     values.baseURL,
		Path:        values.path,
		Dimension:   values.dimension,
		APIKeyEnv:   values.apiKeyEnv,
		AuthHeader:  values.authHeader,
		AuthScheme:  values.authScheme,
		Headers:     values.headers,
		QueryParams: values.queryParams,
		DocPrefix:   values.docPrefix,
		QueryPrefix: values.queryPrefix,
		BatchSize:   values.batchSize,
		Timeout:     values.timeout,
		MaxRetries:  values.maxRetries,
	}, nil
}

// present reports which embedding flags the user passed. Presence, not value,
// decides precedence: a flag that sets an empty value still overrides the
// environment and the configuration file.
func (values *embeddingFlags) present(fs *flag.FlagSet) embedding.Present {
	visited := flagNamesVisited(fs)

	var present embedding.Present
	present.Provider = visited["embedding"]
	present.Model = visited["embedding-model"]
	present.BaseURL = visited["embedding-base-url"]
	present.Path = visited["embedding-path"]
	present.Dimension = visited["embedding-dim"]
	present.APIKeyEnv = visited["embedding-api-key-env"]
	present.AuthHeader = visited["embedding-auth-header"]
	present.AuthScheme = visited["embedding-auth-scheme"]
	present.Headers = visited["embedding-header"]
	present.QueryParams = visited["embedding-query-param"]
	present.DocPrefix = visited["embedding-document-prefix"]
	present.QueryPrefix = visited["embedding-query-prefix"]
	present.BatchSize = visited["embedding-batch-size"]
	present.Timeout = visited["embedding-timeout"]
	present.MaxRetries = visited["embedding-max-retries"]

	return present
}

// flagNamesVisited returns the names of the flags the user passed, which is how a
// value that is explicitly empty stays distinguishable from a default.
func flagNamesVisited(fs *flag.FlagSet) map[string]bool {
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		visited[f.Name] = true
	})
	return visited
}

// registerConfigFlag declares --config on a subcommand. A missing default
// config.yaml is not an error, because the tool runs without a file.
func registerConfigFlag(fs *flag.FlagSet) *string {
	return fs.String(
		"config",
		"",
		fmt.Sprintf("Path to a YAML configuration file (defaults to %s when present)", config.DefaultFileName),
	)
}

// resolveEmbeddingOptions applies the embedding configuration layers in order:
// flags the user passed, environment variables that are set, keys the
// configuration file carries, then built-in defaults.
func resolveEmbeddingOptions(flagOptions embedding.Options, flagPresent embedding.Present, file config.File) (embedding.Options, error) {
	envOptions, envPresent, err := embedding.ReadEnv()
	if err != nil {
		return embedding.Options{}, err
	}

	return config.MergeEmbedding(
		embedding.Layer{Name: embedding.SourceFlag, Options: flagOptions, Present: flagPresent},
		embedding.Layer{Name: embedding.SourceEnv, Options: envOptions, Present: envPresent},
		file,
	), nil
}

// resolveQueryDBPath picks the database for query and stats: --db when the user
// passed it, then db.path from the configuration file, then the default name in
// the working directory.
func resolveQueryDBPath(dbPathFlag string, visited map[string]bool, file config.File) string {
	if visited["db"] {
		return dbPathFlag
	}
	if configured := file.DBPath(); configured != "" {
		return configured
	}
	return dbPathFlag
}

// parseInterspersed parses flags that may appear before or after positional
// arguments. The standard flag package stops at the first positional argument,
// which would reject the documented "index <folder> --rebuild --json" form.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	positional := make([]string, 0, 1)
	remaining := args

	for {
		if err := fs.Parse(remaining); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		remaining = rest[1:]
	}
}

// isProviderListRequest reports whether the caller asked to enumerate providers
// using the "list" pseudo-provider.
func isProviderListRequest(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "list")
}

func printProviderList(out *os.File) {
	_, _ = fmt.Fprintf(out, "available embedding providers: %s\n", strings.Join(embedding.Available(), ", "))
}

func (a *App) runQuery(args []string) int {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	dbPathFlag := fs.String("db", defaultDBFileName, "Path to SQLite DB file")
	configPath := registerConfigFlag(fs)
	queryText := fs.String("q", "", "Query text")
	mode := fs.String("mode", "hybrid", "Retrieval mode: hybrid|lexical|vector")
	fusion := fs.String("fusion", "weighted", "Fusion mode: weighted|rrf")
	alpha := fs.Float64("alpha", 0.70, "Weighted fusion alpha (0..1)")
	rrfK := fs.Int("rrf-k", 60, "RRF k constant")
	topK := fs.Int("top-k", 10, "Final result count")
	offset := fs.Int("offset", 0, "Result offset within ranked list")
	limit := fs.Int("limit", 0, "Result limit after offset (defaults to --top-k)")
	candidateK := fs.Int("candidate-k", 50, "Candidate count per retrieval channel")
	spaceList := &stringListFlag{}
	fs.Var(spaceList, "space", "Filter by space_key; repeatable")
	author := fs.String("author", "", "Filter by creator or last modifier name")
	createdBy := fs.String("created-by", "", "Filter by creator name")
	modifiedBy := fs.String("modified-by", "", "Filter by last modifier name")
	host := fs.String("host", "", "Filter by crawled host")
	depthMin := fs.Int("depth-min", -1, "Minimum crawl depth; 1 excludes seeds")
	depthMax := fs.Int("depth-max", -1, "Maximum crawl depth")
	seedOnly := fs.Bool("seed-only", false, "Keep only the pages the crawl started from")
	hasAttachments := fs.Bool("has-attachments", false, "Keep only pages that carry an attachment")
	updatedSince := fs.String("updated-since", "", "Keep pages modified within an age such as 30d, 2w or 12h")
	priors := fs.String("priors", "", "Metadata ranking priors, comma separated: recency, authority, seed, depth, richness")
	priorStrength := fs.Float64("prior-strength", 0, "Maximum relative score adjustment from priors (0..1; unset means the default)")
	recencyHalfLife := fs.String("recency-half-life", "", "Age at which the recency prior falls to half, for example 180d or 6w")
	pageID := fs.String("page-id", "", "Filter by page_id")
	fromDate := fs.String("from", "", "Lower bound for last_modified_at (YYYY-MM-DD)")
	toDate := fs.String("to", "", "Upper bound for last_modified_at (YYYY-MM-DD)")
	expand := fs.Int("expand", 0, "Adjacent chunk expansion count")
	jsonOutput := fs.Bool("json", false, "Emit machine-readable JSON results")
	explain := fs.Bool("explain", false, "Include score breakdown and diagnostics")
	lexicalOnly := fs.Bool("lexical-only", false, "Force lexical retrieval, requiring no embedding provider")
	embeddingValues := &embeddingFlags{}
	registerEmbeddingFlags(fs, embeddingValues)

	if err := fs.Parse(args); err != nil {
		return exitCodeInvalidUsage
	}

	visited := flagNamesVisited(fs)
	modeSet := visited["mode"]
	if *lexicalOnly {
		if modeSet && *mode != "lexical" {
			fmt.Fprintf(os.Stderr, "query --lexical-only conflicts with --mode %s\n", *mode)
			return exitCodeInvalidUsage
		}
		*mode = "lexical"
		// The flag decided the mode, so it must not be overridden by a file default.
		visited["mode"] = true
	}

	flagOptions, err := embeddingValues.options()
	if err != nil {
		fmt.Fprintf(os.Stderr, "query: %v\n", err)
		return exitCodeInvalidUsage
	}
	if isProviderListRequest(flagOptions.Provider) {
		printProviderList(os.Stdout)
		return exitCodeOK
	}

	if *queryText == "" {
		fmt.Fprintln(os.Stderr, "query requires --q")
		return exitCodeInvalidUsage
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query: %v\n", err)
		return exitCodeInvalidUsage
	}

	// The file supplies retrieval defaults for whatever the command line left alone. A
	// flag that was passed always wins, an empty one included, which is how a single
	// invocation overrides or clears a configured default.
	if fileQuery := cfg.File.Query; fileQuery != nil {
		if !visited["mode"] && fileQuery.Mode != nil {
			*mode = *fileQuery.Mode
		}
		if !visited["fusion"] && fileQuery.Fusion != nil {
			*fusion = *fileQuery.Fusion
		}
		if !visited["alpha"] && fileQuery.Alpha != nil {
			*alpha = *fileQuery.Alpha
		}
		if !visited["top-k"] && fileQuery.TopK != nil {
			*topK = *fileQuery.TopK
		}
		if !visited["candidate-k"] && fileQuery.CandidateK != nil {
			*candidateK = *fileQuery.CandidateK
		}
		if !visited["expand"] && fileQuery.Expand != nil {
			*expand = *fileQuery.Expand
		}
		if !visited["prior-strength"] && fileQuery.PriorStrength != nil {
			*priorStrength = *fileQuery.PriorStrength
		}
		if !visited["recency-half-life"] && fileQuery.RecencyHalfLife != nil {
			// The flag and the file share one age parser, so the text carries over.
			*recencyHalfLife = *fileQuery.RecencyHalfLife
		}
		if !visited["priors"] && fileQuery.Priors != nil {
			*priors = strings.Join(fileQuery.Priors, ",")
		}
	}

	embeddingOptions, err := resolveEmbeddingOptions(flagOptions, embeddingValues.present(fs), cfg.File)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query: %v\n", err)
		return exitCodeInvalidUsage
	}
	dbPath := resolveQueryDBPath(*dbPathFlag, visited, cfg.File)
	if strings.TrimSpace(dbPath) == "" {
		fmt.Fprintln(os.Stderr, "query requires a non-empty --db path")
		return exitCodeInvalidUsage
	}
	if !isOneOf(*mode, query.Modes...) {
		fmt.Fprintf(os.Stderr, "query --mode must be one of: %s\n", strings.Join(query.Modes, ", "))
		return exitCodeInvalidUsage
	}
	if !isOneOf(*fusion, query.Fusions...) {
		fmt.Fprintf(os.Stderr, "query --fusion must be one of: %s\n", strings.Join(query.Fusions, ", "))
		return exitCodeInvalidUsage
	}
	if *alpha < 0 || *alpha > 1 {
		fmt.Fprintln(os.Stderr, "query --alpha must be between 0 and 1")
		return exitCodeInvalidUsage
	}
	if *rrfK <= 0 {
		fmt.Fprintln(os.Stderr, "query --rrf-k must be > 0")
		return exitCodeInvalidUsage
	}
	if *topK <= 0 {
		fmt.Fprintln(os.Stderr, "query --top-k must be > 0")
		return exitCodeInvalidUsage
	}
	if *offset < 0 {
		fmt.Fprintln(os.Stderr, "query --offset must be >= 0")
		return exitCodeInvalidUsage
	}
	if *limit < 0 {
		fmt.Fprintln(os.Stderr, "query --limit must be >= 0")
		return exitCodeInvalidUsage
	}
	if *candidateK <= 0 {
		fmt.Fprintln(os.Stderr, "query --candidate-k must be > 0")
		return exitCodeInvalidUsage
	}
	if *expand < 0 {
		fmt.Fprintln(os.Stderr, "query --expand must be >= 0")
		return exitCodeInvalidUsage
	}
	if *depthMin < -1 || *depthMax < -1 {
		fmt.Fprintln(os.Stderr, "query --depth-min and --depth-max must be >= 0")
		return exitCodeInvalidUsage
	}
	if *depthMin >= 0 && *depthMax >= 0 && *depthMin > *depthMax {
		fmt.Fprintln(os.Stderr, "query --depth-min must not be greater than --depth-max")
		return exitCodeInvalidUsage
	}
	updatedSinceValue, err := parseUpdatedSince(*updatedSince, time.Now().UTC())
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return exitCodeInvalidUsage
	}
	priorList, err := query.ParsePriors(*priors)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query: %v\n", err)
		return exitCodeInvalidUsage
	}
	if *priorStrength < 0 || *priorStrength > 1 {
		fmt.Fprintln(os.Stderr, "query --prior-strength must be between 0 and 1")
		return exitCodeInvalidUsage
	}
	halfLife, err := query.ParseAge(*recencyHalfLife)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query --recency-half-life %v\n", err)
		return exitCodeInvalidUsage
	}
	if halfLife < 0 {
		fmt.Fprintln(os.Stderr, "query --recency-half-life must not be negative")
		return exitCodeInvalidUsage
	}
	if _, err := parseOptionalDate("--from", *fromDate); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return exitCodeInvalidUsage
	}
	if _, err := parseOptionalDate("--to", *toDate); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return exitCodeInvalidUsage
	}
	fromParsed, _ := parseOptionalDate("--from", *fromDate)
	toParsed, _ := parseOptionalDate("--to", *toDate)
	if !fromParsed.IsZero() && !toParsed.IsZero() && fromParsed.After(toParsed) {
		fmt.Fprintln(os.Stderr, "query date range invalid: --from must be <= --to")
		return exitCodeInvalidUsage
	}

	ctx := context.Background()
	req := query.Request{
		Text:       *queryText,
		Mode:       *mode,
		Fusion:     *fusion,
		Alpha:      *alpha,
		RRFK:       *rrfK,
		TopK:       *topK,
		Offset:     *offset,
		Limit:      *limit,
		CandidateK: *candidateK,
		Expand:     *expand,
		Embedding:  embeddingOptions,
		Filters: db.SearchFilters{
			SpaceKey:       firstSpace(*spaceList),
			Spaces:         []string(*spaceList),
			PageID:         *pageID,
			FromDate:       *fromDate,
			ToDate:         *toDate,
			Host:           *host,
			Author:         *author,
			CreatedBy:      *createdBy,
			ModifiedBy:     *modifiedBy,
			DepthMin:       optionalDepth(*depthMin),
			DepthMax:       optionalDepth(*depthMax),
			SeedOnly:       *seedOnly,
			HasAttachments: *hasAttachments,
			UpdatedSince:   updatedSinceValue,
		},
		Priors: query.PriorConfig{
			Priors:          priorList,
			Strength:        *priorStrength,
			RecencyHalfLife: halfLife,
			Now:             time.Now().UTC(),
		},
	}

	queryResp, err := service.Query(ctx, dbPath, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query: %v\n", err)
		return exitCodeInvalidUsage
	}
	results := queryResp.Results
	total := queryResp.Total

	if *jsonOutput {
		payload := map[string]any{
			"schemaVersion": outputSchemaV1,
			"command":       "query",
			"dbPath":        dbPath,
			"request":       req,
			"count":         len(results),
			"total":         total,
			"results":       results,
		}
		payload["pagination"] = map[string]any{
			"offset": req.Offset,
			"limit":  req.Limit,
		}
		if *explain {
			payload["explain"] = buildExplainSummary(results, req)
		}
		b, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println(string(b))
		return exitCodeOK
	}

	if len(results) == 0 {
		fmt.Println("no results")
		return exitCodeOK
	}

	for _, res := range results {
		fmt.Printf("%d. [%s] %s (chunk %d)\n", res.Rank, res.PageID, res.Title, res.ChunkIndex)
		fmt.Printf("   score=%.4f lexical=%.4f vector=%.4f fusion=%s\n", res.Fused, res.Lexical, res.Vector, res.Fusion)
		if res.ContextChunkCount > 0 {
			fmt.Printf("   context-range=%d..%d (%d chunks)\n", res.ContextStartIndex, res.ContextEndIndex, res.ContextChunkCount)
		}
		fmt.Printf("   path=%s\n", res.LocalPath)
		fmt.Printf("   text=%s\n", summarizeText(res.ChunkText, 240))
	}

	if *explain {
		fmt.Println()
		fmt.Println("explain:")
		for _, line := range buildExplainSummary(results, req) {
			fmt.Printf("- %s\n", line)
		}
	}

	return exitCodeOK
}

func (a *App) runStats(args []string) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	dbPathFlag := fs.String("db", defaultDBFileName, "Path to SQLite DB file")
	configPath := registerConfigFlag(fs)
	jsonOutput := fs.Bool("json", false, "Emit machine-readable JSON stats")

	if err := fs.Parse(args); err != nil {
		return exitCodeInvalidUsage
	}

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "stats does not accept positional arguments")
		return exitCodeInvalidUsage
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stats: %v\n", err)
		return exitCodeInvalidUsage
	}
	dbPath := resolveQueryDBPath(*dbPathFlag, flagNamesVisited(fs), cfg.File)
	if strings.TrimSpace(dbPath) == "" {
		fmt.Fprintln(os.Stderr, "stats requires a non-empty --db path")
		return exitCodeInvalidUsage
	}

	ctx := context.Background()
	statsResp, err := service.Stats(ctx, dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stats: %v\n", err)
		return exitCodeInvalidUsage
	}
	stats := statsResp.Stats

	if *jsonOutput {
		payload := map[string]any{
			"schemaVersion": outputSchemaV1,
			"command":       "stats",
			"dbPath":        dbPath,
			"stats":         stats,
		}
		b, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println(string(b))
		return exitCodeOK
	}

	fmt.Printf("db path: %s\n", dbPath)
	fmt.Printf("runs: %d\n", stats.Runs)
	fmt.Printf("documents: %d\n", stats.Documents)
	fmt.Printf("chunks: %d\n", stats.Chunks)
	fmt.Printf("embeddings: %d\n", stats.Embeddings)
	fmt.Printf("vector ready: %t\n", stats.VectorReady)
	if stats.VectorName != "" {
		fmt.Printf("embedding identity: %s\n", stats.VectorName)
	}
	if stats.VectorCapability != "" {
		fmt.Printf("vector capability: %s\n", stats.VectorCapability)
	}
	if stats.Metadata != nil {
		fmt.Printf(
			"metadata: authors=%d links=%d attachments=%d comments=%d seeds=%d nested=%d hosts=%d\n",
			stats.Metadata.WithAuthors,
			stats.Metadata.WithLinks,
			stats.Metadata.WithAttachments,
			stats.Metadata.WithComments,
			stats.Metadata.Seeds,
			stats.Metadata.Nested,
			stats.Metadata.Hosts,
		)
	}
	if stats.Corpus != nil {
		parts := make([]string, 0, 6)
		if stats.Corpus.Mode != "" {
			parts = append(parts, "mode="+stats.Corpus.Mode)
		}
		parts = append(parts,
			fmt.Sprintf("pages=%d", stats.Corpus.PageCount),
			fmt.Sprintf("seeds=%d", stats.Corpus.SeedCount),
		)
		if stats.Corpus.IndexedAt != "" {
			parts = append(parts, "indexed "+stats.Corpus.IndexedAt)
		}
		if stats.Corpus.CompletedAt != "" {
			parts = append(parts, "crawl completed "+stats.Corpus.CompletedAt)
		}
		if stats.Corpus.SucceededAt != "" {
			parts = append(parts, "last successful crawl "+stats.Corpus.SucceededAt)
		}
		fmt.Printf("corpus: %s\n", strings.Join(parts, " "))
	}

	return exitCodeOK
}

func (a *App) printUsage(out *os.File) {
	_, _ = fmt.Fprintln(out, "confluence2md-indexer - index and query confluence2md output")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Usage:")
	_, _ = fmt.Fprintln(out, "  confluence2md-indexer index [folder] [--db path] [--config file] [--rebuild] [--json] [--skip-embeddings]")
	_, _ = fmt.Fprintln(out, "  confluence2md-indexer query --q text [--db path] [--config file] [--mode hybrid|lexical|vector] [--fusion weighted|rrf] [--offset N] [--limit N] [--json] [--explain] [--lexical-only]")
	_, _ = fmt.Fprintln(out, "  confluence2md-indexer stats [--db path] [--config file] [--json]")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Metadata filters, accepted by query (metadata comes from the crawler output):")
	_, _ = fmt.Fprintln(out, "  --space <key>                space_key, repeatable")
	_, _ = fmt.Fprintln(out, "  --page-id <id>               single page")
	_, _ = fmt.Fprintln(out, "  --host <host>                crawled host, for corpora that span sites")
	_, _ = fmt.Fprintln(out, "  --author <name>              creator or last modifier, case-insensitive")
	_, _ = fmt.Fprintln(out, "  --created-by <name>          creator only           --modified-by <name>  last modifier only")
	_, _ = fmt.Fprintln(out, "  --depth-min N --depth-max N  crawl depth range; 1 excludes seed pages")
	_, _ = fmt.Fprintln(out, "  --seed-only                  only the pages the crawl started from")
	_, _ = fmt.Fprintln(out, "  --has-attachments            only pages that carry an attachment")
	_, _ = fmt.Fprintln(out, "  --from/--to YYYY-MM-DD       last_modified_at bounds   --updated-since 30d|2w|12h")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Metadata ranking priors, off unless requested (--priors recency,authority,seed,depth,richness):")
	_, _ = fmt.Fprintln(out, "  --priors <list>              recency, authority, seed, depth, richness")
	_, _ = fmt.Fprintln(out, "  --prior-strength <0..1>      largest relative score adjustment (default 0.15)")
	_, _ = fmt.Fprintln(out, "  --recency-half-life <age>    age at which recency halves (default 180d)")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "A query section in the configuration file can supply defaults for mode, fusion,")
	_, _ = fmt.Fprintln(out, "alpha, top_k, candidate_k, expand and the priors; a flag you pass still wins.")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Indexing defaults to incremental mode; use --rebuild for full rebuild.")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Embedding flags, accepted by index and query:")
	_, _ = fmt.Fprintln(out, "  --embedding <id>             provider id, or \"list\" to enumerate providers")
	_, _ = fmt.Fprintln(out, "  --embedding-model <name>     provider model name")
	_, _ = fmt.Fprintln(out, "  --embedding-base-url <url>   base URL for HTTP providers")
	_, _ = fmt.Fprintln(out, "  --embedding-path <path>      embeddings path appended to the base URL")
	_, _ = fmt.Fprintln(out, "  --embedding-dim <n>          vector size when the model is not known locally")
	_, _ = fmt.Fprintln(out, "  --embedding-api-key-env <V>  name of the variable holding the API key")
	_, _ = fmt.Fprintln(out, "  --embedding-header k=v       extra request header (repeatable)")
	_, _ = fmt.Fprintln(out, "  --embedding-query-param k=v  extra query parameter (repeatable)")
	_, _ = fmt.Fprintln(out, "  --embedding-document-prefix <text> / --embedding-query-prefix <text>")
	_, _ = fmt.Fprintln(out, "  --embedding-batch-size <n>   --embedding-timeout <dur>   --embedding-max-retries <n>")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Query and index must use the same embedding identity; a mismatch is reported")
	_, _ = fmt.Fprintln(out, "instead of returning empty results.")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Configuration file, accepted by every subcommand:")
	_, _ = fmt.Fprintln(out, "  --config <file>              YAML file with db and embedding sections;")
	_, _ = fmt.Fprintln(out, "                               defaults to config.yaml when it exists")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "A setting resolves from the flag you pass, then the CONFLUENCE2MD_EMBEDDING_*")
	_, _ = fmt.Fprintln(out, "environment variable, then the configuration file, then the built-in default.")
}

// firstSpace returns the first value that carries something other than whitespace. It
// keeps the single-valued SpaceKey field populated for callers that still read it
// while Spaces carries the whole list.
func firstSpace(values []string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

// optionalDepth turns the -1 sentinel of the depth flags into "unbounded".
func optionalDepth(value int) *int {
	if value < 0 {
		return nil
	}

	return &value
}

// parseUpdatedSince turns a relative age into the absolute lower bound the SQL
// comparison needs.
func parseUpdatedSince(value string, now time.Time) (string, error) {
	age, err := query.ParseAge(value)
	if err != nil {
		return "", fmt.Errorf("query --updated-since %w", err)
	}
	if age == 0 {
		return "", nil
	}

	return now.Add(-age).Format(time.RFC3339), nil
}

// formatPriorFactors renders prior values in a stable order, so explain output does
// not depend on map iteration order.
func formatPriorFactors(factors map[string]float64) string {
	names := make([]string, 0, len(factors))
	for name := range factors {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s=%.2f", name, factors[name]))
	}

	return strings.Join(parts, ",")
}

func summarizeText(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if max <= 3 || len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func buildExplainSummary(results []query.Result, req query.Request) []string {
	effectiveFusion := req.Fusion
	switch req.Mode {
	case "lexical":
		effectiveFusion = "lexical"
	case "vector":
		effectiveFusion = "vector"
	}

	lines := []string{
		fmt.Sprintf("mode=%s", req.Mode),
		fmt.Sprintf("fusion=%s", effectiveFusion),
		fmt.Sprintf("alpha=%.2f", req.Alpha),
		fmt.Sprintf("rrf-k=%d", req.RRFK),
		fmt.Sprintf("expand=%d", req.Expand),
		fmt.Sprintf("returned=%d", len(results)),
	}
	if len(results) == 0 {
		return lines
	}

	best := results[0]
	weightedLex := req.Alpha * best.Lexical
	weightedVec := (1 - req.Alpha) * best.Vector
	lines = append(lines, fmt.Sprintf("top chunk=%s fused=%.4f lexical=%.4f vector=%.4f", best.ChunkID, best.Fused, best.Lexical, best.Vector))
	if effectiveFusion == "weighted" {
		lines = append(lines, fmt.Sprintf("top weighted-components lex=%.4f vec=%.4f", weightedLex, weightedVec))
	}
	if best.ContextChunkCount > 0 {
		lines = append(lines,
			fmt.Sprintf("top context-range=%d..%d (%d chunks)", best.ContextStartIndex, best.ContextEndIndex, best.ContextChunkCount),
		)
	}

	if req.Priors.Enabled() {
		resolved := req.Priors.Resolved()
		names := make([]string, 0, len(resolved.Priors))
		for _, prior := range resolved.Priors {
			names = append(names, string(prior))
		}
		lines = append(lines, fmt.Sprintf(
			"priors=%s strength=%.2f recency-half-life=%s",
			strings.Join(names, ","), resolved.Strength, resolved.RecencyHalfLife,
		))
		if len(best.MetadataFactors) > 0 {
			lines = append(lines, "top metadata factors="+formatPriorFactors(best.MetadataFactors))
		}
		if best.MetadataBoost != 0 {
			lines = append(lines, fmt.Sprintf("top metadata boost=+%.4f", best.MetadataBoost))
		}
	}

	sorted := append([]query.Result(nil), results...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Fused > sorted[j].Fused })
	if len(sorted) > 1 {
		gap := sorted[0].Fused - sorted[1].Fused
		lines = append(lines, fmt.Sprintf("top gap=%.4f", gap))
	}

	return lines
}

// resolveDBPath picks the index location: --db when the user passed it, then
// db.path from the configuration file, then the default file inside the indexed
// folder.
func resolveDBPath(folder string, dbPathFlag string, configuredPath string) (string, error) {
	switch {
	case strings.TrimSpace(dbPathFlag) != "":
		return filepath.Abs(dbPathFlag)
	case strings.TrimSpace(configuredPath) != "":
		return filepath.Abs(configuredPath)
	}

	folderAbs, err := filepath.Abs(folder)
	if err != nil {
		return "", fmt.Errorf("resolve folder %q: %w", folder, err)
	}
	return filepath.Join(folderAbs, defaultDBFileName), nil
}

func parseOptionalDate(flagName, value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("query %s must be YYYY-MM-DD", flagName)
	}
	return t, nil
}

func isOneOf(value string, allowed ...string) bool {
	return slices.Contains(allowed, value)
}
