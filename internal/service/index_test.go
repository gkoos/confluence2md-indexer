package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/embedding"
	"github.com/gkoos/confluence2md-indexer/internal/embedding/embeddingtest"
	"github.com/gkoos/confluence2md-indexer/internal/query"
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

// recordingProvider keeps the document texts the indexer asked to embed, which is the
// only place the context header is observable.
type recordingProvider struct {
	*embeddingtest.Provider
	documents []string
}

func (p *recordingProvider) Embed(ctx context.Context, kind embedding.Kind, texts []string) ([][]float32, error) {
	if kind == embedding.KindDocument {
		p.documents = append(p.documents, texts...)
	}

	return p.Provider.Embed(ctx, kind, texts)
}

func TestIndexEmbedsTheContextHeaderAndKeepsStoredTextRaw(t *testing.T) {
	dir := t.TempDir()

	metadata := `{
  "pages": {
    "1": {
      "local_path": "a.md",
      "title": "Deployment runbook",
      "space_key": "SRE",
      "last_modified_at": "2026-01-15T12:00:00Z",
      "source_url": "https://example.test/1"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# Rollback\n\nshort chunk"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	dbPath := filepath.Join(dir, "confluence2md-index.db")
	provider := &recordingProvider{Provider: embeddingtest.New(8)}

	_, err := Index(context.Background(), IndexRequest{
		Folder:   dir,
		DBPath:   dbPath,
		Provider: provider,
		Embedding: embedding.Options{Context: []string{
			embedding.ContextTitle, embedding.ContextSpace, embedding.ContextSection,
		}},
	})
	if err != nil {
		t.Fatalf("index failed: %v", err)
	}

	if len(provider.documents) == 0 {
		t.Fatal("expected the indexer to embed the page")
	}
	wantHeader := "title: Deployment runbook\nspace: SRE\nsection: Rollback\n\n"
	if !strings.HasPrefix(provider.documents[0], wantHeader) {
		t.Fatalf("embedding input = %q, want it to start with %q", provider.documents[0], wantHeader)
	}

	// Everything that is stored and returned stays raw: only the provider input is
	// decorated.
	response, err := Query(context.Background(), dbPath, query.Request{Text: "short", Mode: "lexical", TopK: 1})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(response.Results) != 1 {
		t.Fatalf("expected one result, got %d", len(response.Results))
	}
	if strings.Contains(response.Results[0].ChunkText, "title:") {
		t.Fatalf("stored chunk text was decorated: %q", response.Results[0].ChunkText)
	}
	if !strings.Contains(response.Results[0].ChunkText, "short chunk") {
		t.Fatalf("stored chunk text = %q, want the raw chunk", response.Results[0].ChunkText)
	}
}

func TestIndexRecordsTheContextInTheIdentity(t *testing.T) {
	dir := t.TempDir()

	metadata := `{
  "pages": {
    "1": {
      "local_path": "a.md",
      "title": "Deployment runbook",
      "space_key": "SRE",
      "last_modified_at": "2026-01-15T12:00:00Z",
      "source_url": "https://example.test/1"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# Rollback\n\nshort chunk"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	dbPath := filepath.Join(dir, "confluence2md-index.db")
	ctx := context.Background()

	withContext, err := Index(ctx, IndexRequest{
		Folder: dir,
		DBPath: dbPath,
		Embedding: embedding.Options{
			Provider: embedding.ProviderBowLocal,
			// Named out of order on purpose: the identity has to be canonical.
			Context: []string{embedding.ContextSpace, embedding.ContextTitle},
		},
	})
	if err != nil {
		t.Fatalf("index failed: %v", err)
	}
	if want := "bow-local:fnv1a@256+ctx:title,space"; withContext.EmbeddingName != want {
		t.Fatalf("identity = %q, want %q", withContext.EmbeddingName, want)
	}

	// Dropping the selection changes the identity, so the next run re-embeds instead of
	// keeping vectors from the decorated space, without needing --rebuild.
	plain, err := Index(ctx, IndexRequest{
		Folder:    dir,
		DBPath:    dbPath,
		Embedding: embedding.Options{Provider: embedding.ProviderBowLocal},
	})
	if err != nil {
		t.Fatalf("second index failed: %v", err)
	}
	if plain.EmbeddingName != "bow-local:fnv1a@256" {
		t.Fatalf("identity = %q, want the undecorated one", plain.EmbeddingName)
	}
	if plain.Skipped != 0 || plain.EmbeddingWrites == 0 {
		t.Fatalf("expected a re-embed, got skipped=%d writes=%d", plain.Skipped, plain.EmbeddingWrites)
	}
}

// TestIndexReembedsWhenTheHeaderMetadataChanges covers the stale-vector case: with title
// context on, a retitled page keeps its markdown, so the content hash does not move. The
// embedded header does, and the vectors have to follow; metadata the header does not
// carry must still be refreshed without re-embedding anything.
func TestIndexReembedsWhenTheHeaderMetadataChanges(t *testing.T) {
	dir := t.TempDir()

	writeMetadata := func(title, author string) {
		t.Helper()

		metadata := fmt.Sprintf(`{
  "pages": {
    "1": {
      "local_path": "a.md",
      "title": %q,
      "space_key": "SRE",
      "created_by_name": %q,
      "last_modified_at": "2026-01-15T12:00:00Z",
      "source_url": "https://example.test/1"
    }
  }
}`, title, author)
		if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
			t.Fatalf("write metadata: %v", err)
		}
	}

	writeMetadata("Deployment runbook", "Ada Lovelace")
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# Rollback\n\nshort chunk"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	dbPath := filepath.Join(dir, "confluence2md-index.db")
	provider := &recordingProvider{Provider: embeddingtest.New(8)}
	request := IndexRequest{
		Folder:    dir,
		DBPath:    dbPath,
		Provider:  provider,
		Embedding: embedding.Options{Context: []string{embedding.ContextTitle}},
	}

	first, err := Index(context.Background(), request)
	if err != nil {
		t.Fatalf("first index failed: %v", err)
	}
	if first.EmbeddingWrites == 0 || len(provider.documents) != 1 {
		t.Fatalf("expected the page to be embedded, got writes=%d inputs=%d", first.EmbeddingWrites, len(provider.documents))
	}
	if !strings.Contains(provider.documents[0], "title: Deployment runbook") {
		t.Fatalf("embedding input = %q, want the title header", provider.documents[0])
	}

	// Only the title moves; the markdown is untouched.
	writeMetadata("Deployment runbook v2", "Ada Lovelace")
	provider.documents = nil

	second, err := Index(context.Background(), request)
	if err != nil {
		t.Fatalf("second index failed: %v", err)
	}
	if second.Updated != 1 || second.EmbeddingWrites == 0 {
		t.Fatalf("expected a rewrite and a re-embed, got updated=%d writes=%d", second.Updated, second.EmbeddingWrites)
	}
	if len(provider.documents) != 1 || !strings.Contains(provider.documents[0], "title: Deployment runbook v2") {
		t.Fatalf("embedding inputs = %v, want the new title header", provider.documents)
	}

	// A metadata change the header does not carry keeps the vectors.
	writeMetadata("Deployment runbook v2", "Grace Hopper")
	provider.documents = nil

	third, err := Index(context.Background(), request)
	if err != nil {
		t.Fatalf("third index failed: %v", err)
	}
	if third.MetadataUpdates != 1 || third.EmbeddingWrites != 0 || len(provider.documents) != 0 {
		t.Fatalf("expected a metadata refresh only, got metadata=%d writes=%d inputs=%d",
			third.MetadataUpdates, third.EmbeddingWrites, len(provider.documents))
	}
}
