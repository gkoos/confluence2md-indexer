package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/config"
)

// testAppWithVersion returns an app that reports the given build, with the
// configuration file pinned the way the other CLI tests pin it.
func testAppWithVersion(t *testing.T, version string) *App {
	t.Helper()

	app := newTestApp(t)
	app.Version = version

	return app
}

func TestVersionCommandPrintsTheBuild(t *testing.T) {
	for _, arg := range []string{"version", "-v", "--version"} {
		t.Run(arg, func(t *testing.T) {
			app := testAppWithVersion(t, "1.2.3")

			stdout := captureStdout(t, func() {
				if exit := app.Run([]string{arg}); exit != exitCodeOK {
					t.Fatalf("exit code = %d, want %d", exit, exitCodeOK)
				}
			})
			if stdout != "1.2.3\n" {
				t.Fatalf("output = %q, want the bare version", stdout)
			}
		})
	}
}

func TestVersionCommandIgnoresConfiguration(t *testing.T) {
	// The version is answered before configuration or a database is touched, so a
	// broken config path cannot break it.
	t.Setenv(config.EnvFileName, filepath.Join(t.TempDir(), "absent.yaml"))
	app := NewApp()

	stdout := captureStdout(t, func() {
		if exit := app.Run([]string{"--version"}); exit != exitCodeOK {
			t.Fatalf("exit code = %d, want %d", exit, exitCodeOK)
		}
	})
	if strings.TrimSpace(stdout) != DefaultVersion {
		t.Fatalf("output = %q, want %q", stdout, DefaultVersion)
	}
}

func TestZeroValueAppReportsTheDevelopmentVersion(t *testing.T) {
	var app App

	stdout := captureStdout(t, func() {
		if exit := app.Run([]string{"--version"}); exit != exitCodeOK {
			t.Fatalf("exit code = %d, want %d", exit, exitCodeOK)
		}
	})
	if strings.TrimSpace(stdout) != DefaultVersion {
		t.Fatalf("output = %q, want %q", stdout, DefaultVersion)
	}
}

func TestCommandsLogTheBuildOnStderr(t *testing.T) {
	app := testAppWithVersion(t, "9.9.9")
	dir := writeIndexFixture(t, "# Doc\n\nbody text\n")
	dbPath := filepath.Join(t.TempDir(), "index.db")

	stderr := captureStderr(t, func() {
		if exit := app.Run([]string{"index", dir, "--db", dbPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("index exit code: %d", exit)
		}
	})
	if !strings.Contains(stderr, "confluence2md-indexer 9.9.9") {
		t.Fatalf("stderr = %q, want a startup line naming the build", stderr)
	}

	statsOut := captureStdout(t, func() {
		if exit := app.Run([]string{"stats", "--db", dbPath, "--json"}); exit != exitCodeOK {
			t.Fatalf("stats exit code: %d", exit)
		}
	})
	if strings.Contains(statsOut, "9.9.9") {
		t.Fatalf("stdout = %q, want the build kept out of the data channel", statsOut)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(statsOut), &payload); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
}

func TestUnknownSubcommandLogsBeforeFailing(t *testing.T) {
	app := testAppWithVersion(t, "9.9.9")

	stderr := captureStderr(t, func() {
		if exit := app.Run([]string{"nonsense"}); exit != exitCodeInvalidUsage {
			t.Fatalf("exit code = %d, want %d", exit, exitCodeInvalidUsage)
		}
	})
	if !strings.Contains(stderr, "confluence2md-indexer 9.9.9") || !strings.Contains(stderr, "unknown subcommand: nonsense") {
		t.Fatalf("stderr = %q, want the build line and the failure", stderr)
	}
}

func TestUsageListsTheVersionCommand(t *testing.T) {
	app := testAppWithVersion(t, "9.9.9")

	stdout := captureStdout(t, func() {
		if exit := app.Run([]string{"--help"}); exit != exitCodeOK {
			t.Fatalf("exit code = %d, want %d", exit, exitCodeOK)
		}
	})
	if !strings.Contains(stdout, "--version") {
		t.Fatalf("usage = %q, want it to document --version", stdout)
	}
}
