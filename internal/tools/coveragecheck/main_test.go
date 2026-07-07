package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMinFromEnvDefaultAndInvalid(t *testing.T) {
	t.Setenv("COVERAGE_MIN", "")
	if got := minFromEnv(); got != 70 {
		t.Fatalf("expected default 70 got %v", got)
	}

	t.Setenv("COVERAGE_MIN", "invalid")
	if got := minFromEnv(); got != 70 {
		t.Fatalf("expected fallback 70 for invalid value got %v", got)
	}
}

func TestMinFromEnvValid(t *testing.T) {
	t.Setenv("COVERAGE_MIN", "72.5")
	if got := minFromEnv(); got != 72.5 {
		t.Fatalf("expected 72.5 got %v", got)
	}
}

func TestParseProfile(t *testing.T) {
	profile := "mode: atomic\nfoo.go:1.1,1.10 2 1\nbar.go:1.1,1.10 3 0\n"
	path := filepath.Join(t.TempDir(), "coverage.out")
	if err := os.WriteFile(path, []byte(profile), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	total, covered, err := parseProfile(path)
	if err != nil {
		t.Fatalf("parse profile: %v", err)
	}
	if total != 5 || covered != 2 {
		t.Fatalf("expected total=5 covered=2 got total=%d covered=%d", total, covered)
	}
}

func TestParseProfileInvalidHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coverage.out")
	if err := os.WriteFile(path, []byte("bad-header\n"), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	_, _, err := parseProfile(path)
	if err == nil {
		t.Fatalf("expected parse error for invalid header")
	}
}
