package indexerapi

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/db"
)

// seedStatsIndex writes a small index with crawler metadata and one recorded crawl, so
// the counts, coverage and freshness blocks all carry values.
func seedStatsIndex(t *testing.T, dbPath string) {
	t.Helper()

	ctx := context.Background()
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	if err := db.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	docs := []db.DocumentRecord{
		{
			ID: "p1", PageID: "p1", Title: "Alpha", LocalPath: "p1.md", SpaceKey: "ENG",
			SourceURL: "https://example.test/p1", LastModifiedAt: "2026-01-01T00:00:00Z",
			CreatedByName: "Ada Lovelace", ModifiedByName: "Ada Lovelace",
			Host: "a.test", Depth: 0, IsSeed: true, Attachments: 1, ContentHash: "h1",
		},
		{
			ID: "p2", PageID: "p2", Title: "Beta", LocalPath: "p2.md", SpaceKey: "OPS",
			SourceURL: "https://example.test/p2", LastModifiedAt: "2026-01-02T00:00:00Z",
			CreatedByName: "Grace Hopper", ModifiedByName: "Ada Lovelace",
			Host: "b.test", Depth: 1, ContentHash: "h2",
		},
	}
	for _, doc := range docs {
		chunk := db.ChunkRecord{ID: doc.ID + ":000000", ChunkIndex: 0, Text: "shared text", ChunkHash: "c-" + doc.ID}
		if _, _, err := db.UpsertDocumentWithChunks(ctx, database, doc, []db.ChunkRecord{chunk}, ""); err != nil {
			t.Fatalf("seed doc %s: %v", doc.ID, err)
		}
	}

	run, err := db.BeginRun(ctx, database, "incremental")
	if err != nil {
		t.Fatalf("begin run: %v", err)
	}
	if err := db.CompleteRun(ctx, database, run.ID); err != nil {
		t.Fatalf("complete run: %v", err)
	}
	if err := db.RecordCorpusSnapshot(ctx, database, run.ID, db.CorpusSnapshot{
		RunID:       run.ID,
		StartedAt:   "2026-05-22T10:00:00Z",
		CompletedAt: "2026-05-22T10:20:03Z",
		SucceededAt: "2026-05-22T10:20:03Z",
		Mode:        "updates",
		SeedCount:   1,
		PageCount:   2,
	}); err != nil {
		t.Fatalf("record corpus snapshot: %v", err)
	}
}

func jsonKeys(t *testing.T, raw []byte) []string {
	t.Helper()

	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode json object: %v", err)
	}

	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys
}

func TestStatsReportsTheIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "stats.db")
	seedStatsIndex(t, dbPath)

	resp, err := Stats(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if resp.SchemaVersion != OutputSchemaVersion || resp.Command != "stats" {
		t.Fatalf("envelope = %q/%q, want %q/stats", resp.SchemaVersion, resp.Command, OutputSchemaVersion)
	}
	if !filepath.IsAbs(resp.DBPath) || resp.DBPath != filepath.Clean(dbPath) {
		t.Fatalf("dbPath = %q, want the absolute path of %q", resp.DBPath, dbPath)
	}

	// Freshness and coverage are the reason this API exists: the MCP server reads them
	// without shelling out to the CLI.
	if resp.Stats.Runs != 1 || resp.Stats.Documents != 2 || resp.Stats.Chunks != 2 {
		t.Fatalf("counts = %+v, want one run, two documents and two chunks", resp.Stats)
	}
	if resp.Stats.VectorReady || resp.Stats.VectorName != "" || resp.Stats.Embeddings != 0 {
		t.Fatalf("vector state = %+v, want an index without embeddings", resp.Stats)
	}

	coverage := resp.Stats.Metadata
	if coverage == nil {
		t.Fatal("expected the metadata coverage block")
	}
	if coverage.Documents != 2 || coverage.WithAuthors != 2 || coverage.Seeds != 1 || coverage.Nested != 1 || coverage.Hosts != 2 {
		t.Fatalf("coverage = %+v, want the stored metadata counted", coverage)
	}
	if coverage.WithAttachments != 1 {
		t.Fatalf("coverage = %+v, want the attachment counted", coverage)
	}

	corpus := resp.Stats.Corpus
	if corpus == nil {
		t.Fatal("expected the corpus block")
	}
	if corpus.Mode != "updates" || corpus.PageCount != 2 || corpus.SeedCount != 1 {
		t.Fatalf("corpus = %+v, want the crawl the index was built from", corpus)
	}
	if corpus.CompletedAt != "2026-05-22T10:20:03Z" || corpus.IndexedAt == "" {
		t.Fatalf("corpus = %+v, want both timestamps so staleness is comparable", corpus)
	}
}

// TestStatsPayloadMatchesTheCLIShape pins the field names, because the exported response
// is the documented stats --json document.
func TestStatsPayloadMatchesTheCLIShape(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "payload.db")
	seedStatsIndex(t, dbPath)

	resp, err := Stats(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	if got, want := jsonKeys(t, encoded), []string{"command", "dbPath", "schemaVersion", "stats"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("envelope keys = %v, want %v", got, want)
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}

	wantStats := []string{
		"chunks", "corpus", "documents", "embeddingModels", "embeddings",
		"metadata", "runs", "vectorCapability", "vectorName", "vectorReady",
	}
	if got := jsonKeys(t, envelope["stats"]); !reflect.DeepEqual(got, wantStats) {
		t.Fatalf("stats keys = %v, want %v", got, wantStats)
	}
}

func TestStatsRejectsAnEmptyDBPath(t *testing.T) {
	_, err := Stats(context.Background(), "   ")
	if err == nil || err.Error() != "stats requires a non-empty --db path" {
		t.Fatalf("error = %v, want the stats path validation", err)
	}
}

func TestStatsRefusesAFutureSchema(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "future.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := database.ExecContext(ctx, "PRAGMA user_version = 2;"); err != nil {
		t.Fatalf("stamp schema version: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	_, err = Stats(ctx, dbPath)
	want := "database schema 2 is newer than the schema this build writes (1); upgrade confluence2md-indexer"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestStatsRefusesAnEmptyIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "empty.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	_, err = Stats(context.Background(), dbPath)
	want := "this file holds no index yet; build one with: confluence2md-indexer index <folder>"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestStatsRefusesAnUnversionedIndex(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := database.ExecContext(ctx, "CREATE TABLE legacy (id TEXT);"); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	_, err = Stats(ctx, dbPath)
	want := "database schema 0 is not the schema this build writes (1); rebuild the index with: confluence2md-indexer index <folder> --rebuild"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}
