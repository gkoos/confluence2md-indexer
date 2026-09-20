package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/embedding"
	"github.com/gkoos/confluence2md-indexer/internal/embedding/embeddingtest"
)

func TestIndexAndStatsHappyPath(t *testing.T) {
	dir := t.TempDir()

	metadata := `{
  "pages": {
    "1": {
      "local_path": "a.md",
      "title": "Alpha",
      "space_key": "ENG",
      "last_modified_at": "2026-01-15T12:00:00Z",
      "source_url": "https://example.test/1"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# heading\n\nbanana text"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	dbPath := filepath.Join(dir, "confluence2md-index.db")
	idxResp, err := Index(context.Background(), IndexRequest{Folder: dir, DBPath: dbPath, Rebuild: false})
	if err != nil {
		t.Fatalf("index failed: %v", err)
	}
	if idxResp.PageCount != 1 {
		t.Fatalf("expected page count 1, got %d", idxResp.PageCount)
	}
	if idxResp.DBStats == nil || idxResp.DBStats.Documents == 0 {
		t.Fatalf("expected non-empty db stats")
	}

	statsResp, err := Stats(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("stats failed: %v", err)
	}
	if statsResp.Stats == nil || statsResp.Stats.Documents == 0 {
		t.Fatalf("expected documents > 0 from stats")
	}
}

func TestIndexRebuildRecreatesDB(t *testing.T) {
	dir := t.TempDir()

	metadata := `{
  "pages": {
    "1": {
      "local_path": "a.md",
      "title": "Alpha",
      "space_key": "ENG",
      "last_modified_at": "2026-01-15T12:00:00Z",
      "source_url": "https://example.test/1"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# heading\n\nbanana text"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	dbPath := filepath.Join(dir, "confluence2md-index.db")
	if _, err := Index(context.Background(), IndexRequest{Folder: dir, DBPath: dbPath, Rebuild: false}); err != nil {
		t.Fatalf("initial index failed: %v", err)
	}

	rebuildResp, err := Index(context.Background(), IndexRequest{Folder: dir, DBPath: dbPath, Rebuild: true})
	if err != nil {
		t.Fatalf("rebuild index failed: %v", err)
	}
	if rebuildResp.Inserted != 1 {
		t.Fatalf("expected rebuild to insert document again, got inserted=%d", rebuildResp.Inserted)
	}
	if rebuildResp.Skipped != 0 {
		t.Fatalf("expected rebuild skip count 0, got skipped=%d", rebuildResp.Skipped)
	}
}

func TestIndexValidatesInputs(t *testing.T) {
	_, err := Index(context.Background(), IndexRequest{Folder: "", DBPath: "db.sqlite"})
	if err == nil {
		t.Fatalf("expected error for empty folder")
	}

	_, err = Index(context.Background(), IndexRequest{Folder: ".", DBPath: ""})
	if err == nil {
		t.Fatalf("expected error for empty db path")
	}
}

func TestStatsValidatesDBPath(t *testing.T) {
	_, err := Stats(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error for empty db path")
	}
}

// TestIndexRecordsEmbeddingIdentity covers the identity bookkeeping end to end:
// the run records which provider produced the vectors, stats reports it, and
// switching provider re-embeds instead of keeping the previous space.
func TestIndexRecordsEmbeddingIdentity(t *testing.T) {
	dir := t.TempDir()

	metadata := `{
  "pages": {
    "1": {
      "local_path": "a.md",
      "title": "Alpha",
      "space_key": "ENG",
      "last_modified_at": "2026-01-15T12:00:00Z",
      "source_url": "https://example.test/1"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# heading\n\nbanana text"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	dbPath := filepath.Join(dir, "confluence2md-index.db")
	ctx := context.Background()

	first, err := Index(ctx, IndexRequest{Folder: dir, DBPath: dbPath})
	if err != nil {
		t.Fatalf("index failed: %v", err)
	}
	if first.EmbeddingName != "bow-local:fnv1a@256" {
		t.Fatalf("unexpected default embedding identity %q", first.EmbeddingName)
	}
	if first.EmbeddingCapability != embedding.CapabilityLexical {
		t.Fatalf("expected a lexical capability, got %q", first.EmbeddingCapability)
	}
	if first.EmbeddingDimension != embedding.DefaultBowDim {
		t.Fatalf("expected dimension %d, got %d", embedding.DefaultBowDim, first.EmbeddingDimension)
	}

	statsResp, err := Stats(ctx, dbPath)
	if err != nil {
		t.Fatalf("stats failed: %v", err)
	}
	if !statsResp.Stats.VectorReady {
		t.Fatal("expected a ready vector channel after indexing")
	}
	if statsResp.Stats.VectorName != first.EmbeddingName {
		t.Fatalf("expected stats identity %q, got %q", first.EmbeddingName, statsResp.Stats.VectorName)
	}
	if statsResp.Stats.VectorCapability != embedding.CapabilityLexical {
		t.Fatalf("expected capability %q, got %q", embedding.CapabilityLexical, statsResp.Stats.VectorCapability)
	}
	if len(statsResp.Stats.EmbeddingModels) != 1 {
		t.Fatalf("expected one embedding identity, got %v", statsResp.Stats.EmbeddingModels)
	}

	// Re-index with a different provider: content is unchanged, so this only
	// re-embeds because the identity changed.
	other := embeddingtest.New(16)
	second, err := Index(ctx, IndexRequest{Folder: dir, DBPath: dbPath, Provider: other})
	if err != nil {
		t.Fatalf("second index failed: %v", err)
	}
	if second.Skipped != 0 {
		t.Fatalf("expected re-embedding instead of skipping, got skipped=%d", second.Skipped)
	}
	if second.EmbeddingWrites == 0 {
		t.Fatal("expected vectors to be rewritten for the new identity")
	}

	statsResp, err = Stats(ctx, dbPath)
	if err != nil {
		t.Fatalf("stats failed: %v", err)
	}
	if statsResp.Stats.VectorName != other.Name() {
		t.Fatalf("expected stats identity %q, got %q", other.Name(), statsResp.Stats.VectorName)
	}
	if len(statsResp.Stats.EmbeddingModels) != 1 {
		t.Fatalf("expected no mixed identities, got %v", statsResp.Stats.EmbeddingModels)
	}
}
