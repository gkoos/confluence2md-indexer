package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// commandPayload mirrors the fields these tests read from index, query and stats
// output.
type commandPayload struct {
	DBPath    string `json:"dbPath"`
	Embedding struct {
		Provider string `json:"provider"`
		Source   string `json:"source"`
		Written  int    `json:"written"`
	} `json:"embedding"`
}

// writeConfigFile stores a configuration file and returns its path.
func writeConfigFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return path
}

func decodePayload(t *testing.T, output string) commandPayload {
	t.Helper()

	var payload commandPayload
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("decode output %q: %v", output, err)
	}
	return payload
}

func TestIndexAppliesEmbeddingAndDBFromConfigFile(t *testing.T) {
	dir := writeIndexFixture(t, "# Doc\n\nbody text\n")
	dbPath := filepath.Join(t.TempDir(), "configured.db")
	configPath := writeConfigFile(t, "db:\n  path: '"+dbPath+"'\n\nembedding:\n  provider: \"bow-local\"\n")
	app := newTestApp(t)

	output := captureStdout(t, func() {
		if exit := app.Run([]string{"index", dir, "--config", configPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("index exit code: %d", exit)
		}
	})

	payload := decodePayload(t, output)
	if payload.Embedding.Source != "config" {
		t.Fatalf("embedding source = %q, want %q", payload.Embedding.Source, "config")
	}
	if !strings.HasPrefix(payload.Embedding.Provider, "bow-local") {
		t.Fatalf("embedding provider = %q, want it to name bow-local", payload.Embedding.Provider)
	}
	if payload.DBPath != dbPath {
		t.Fatalf("db path = %q, want %q", payload.DBPath, dbPath)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("the configured database was not created: %v", err)
	}
}

func TestIndexFlagOverridesConfigFile(t *testing.T) {
	dir := writeIndexFixture(t, "# Doc\n\nbody text\n")
	configuredDB := filepath.Join(t.TempDir(), "configured.db")
	flagDB := filepath.Join(t.TempDir(), "flagged.db")
	configPath := writeConfigFile(t, "db:\n  path: '"+configuredDB+"'\n\nembedding:\n  provider: \"bow-local\"\n")
	app := newTestApp(t)

	output := captureStdout(t, func() {
		exit := app.Run([]string{"index", dir, "--config", configPath, "--db", flagDB, "--embedding", "bow-local", "--json"})
		if exit != exitCodeOK {
			t.Fatalf("index exit code: %d", exit)
		}
	})

	payload := decodePayload(t, output)
	if payload.DBPath != flagDB {
		t.Fatalf("db path = %q, want the flag value %q", payload.DBPath, flagDB)
	}
	if payload.Embedding.Source != "flag" {
		t.Fatalf("embedding source = %q, want %q", payload.Embedding.Source, "flag")
	}
	if _, err := os.Stat(configuredDB); err == nil {
		t.Fatalf("the configured database %q must not be created", configuredDB)
	}
}

func TestStatsAndQueryReadDBPathFromConfigFile(t *testing.T) {
	dir := writeIndexFixture(t, "# Doc\n\nbanana body text\n")
	dbPath := filepath.Join(t.TempDir(), "configured.db")
	configPath := writeConfigFile(t, "db:\n  path: '"+dbPath+"'\n")
	app := newTestApp(t)

	captureStdout(t, func() {
		if exit := app.Run([]string{"index", dir, "--config", configPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("index exit code: %d", exit)
		}
	})

	statsOut := captureStdout(t, func() {
		if exit := app.Run([]string{"stats", "--config", configPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("stats exit code: %d", exit)
		}
	})
	if got := decodePayload(t, statsOut).DBPath; got != dbPath {
		t.Fatalf("stats db path = %q, want %q", got, dbPath)
	}

	queryOut := captureStdout(t, func() {
		if exit := app.Run([]string{"query", "--config", configPath, "--q", "banana", "--json"}); exit != exitCodeOK {
			t.Fatalf("query exit code: %d", exit)
		}
	})
	if got := decodePayload(t, queryOut).DBPath; got != dbPath {
		t.Fatalf("query db path = %q, want %q", got, dbPath)
	}
}

func TestIndexConfigFileCanSkipEmbeddings(t *testing.T) {
	dir := writeIndexFixture(t, "# Doc\n\nbody text\n")
	configPath := writeConfigFile(t, "embedding:\n  provider: \"bow-local\"\n  skip: true\n")
	app := newTestApp(t)

	output := captureStdout(t, func() {
		exit := app.Run([]string{"index", dir, "--config", configPath, "--db", filepath.Join(t.TempDir(), "skip.db"), "--json"})
		if exit != exitCodeOK {
			t.Fatalf("index exit code: %d", exit)
		}
	})

	payload := decodePayload(t, output)
	if payload.Embedding.Source != "config" {
		t.Fatalf("embedding source = %q, want %q", payload.Embedding.Source, "config")
	}
	if payload.Embedding.Written != 0 {
		t.Fatalf("embedding writes = %d, want none when the file sets skip", payload.Embedding.Written)
	}
}

func TestIndexRejectsBrokenConfigFile(t *testing.T) {
	dir := writeIndexFixture(t, "# Doc\n\nbody text\n")
	app := newTestApp(t)

	stderr := captureStderr(t, func() {
		missing := filepath.Join(t.TempDir(), "absent.yaml")
		if exit := app.Run([]string{"index", dir, "--config", missing}); exit != exitCodeInvalidUsage {
			t.Fatalf("exit code = %d, want %d", exit, exitCodeInvalidUsage)
		}
	})
	if !strings.Contains(stderr, "does not exist") {
		t.Fatalf("stderr = %q, want a does-not-exist message", stderr)
	}

	unknownKey := writeConfigFile(t, "embedding:\n  dimesion: 256\n")
	stderr = captureStderr(t, func() {
		if exit := app.Run([]string{"index", dir, "--config", unknownKey}); exit != exitCodeInvalidUsage {
			t.Fatalf("exit code = %d, want %d", exit, exitCodeInvalidUsage)
		}
	})
	if !strings.Contains(stderr, "dimesion") {
		t.Fatalf("stderr = %q, want it to name the unknown key", stderr)
	}
}

func TestEmbeddingListDoesNotNeedAConfigFile(t *testing.T) {
	app := newTestApp(t)

	output := captureStdout(t, func() {
		missing := filepath.Join(t.TempDir(), "absent.yaml")
		if exit := app.Run([]string{"index", "--config", missing, "--embedding", "list"}); exit != exitCodeOK {
			t.Fatalf("exit code = %d, want %d", exit, exitCodeOK)
		}
	})
	if !strings.Contains(output, "bow-local") {
		t.Fatalf("provider list = %q, want it to mention bow-local", output)
	}
}
