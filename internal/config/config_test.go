package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gkoos/confluence2md-indexer/internal/embedding"
)

const sampleFile = `
db:
  path: "indexes/wiki.db"

embedding:
  provider: "openai-compatible"
  model: "Qwen/Qwen3-Embedding-0.6B"
  base_url: "https://api.example.test/v1"
  path: "/embeddings"
  dimension: 1024
  api_key_env: "EXAMPLE_API_KEY"
  auth_header: "X-Api-Key"
  auth_scheme: "none"
  headers:
    - "X-Tenant=docs"
  query_params:
    - "api-version=2024-05"
  document_prefix: "passage: "
  query_prefix: "query: "
  batch_size: 32
  timeout: 12s
  max_retries: 1
  skip: false
`

// writeConfig writes a configuration file into a fresh temporary directory and
// returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), DefaultFileName)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return path
}

func TestLoadReadsEverySupportedKey(t *testing.T) {
	cfg, err := Load(writeConfig(t, sampleFile))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !filepath.IsAbs(cfg.Path) {
		t.Fatalf("reported path = %q, want an absolute path", cfg.Path)
	}
	if got, want := cfg.File.DBPath(), "indexes/wiki.db"; got != want {
		t.Fatalf("db path = %q, want %q", got, want)
	}

	options, present := cfg.File.EmbeddingOptions()
	want := embedding.Options{
		Provider:    "openai-compatible",
		Model:       "Qwen/Qwen3-Embedding-0.6B",
		BaseURL:     "https://api.example.test/v1",
		Path:        "/embeddings",
		Dimension:   1024,
		APIKeyEnv:   "EXAMPLE_API_KEY",
		AuthHeader:  "X-Api-Key",
		AuthScheme:  "none",
		Headers:     []string{"X-Tenant=docs"},
		QueryParams: []string{"api-version=2024-05"},
		DocPrefix:   "passage: ",
		QueryPrefix: "query: ",
		BatchSize:   32,
		Timeout:     12 * time.Second,
		MaxRetries:  1,
	}
	if !reflect.DeepEqual(options, want) {
		t.Fatalf("options = %+v, want %+v", options, want)
	}

	wantPresent := embedding.Present{
		Provider:    true,
		Model:       true,
		BaseURL:     true,
		Path:        true,
		Dimension:   true,
		APIKeyEnv:   true,
		AuthHeader:  true,
		AuthScheme:  true,
		Headers:     true,
		QueryParams: true,
		DocPrefix:   true,
		QueryPrefix: true,
		BatchSize:   true,
		Timeout:     true,
		MaxRetries:  true,
		Skip:        true,
	}
	if present != wantPresent {
		t.Fatalf("present = %+v, want %+v", present, wantPresent)
	}
	if present.APIKey {
		t.Fatal("api_key was absent, so it must not be reported as present")
	}
}

