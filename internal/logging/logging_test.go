package logging

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestNewReturnsLogger(t *testing.T) {
	logger := New()
	if logger == nil {
		t.Fatalf("expected logger instance")
	}
}

func TestLoggerWritesInfoToStderr(t *testing.T) {
	output := captureStderr(t, func() {
		logger := New()
		logger.Info("hello-info")
	})

	if !strings.Contains(output, "hello-info") {
		t.Fatalf("expected info message in output: %s", output)
	}
}

func TestLoggerFiltersDebugAtInfoLevel(t *testing.T) {
	output := captureStderr(t, func() {
		logger := New()
		logger.Debug("hidden-debug")
		logger.Info("visible-info")
	})

	if strings.Contains(output, "hidden-debug") {
		t.Fatalf("did not expect debug message in output: %s", output)
	}
	if !strings.Contains(output, "visible-info") {
		t.Fatalf("expected info message in output: %s", output)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()

	return buf.String()
}
