// Command smokevector verifies the offline vector pipeline end to end.
//
// The support matrix makes vector capability a release gate, so this tool indexes
// a fixture corpus with the default provider and then checks the vector query,
// hybrid fusion, lexical-only retrieval and the embedding identity guard. It needs
// no network, no API key and no external service, which is what makes it usable as
// a CI gate on every platform.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gkoos/confluence2md-indexer/internal/cli"
	"github.com/gkoos/confluence2md-indexer/internal/embedding"
)

// exitCodeInvalidUsage mirrors the CLI's code for validation and query failures.
const exitCodeInvalidUsage = 2

const fixtureMetadata = `{
  "pages": {
    "1": {
      "local_path": "deployment.md",
      "title": "Deployment Guide",
      "space_key": "OPS",
      "last_modified_at": "2026-01-15T12:00:00Z",
      "source_url": "https://example.test/1"
    }
  }
}`

const fixtureMarkdown = `# Deployment

Rotate the deployment credentials from the runbook before every release.

## Rollback

kubectl rollout undo deploy/mfs
`

func main() {
	verbose := flag.Bool("v", false, "print each successful command")
	flag.Parse()

	if err := run(*verbose); err != nil {
		fmt.Fprintf(os.Stderr, "vector smoke gate FAILED: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("vector smoke gate passed: index, stats, vector query, hybrid query, lexical-only query, identity guard")
}

func run(verbose bool) error {
	// Pin the gate to the built-in default provider so a developer's shell or a CI
	// job cannot silently redirect it at a network endpoint.
	clearEmbeddingEnv()

	dir, err := os.MkdirTemp("", "c2md-smokevector-")
	if err != nil {
		return fmt.Errorf("create fixture directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// Run from the fixture directory: a config.yaml in the caller's working
	// directory must not be able to redirect the gate.
	leave, err := chdir(dir)
	if err != nil {
		return err
	}
	defer leave()

	if err := writeFixture(dir); err != nil {
		return err
	}

	dbPath := filepath.Join(dir, "smoke.db")
	app := cli.NewApp()

	// Index with no embedding flags: the offline default has to carry this alone.
	if _, code, err := runApp(app, []string{"index", dir, "--db", dbPath}); err != nil {
		return err
	} else if code != 0 {
		return fmt.Errorf("index exited with code %d", code)
	}

	if err := checkStats(app, dbPath); err != nil {
		return err
	}
	if _, code, err := runApp(app, []string{"stats", "--db", dbPath}); err != nil {
		return err
	} else if code != 0 {
		return fmt.Errorf("stats exited with code %d", code)
	}

	// A vector query must be scored, not merely executed: returning zero results
	// also exits successfully, which is exactly the failure this gate guards.
	if err := checkQuery(app, dbPath, []string{"--q", "rollback", "--mode", "vector"}, true); err != nil {
		return fmt.Errorf("vector query: %w", err)
	}
	if err := checkQuery(app, dbPath, []string{"--q", "rollback", "--mode", "hybrid"}, true); err != nil {
		return fmt.Errorf("hybrid query: %w", err)
	}
	if err := checkQuery(app, dbPath, []string{"--q", "rollback", "--lexical-only"}, false); err != nil {
		return fmt.Errorf("lexical-only query: %w", err)
	}

	// A provider that does not match the stored identity must fail loudly rather
	// than scoring zeros everywhere and reporting no results.
	guardOutput, code, err := runAppQuiet(app, []string{"query", "--db", dbPath, "--q", "rollback", "--mode", "vector", "--embedding-dim", "512"})
	if err != nil {
		return err
	}
	if code != exitCodeInvalidUsage {
		return fmt.Errorf("expected the embedding identity guard to exit with %d, got %d", exitCodeInvalidUsage, code)
	}
	if !strings.Contains(guardOutput, "embedding mismatch") {
		return fmt.Errorf("expected the identity guard to explain the mismatch, got %q", strings.TrimSpace(guardOutput))
	}

	if verbose {
		fmt.Println("smoke fixture indexed and queried in", dir)
	}
	return nil
}

// clearEmbeddingEnv removes embedding configuration so the default provider is
// the one under test.
// chdir enters dir and returns a function that restores the previous working
// directory.
func chdir(dir string) (func(), error) {
	previous, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	if err := os.Chdir(dir); err != nil {
		return nil, fmt.Errorf("enter %s: %w", dir, err)
	}
	return func() { _ = os.Chdir(previous) }, nil
}

func clearEmbeddingEnv() {
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, embedding.EnvPrefix) {
			_ = os.Unsetenv(name)
		}
	}
}

func writeFixture(dir string) error {
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(fixtureMetadata), 0o644); err != nil {
		return fmt.Errorf("write fixture metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deployment.md"), []byte(fixtureMarkdown), 0o644); err != nil {
		return fmt.Errorf("write fixture markdown: %w", err)
	}
	return nil
}

// runApp runs a command with stdout captured and stderr passed through.
func runApp(app *cli.App, args []string) (string, int, error) {
	return runCommand(app, args, false)
}

// runAppQuiet also captures stderr, for commands whose stderr is an expected
// failure message rather than something an operator should see.
func runAppQuiet(app *cli.App, args []string) (string, int, error) {
	return runCommand(app, args, true)
}

// runCommand runs a command with its output captured, so JSON payloads can be
// asserted on and the gate stays quiet.
func runCommand(app *cli.App, args []string, captureStderr bool) (string, int, error) {
	outFile, err := os.CreateTemp("", "c2md-smokevector-out-")
	if err != nil {
		return "", 0, fmt.Errorf("create output file: %w", err)
	}
	outName := outFile.Name()
	defer func() { _ = os.Remove(outName) }()

	var errFile *os.File
	if captureStderr {
		errFile, err = os.CreateTemp("", "c2md-smokevector-err-")
		if err != nil {
			_ = outFile.Close()
			return "", 0, fmt.Errorf("create error file: %w", err)
		}
		errName := errFile.Name()
		defer func() { _ = os.Remove(errName) }()
	}

	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout = outFile
	if captureStderr {
		os.Stderr = errFile
	}
	defer func() {
		os.Stdout, os.Stderr = savedOut, savedErr
	}()

	code := app.Run(args)

	output, err := readCaptured(outFile, outName, "output")
	if err != nil {
		return "", 0, err
	}
	if captureStderr {
		errorOutput, err := readCaptured(errFile, errFile.Name(), "error output")
		if err != nil {
			return "", 0, err
		}
		output += errorOutput
	}

	return output, code, nil
}

func readCaptured(file *os.File, name string, label string) (string, error) {
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close %s file: %w", label, err)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("read command %s: %w", label, err)
	}
	return string(data), nil
}

