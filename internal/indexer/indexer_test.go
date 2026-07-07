package indexer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightSuccess(t *testing.T) {
	dir := t.TempDir()
	metadata := `{
  "pages": {
    "123": {"local_path": "doc_123.md"}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, MetadataFileName), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "doc_123.md"), []byte("# hello"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	summary, err := Preflight(dir)
	if err != nil {
		t.Fatalf("preflight error: %v", err)
	}
	if summary.PageCount != 1 {
		t.Fatalf("expected page count 1, got %d", summary.PageCount)
	}
	if summary.MarkdownChecked != 1 {
		t.Fatalf("expected markdown checked 1, got %d", summary.MarkdownChecked)
	}
}

func TestPreflightMissingMetadata(t *testing.T) {
	dir := t.TempDir()
	_, err := Preflight(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing required file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreflightEmptyPagesCorpus(t *testing.T) {
	dir := t.TempDir()
	metadata := `{"pages": {}}`
	if err := os.WriteFile(filepath.Join(dir, MetadataFileName), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	_, err := Preflight(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "empty pages corpus") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreflightMissingMarkdownFile(t *testing.T) {
	dir := t.TempDir()
	metadata := `{
  "pages": {
    "123": {"local_path": "doc_123.md"}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, MetadataFileName), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	_, err := Preflight(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