func TestLoadKeepsLiteralAPIKey(t *testing.T) {
	cfg, err := Load(writeConfig(t, "embedding:\n  api_key: \"sk-literal\"\n"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	options, present := cfg.File.EmbeddingOptions()
	if !present.APIKey || options.APIKey != "sk-literal" {
		t.Fatalf("api_key = %q (present %t), want %q", options.APIKey, present.APIKey, "sk-literal")
	}
}

func TestLoadLeavesAbsentKeysAbsent(t *testing.T) {
	cfg, err := Load(writeConfig(t, "embedding:\n  dimension: 512\n"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	options, present := cfg.File.EmbeddingOptions()
	if want := (embedding.Options{Dimension: 512}); !reflect.DeepEqual(options, want) {
		t.Fatalf("options = %+v, want %+v", options, want)
	}
	if want := (embedding.Present{Dimension: true}); present != want {
		t.Fatalf("present = %+v, want %+v", present, want)
	}
	// Operational defaults belong to the merge step, not to the file.
	if options.BatchSize != 0 || options.Timeout != 0 || options.MaxRetries != 0 {
		t.Fatalf("absent operational keys must stay zero, got %+v", options)
	}
}

func TestLoadEmptyFileSuppliesNothing(t *testing.T) {
	cfg, err := Load(writeConfig(t, "# deliberately empty\n"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	options, present := cfg.File.EmbeddingOptions()
	if !reflect.DeepEqual(options, embedding.Options{}) {
		t.Fatalf("options = %+v, want all zero", options)
	}
	if present != (embedding.Present{}) {
		t.Fatalf("present = %+v, want all false", present)
	}
	if got := cfg.File.DBPath(); got != "" {
		t.Fatalf("db path = %q, want empty", got)
	}
}

func TestLoadDiscoversDefaultFileName(t *testing.T) {
	t.Setenv(EnvFileName, "")

	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, DefaultFileName), []byte("db:\n  path: found.db\n"), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.File.DBPath(), "found.db"; got != want {
		t.Fatalf("db path = %q, want %q", got, want)
	}
}

func TestLoadWithoutAnyFileSucceeds(t *testing.T) {
	t.Setenv(EnvFileName, "")
	t.Chdir(t.TempDir())

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("a missing default file must not fail: %v", err)
	}
	if cfg.Path != "" {
		t.Fatalf("reported path = %q, want empty", cfg.Path)
	}
}

func TestLoadPrefersExplicitPathOverEnvironmentAndDefault(t *testing.T) {
	t.Setenv(EnvFileName, writeConfig(t, "db:\n  path: from-env.db\n"))

	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, DefaultFileName), []byte("db:\n  path: from-default.db\n"), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	explicit := writeConfig(t, "db:\n  path: from-flag.db\n")
	cfg, err := Load(explicit)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.File.DBPath(), "from-flag.db"; got != want {
		t.Fatalf("db path = %q, want %q", got, want)
	}
}

func TestLoadReadsFileNamedByEnvironmentBeforeDefault(t *testing.T) {
	t.Setenv(EnvFileName, writeConfig(t, "db:\n  path: from-env.db\n"))

	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, DefaultFileName), []byte("db:\n  path: from-default.db\n"), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.File.DBPath(), "from-env.db"; got != want {
		t.Fatalf("db path = %q, want %q", got, want)
	}
}

func TestLoadRejectsMissingFiles(t *testing.T) {
	t.Setenv(EnvFileName, "")
	t.Chdir(t.TempDir())

	t.Run("explicit path", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("error = %v, want a does-not-exist message", err)
		}
	})

	t.Run("path from the environment", func(t *testing.T) {
		t.Setenv(EnvFileName, filepath.Join(t.TempDir(), "absent.yaml"))

		_, err := Load("")
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("error = %v, want a does-not-exist message", err)
		}
	})
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	cases := map[string]struct {
		content string
		want    string
	}{
		"unknown section": {"index:\n  rebuild: true\n", "index"},
		"embedding typo":  {"embedding:\n  dimesion: 1024\n", "dimesion"},
		"db typo":         {"db:\n  file: wiki.db\n", "file"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.content))
			if err == nil {
				t.Fatalf("expected an error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := map[string]struct {
		content string
		want    string
	}{
		"negative dimension":  {"embedding:\n  dimension: -1\n", "embedding.dimension"},
		"negative batch size": {"embedding:\n  batch_size: -1\n", "embedding.batch_size"},
		"negative timeout":    {"embedding:\n  timeout: -1s\n", "embedding.timeout"},
		"retries below -1":    {"embedding:\n  max_retries: -2\n", "embedding.max_retries"},
		"two credentials":     {"embedding:\n  api_key: \"literal\"\n  api_key_env: \"SOME_VAR\"\n", "api_key"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.content))
			if err == nil {
				t.Fatalf("expected an error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), "invalid config file") {
				t.Fatalf("error = %v, want an invalid-config message", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadTreatsEmptyValuesAsNoOverride(t *testing.T) {
	// A template that spells out every key spells some of them empty.
	cfg, err := Load(writeConfig(t, "db:\n  path: \"\"\n\nembedding:\n  provider: \"\"\n  model: \"\"\n"))
	if err != nil {
		t.Fatalf("empty values must not be rejected: %v", err)
	}
	if got := cfg.File.DBPath(); got != "" {
		t.Fatalf("db path = %q, want empty", got)
	}
}

func TestLoadRejectsBrokenYAML(t *testing.T) {
	_, err := Load(writeConfig(t, "embedding:\n  provider: [unclosed\n"))
	if err == nil || !strings.Contains(err.Error(), "read config file") {
		t.Fatalf("error = %v, want a read-config message", err)
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Fatalf("error = %v, want it to point at the YAML syntax", err)
	}
}
func TestLoadReadsQueryDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, `query:
  mode: "lexical"
  fusion: "rrf"
  alpha: 0.4
  top_k: 5
  candidate_k: 20
  expand: 2
  priors: ["recency", "seed"]
  prior_strength: 0.3
  recency_half_life: 90d
`))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	q := cfg.File.Query
	if q == nil {
		t.Fatal("the query section must be present")
	}
	if q.Mode == nil || *q.Mode != "lexical" || q.Fusion == nil || *q.Fusion != "rrf" {
		t.Fatalf("mode/fusion = %+v, want the file values", q)
	}
	if q.Alpha == nil || *q.Alpha != 0.4 {
		t.Fatalf("alpha = %v, want 0.4", q.Alpha)
	}
	if q.TopK == nil || *q.TopK != 5 || q.CandidateK == nil || *q.CandidateK != 20 || q.Expand == nil || *q.Expand != 2 {
		t.Fatalf("counts = %+v, want the file values", q)
	}
	if len(q.Priors) != 2 || q.Priors[0] != "recency" || q.Priors[1] != "seed" {
		t.Fatalf("priors = %v, want the file list", q.Priors)
	}
	if q.PriorStrength == nil || *q.PriorStrength != 0.3 {
		t.Fatalf("prior strength = %v, want 0.3", q.PriorStrength)
	}
	if q.RecencyHalfLife == nil || *q.RecencyHalfLife != "90d" {
		t.Fatalf("recency half-life = %v, want the age text 90d", q.RecencyHalfLife)
	}
}

func TestLoadLeavesAbsentQueryKeysUnset(t *testing.T) {
	cfg, err := Load(writeConfig(t, "query:\n  mode: \"hybrid\"\n"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	q := cfg.File.Query
	if q == nil || q.Mode == nil {
		t.Fatalf("query = %+v, want the mode the file sets", q)
	}
	if q.Fusion != nil || q.Alpha != nil || q.TopK != nil || q.CandidateK != nil || q.Expand != nil {
		t.Fatalf("query = %+v, want absent keys to stay nil", q)
	}
	if q.Priors != nil || q.PriorStrength != nil || q.RecencyHalfLife != nil {
		t.Fatalf("query = %+v, want absent prior keys to stay nil", q)
	}

	withoutSection, err := Load(writeConfig(t, "db:\n  path: \"\"\n"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if withoutSection.File.Query != nil {
		t.Fatalf("query = %+v, want nil without a query section", withoutSection.File.Query)
	}
}

func TestLoadReadsEmptyPriorListAsOff(t *testing.T) {
	cfg, err := Load(writeConfig(t, "query:\n  priors: []\n"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.File.Query == nil || cfg.File.Query.Priors == nil {
		t.Fatal("an empty list is present, not absent, so it clears a configured default")
	}
	if len(cfg.File.Query.Priors) != 0 {
		t.Fatalf("priors = %v, want an empty list", cfg.File.Query.Priors)
	}
}

func TestLoadRejectsInvalidQueryDefaults(t *testing.T) {
	cases := map[string]struct {
		content string
		want    string
	}{
		"unknown mode":         {"query:\n  mode: \"semantic\"\n", "query.mode"},
		"unknown fusion":       {"query:\n  fusion: \"borda\"\n", "query.fusion"},
		"alpha above one":      {"query:\n  alpha: 1.5\n", "query.alpha"},
		"top k of zero":        {"query:\n  top_k: 0\n", "query.top_k"},
		"negative candidate k": {"query:\n  candidate_k: -5\n", "query.candidate_k"},
		"negative expand":      {"query:\n  expand: -1\n", "query.expand"},
		"unknown prior":        {"query:\n  priors: [\"freshness\"]\n", "query.priors"},
		"strength above one":   {"query:\n  prior_strength: 2\n", "query.prior_strength"},
		"negative half-life":   {"query:\n  recency_half_life: -1h\n", "query.recency_half_life"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.content))
			if err == nil {
				t.Fatalf("expected an error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), "invalid config file") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want an invalid-config message naming %q", err, tc.want)
			}
		})
	}
}