type statsPayload struct {
	Stats struct {
		Embeddings       int    `json:"embeddings"`
		VectorReady      bool   `json:"vectorReady"`
		VectorName       string `json:"vectorName"`
		VectorCapability string `json:"vectorCapability"`
	} `json:"stats"`
}

type queryPayload struct {
	Count   int `json:"count"`
	Results []struct {
		ChunkID     string  `json:"chunkId"`
		VectorScore float64 `json:"vectorScore"`
	} `json:"results"`
}

// checkStats asserts that indexing produced a usable vector channel with the
// default provider's identity recorded.
func checkStats(app *cli.App, dbPath string) error {
	output, code, err := runApp(app, []string{"stats", "--db", dbPath, "--json"})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("stats --json exited with code %d", code)
	}

	var payload statsPayload
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return fmt.Errorf("decode stats output: %w", err)
	}
	if !payload.Stats.VectorReady {
		return fmt.Errorf("expected a ready vector channel after indexing, got %s", strings.TrimSpace(output))
	}
	if payload.Stats.Embeddings == 0 {
		return fmt.Errorf("expected stored embeddings after indexing, got %s", strings.TrimSpace(output))
	}
	if payload.Stats.VectorCapability != embedding.CapabilityLexical {
		return fmt.Errorf("expected the default provider's capability %q, got %q", embedding.CapabilityLexical, payload.Stats.VectorCapability)
	}
	if !strings.HasPrefix(payload.Stats.VectorName, embedding.ProviderBowLocal+":") {
		return fmt.Errorf("expected the default provider identity, got %q", payload.Stats.VectorName)
	}

	return nil
}

// checkQuery asserts that a query returns at least one result, and optionally
// that the vector channel contributed a non-zero score.
func checkQuery(app *cli.App, dbPath string, args []string, requireVectorScore bool) error {
	full := append([]string{"query", "--db", dbPath, "--json"}, args...)

	output, code, err := runApp(app, full)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("%v exited with code %d", args, code)
	}

	var payload queryPayload
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return fmt.Errorf("decode query output for %v: %w", args, err)
	}
	if payload.Count == 0 {
		return fmt.Errorf("expected at least one result for %v", args)
	}

	if requireVectorScore {
		best := 0.0
		for _, result := range payload.Results {
			if result.VectorScore > best {
				best = result.VectorScore
			}
		}
		if best <= 0 {
			return fmt.Errorf("expected a non-zero vector score for %v, got %s", args, strings.TrimSpace(output))
		}
	}

	return nil
}
