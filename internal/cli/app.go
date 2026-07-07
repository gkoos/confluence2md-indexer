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

	"github.com/gkoos/confluence2md-indexer/internal/db"
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
	parsed, err := parseIndexArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "index: %v\n", err)
		return exitCodeInvalidUsage
	}

	dbPath, err := resolveDBPath(parsed.folder, parsed.dbPathFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "index: %v\n", err)
		return exitCodeInvalidUsage
	}

	ctx := context.Background()
	indexResp, err := service.Index(ctx, service.IndexRequest{
		Folder:  parsed.folder,
		DBPath:  dbPath,
		Rebuild: parsed.rebuild,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "index: %v\n", err)
		return exitCodeInvalidUsage
	}

	if parsed.jsonOutput {
		payload := map[string]any{
			"schemaVersion": outputSchemaV1,
			"command":       "index",
			"status":        indexResp.Status,
			"incremental":   indexResp.Incremental,
			"rebuild":       indexResp.Rebuild,
			"dbPath":        indexResp.DBPath,
			"embedding": map[string]any{
				"provider": indexResp.EmbeddingName,
				"source":   indexResp.EmbeddingSource,
				"written":  indexResp.EmbeddingWrites,
			},
			"inputFolder":  indexResp.InputFolder,
			"metadataPath": indexResp.MetadataPath,
			"pageCount":    indexResp.PageCount,
			"checkedFiles": indexResp.CheckedFiles,
			"documents": map[string]any{
				"inserted": indexResp.Inserted,
				"updated":  indexResp.Updated,
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
		fmt.Printf("schema version: %d\n", indexResp.DBStats.SchemaVersion)
		fmt.Printf("runs recorded: %d\n", indexResp.DBStats.Runs)
		fmt.Printf("documents inserted: %d\n", indexResp.Inserted)
		fmt.Printf("documents updated: %d\n", indexResp.Updated)
		fmt.Printf("documents skipped: %d\n", indexResp.Skipped)
		fmt.Printf("documents deleted: %d\n", indexResp.Deleted)
		fmt.Printf("chunks written: %d\n", indexResp.ChunkWrites)
		fmt.Printf("embeddings written: %d (%s; source=%s)\n", indexResp.EmbeddingWrites, indexResp.EmbeddingName, indexResp.EmbeddingSource)
		if parsed.rebuild {
			fmt.Println("mode: full rebuild")
		} else {
			fmt.Println("mode: incremental (default)")
		}
	}

	return exitCodeOK
}

type indexArgs struct {
	folder     string
	dbPathFlag string
	rebuild    bool
	jsonOutput bool
}

func parseIndexArgs(args []string) (*indexArgs, error) {
	parsed := &indexArgs{folder: "."}

	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}

		switch {
		case arg == "--rebuild":
			parsed.rebuild = true
		case arg == "--json":
			parsed.jsonOutput = true
		case arg == "--db":
			i++
			if i >= len(args) {
				return nil, fmt.Errorf("--db requires a value")
			}
			parsed.dbPathFlag = strings.TrimSpace(args[i])
			if parsed.dbPathFlag == "" {
				return nil, fmt.Errorf("--db requires a non-empty value")
			}
		case strings.HasPrefix(arg, "--db="):
			parsed.dbPathFlag = strings.TrimSpace(strings.TrimPrefix(arg, "--db="))
			if parsed.dbPathFlag == "" {
				return nil, fmt.Errorf("--db requires a non-empty value")
			}
		case strings.HasPrefix(arg, "-"):
			return nil, fmt.Errorf("unknown flag %s", arg)
		default:
			if parsed.folder != "." {
				return nil, errors.New("index accepts at most one folder argument")
			}
			parsed.folder = arg
		}
	}

	return parsed, nil
}

