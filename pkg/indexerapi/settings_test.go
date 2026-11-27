package indexerapi

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gkoos/confluence2md-indexer/internal/query"
)

func writeSettings(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadSettingsWithoutFileKeepsBuiltInDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CONFLUENCE2MD_CONFIG", "")

	settings, err := LoadSettings("")
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.Path != "" || settings.DBPath() != "" {
		t.Fatalf("no file must mean no path and no db: %+v", settings)
	}

	req, err := settings.QueryDefaults()
	if err != nil {
		t.Fatalf("QueryDefaults: %v", err)
	}
	want := QueryRequest{
		Mode: "hybrid", Fusion: "weighted", Alpha: 0.70, RRFK: 60, TopK: 10, CandidateK: 50,
	}
	if !reflect.DeepEqual(req, want) {
		t.Fatalf("defaults = %+v, want %+v", req, want)
	}
}

func TestLoadSettingsRejectsAMissingNamedFile(t *testing.T) {
	if _, err := LoadSettings(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Fatal("a named config file that does not exist must be an error")
	}
}

func TestQueryDefaultsOverlayTheFile(t *testing.T) {
	path := writeSettings(t, `
db:
  path: wiki.db
query:
  mode: lexical
  top_k: 25
  expand: 1
  priors: [recency, seed]
  prior_strength: 0.3
  recency_half_life: 30d
  source_weights:
    - id: Docs
      weight: 1.5
`)

	settings, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.DBPath() != "wiki.db" {
		t.Fatalf("DBPath = %q, want wiki.db", settings.DBPath())
	}

	req, err := settings.QueryDefaults()
	if err != nil {
		t.Fatalf("QueryDefaults: %v", err)
	}
	if req.Mode != "lexical" || req.TopK != 25 || req.Expand != 1 {
		t.Fatalf("file values lost: %+v", req)
	}
	if req.Fusion != "weighted" || req.CandidateK != 50 {
		t.Fatalf("keys the file omits must keep the built-in default: %+v", req)
	}
	if got := req.Priors.Priors; len(got) != 2 || got[0] != query.PriorRecency || got[1] != query.PriorSeed {
		t.Fatalf("priors = %v, want [recency seed]", got)
	}
	if req.Priors.Strength != 0.3 || req.Priors.RecencyHalfLife != 30*24*time.Hour {
		t.Fatalf("prior knobs = %+v", req.Priors)
	}
	if !reflect.DeepEqual(req.SourceWeights, map[string]float64{"Docs": 1.5}) {
		t.Fatalf("source weights = %v, want Docs=1.5 with its case kept", req.SourceWeights)
	}
}

func TestResolveEmbeddingLayersFlagEnvFile(t *testing.T) {
	path := writeSettings(t, `
embedding:
  provider: bow-local
  dimension: 128
  context: title
`)
	settings, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}

	t.Setenv("CONFLUENCE2MD_EMBEDDING_DIM", "256")

	fromFile, err := settings.ResolveEmbedding(EmbeddingOptions{}, EmbeddingPresent{})
	if err != nil {
		t.Fatalf("ResolveEmbedding: %v", err)
	}
	if fromFile.Provider != "bow-local" || fromFile.Dimension != 256 {
		t.Fatalf("env must beat the file and the file must beat defaults: %+v", fromFile)
	}
	if len(fromFile.Context) != 1 || fromFile.Context[0] != "title" {
		t.Fatalf("context from the file = %v, want [title]", fromFile.Context)
	}

	explicit, err := settings.ResolveEmbedding(
		EmbeddingOptions{Dimension: 512},
		EmbeddingPresent{Dimension: true},
	)
	if err != nil {
		t.Fatalf("ResolveEmbedding: %v", err)
	}
	if explicit.Dimension != 512 {
		t.Fatalf("an explicit value must beat the environment: %+v", explicit)
	}
}

func TestQueryDefaultsTrimModeAndFusion(t *testing.T) {
	settings, err := LoadSettings(writeSettings(t, "query:\n  mode: \" lexical \"\n  fusion: \" rrf \"\n"))
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}

	req, err := settings.QueryDefaults()
	if err != nil {
		t.Fatalf("QueryDefaults: %v", err)
	}
	if req.Mode != "lexical" || req.Fusion != "rrf" {
		t.Fatalf("mode/fusion = %q/%q, want trimmed lexical/rrf", req.Mode, req.Fusion)
	}
}

func TestResolveEmbeddingDoesNotShareSlicesWithSettings(t *testing.T) {
	settings, err := LoadSettings(writeSettings(t, "embedding:\n  provider: bow-local\n  headers: [\"X-One=1\"]\n  context: title\n"))
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}

	first, err := settings.ResolveEmbedding(EmbeddingOptions{}, EmbeddingPresent{})
	if err != nil {
		t.Fatalf("ResolveEmbedding: %v", err)
	}
	first.Headers[0] = "X-Changed=2"
	first.Context[0] = "space"

	second, err := settings.ResolveEmbedding(EmbeddingOptions{}, EmbeddingPresent{})
	if err != nil {
		t.Fatalf("ResolveEmbedding: %v", err)
	}
	if second.Headers[0] != "X-One=1" || second.Context[0] != "title" {
		t.Fatalf("editing one result leaked into the next: %v / %v", second.Headers, second.Context)
	}
}
