package indexer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDocumentsBuildsChunksAndStripsFrontMatter(t *testing.T) {
	dir := t.TempDir()
	metadata := `{
  "pages": {
    "1": {"local_path": "a.md", "title": "Doc A"}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, MetadataFileName), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	markdown := `---
page_id: "1"
title: "Doc A"
---

# Heading
This is body content that should be chunked.`
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(markdown), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	docs, err := LoadDocuments(dir, 40, 10)
	if err != nil {
		t.Fatalf("load documents: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc got %d", len(docs))
	}
	if len(docs[0].Chunks) == 0 {
		t.Fatalf("expected chunks")
	}
	if strings.Contains(docs[0].Chunks[0].Text, "page_id") {
		t.Fatalf("front matter should not appear in chunk text")
	}
}