func (a *App) runQuery(args []string) int {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	dbPath := fs.String("db", defaultDBFileName, "Path to SQLite DB file")
	queryText := fs.String("q", "", "Query text")
	mode := fs.String("mode", "hybrid", "Retrieval mode: hybrid|lexical|vector")
	fusion := fs.String("fusion", "weighted", "Fusion mode: weighted|rrf")
	alpha := fs.Float64("alpha", 0.70, "Weighted fusion alpha (0..1)")
	rrfK := fs.Int("rrf-k", 60, "RRF k constant")
	topK := fs.Int("top-k", 10, "Final result count")
	offset := fs.Int("offset", 0, "Result offset within ranked list")
	limit := fs.Int("limit", 0, "Result limit after offset (defaults to --top-k)")
	candidateK := fs.Int("candidate-k", 50, "Candidate count per retrieval channel")
	space := fs.String("space", "", "Filter by space_key")
	pageID := fs.String("page-id", "", "Filter by page_id")
	fromDate := fs.String("from", "", "Lower bound for last_modified_at (YYYY-MM-DD)")
	toDate := fs.String("to", "", "Upper bound for last_modified_at (YYYY-MM-DD)")
	expand := fs.Int("expand", 0, "Adjacent chunk expansion count")
	jsonOutput := fs.Bool("json", false, "Emit machine-readable JSON results")
	explain := fs.Bool("explain", false, "Include score breakdown and diagnostics")

	if err := fs.Parse(args); err != nil {
		return exitCodeInvalidUsage
	}

	if *queryText == "" {
		fmt.Fprintln(os.Stderr, "query requires --q")
		return exitCodeInvalidUsage
	}
	if strings.TrimSpace(*dbPath) == "" {
		fmt.Fprintln(os.Stderr, "query requires a non-empty --db path")
		return exitCodeInvalidUsage
	}
	if !isOneOf(*mode, "hybrid", "lexical", "vector") {
		fmt.Fprintln(os.Stderr, "query --mode must be one of: hybrid, lexical, vector")
		return exitCodeInvalidUsage
	}
	if !isOneOf(*fusion, "weighted", "rrf") {
		fmt.Fprintln(os.Stderr, "query --fusion must be one of: weighted, rrf")
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
		Filters: db.SearchFilters{
			SpaceKey: *space,
			PageID:   *pageID,
			FromDate: *fromDate,
			ToDate:   *toDate,
		},
	}

	queryResp, err := service.Query(ctx, *dbPath, req)
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
			"dbPath":        *dbPath,
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

	dbPath := fs.String("db", defaultDBFileName, "Path to SQLite DB file")
	jsonOutput := fs.Bool("json", false, "Emit machine-readable JSON stats")

	if err := fs.Parse(args); err != nil {
		return exitCodeInvalidUsage
	}

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "stats does not accept positional arguments")
		return exitCodeInvalidUsage
	}
	if strings.TrimSpace(*dbPath) == "" {
		fmt.Fprintln(os.Stderr, "stats requires a non-empty --db path")
		return exitCodeInvalidUsage
	}

	ctx := context.Background()
	statsResp, err := service.Stats(ctx, *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stats: %v\n", err)
		return exitCodeInvalidUsage
	}
	stats := statsResp.Stats

	if *jsonOutput {
		payload := map[string]any{
			"schemaVersion": outputSchemaV1,
			"command":       "stats",
			"dbPath":        *dbPath,
			"stats":         stats,
		}
		b, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println(string(b))
		return exitCodeOK
	}

	fmt.Printf("db path: %s\n", *dbPath)
	fmt.Printf("schema version: %d\n", stats.SchemaVersion)
	fmt.Printf("runs: %d\n", stats.Runs)
	fmt.Printf("documents: %d\n", stats.Documents)
	fmt.Printf("chunks: %d\n", stats.Chunks)
	fmt.Printf("embeddings: %d\n", stats.Embeddings)

	return exitCodeOK
}

func (a *App) printUsage(out *os.File) {
	_, _ = fmt.Fprintln(out, "confluence2md-indexer - index and query confluence2md output")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Usage:")
	_, _ = fmt.Fprintln(out, "  confluence2md-indexer index [folder] [--db path] [--rebuild] [--json]")
	_, _ = fmt.Fprintln(out, "  confluence2md-indexer query --q text [--db path] [--mode hybrid|lexical|vector] [--fusion weighted|rrf] [--offset N] [--limit N] [--json] [--explain]")
	_, _ = fmt.Fprintln(out, "  confluence2md-indexer stats [--db path] [--json]")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Indexing defaults to incremental mode; use --rebuild for full rebuild.")
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

	sorted := append([]query.Result(nil), results...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Fused > sorted[j].Fused })
	if len(sorted) > 1 {
		gap := sorted[0].Fused - sorted[1].Fused
		lines = append(lines, fmt.Sprintf("top gap=%.4f", gap))
	}

	return lines
}

func resolveDBPath(folder string, dbPathFlag string) (string, error) {
	if strings.TrimSpace(dbPathFlag) != "" {
		return filepath.Abs(dbPathFlag)
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
