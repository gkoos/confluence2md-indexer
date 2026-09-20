package db

import (
	"context"
	"database/sql"
	"reflect"
	"sort"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/embedding"
	"github.com/gkoos/confluence2md-indexer/internal/embedding/embeddingtest"
)

func intPointer(value int) *int { return &value }

// seedFilterCorpus stores three documents that differ in every metadata filter the
// query layer exposes.
func seedFilterCorpus(t *testing.T, database *sql.DB) {
	t.Helper()

	docs := []DocumentRecord{
		{
			ID: "1", PageID: "1", Title: "Seed page", LocalPath: "1.md", SpaceKey: "OPS",
			SourceURL: "https://example.test/1", LastModifiedAt: "2026-05-20T10:00:00Z",
			ContentHash: "h-1", MetadataHash: "m-1", Host: "a.test", Depth: 0, IsSeed: true,
			CreatedByName: "Ada Lovelace", ModifiedByName: "Ada Lovelace", Attachments: 2,
		},
		{
			ID: "2", PageID: "2", Title: "Child page", LocalPath: "2.md", SpaceKey: "OPS",
			SourceURL: "https://example.test/2", LastModifiedAt: "2025-01-01T10:00:00Z",
			ContentHash: "h-2", MetadataHash: "m-2", Host: "a.test", Depth: 1, IsSeed: false,
			CreatedByName: "Grace Hopper", ModifiedByName: "Ada Lovelace", Attachments: 0,
		},
		{
			ID: "3", PageID: "3", Title: "Other space", LocalPath: "3.md", SpaceKey: "ENG",
			SourceURL: "https://example.test/3", LastModifiedAt: "2026-06-01T10:00:00Z",
			ContentHash: "h-3", MetadataHash: "m-3", Host: "b.test", Depth: 2, IsSeed: false,
			CreatedByName: "Ada Lovelace", ModifiedByName: "Grace Hopper", Attachments: 1,
		},
	}

	for _, doc := range docs {
		chunk := ChunkRecord{ID: doc.ID + ":000000", ChunkIndex: 0, Text: "shared term", ChunkHash: "c-" + doc.ID}
		seedSearchDocument(t, database, doc, chunk)
	}
}

func candidateIDs(candidates []Candidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.DocumentID)
	}
	sort.Strings(ids)

	return ids
}

func TestSearchLexicalNarrowsByMetadata(t *testing.T) {
	database := newTestDatabase(t)
	seedFilterCorpus(t, database)
	ctx := context.Background()

	cases := map[string]struct {
		filters SearchFilters
		want    []string
	}{
		"no filters":         {SearchFilters{}, []string{"1", "2", "3"}},
		"space":              {SearchFilters{SpaceKey: "ENG"}, []string{"3"}},
		"spaces":             {SearchFilters{Spaces: []string{"OPS", "ENG"}}, []string{"1", "2", "3"}},
		"host":               {SearchFilters{Host: "b.test"}, []string{"3"}},
		"author either role": {SearchFilters{Author: "ada lovelace"}, []string{"1", "2", "3"}},
		"created by":         {SearchFilters{CreatedBy: "grace hopper"}, []string{"2"}},
		"modified by":        {SearchFilters{ModifiedBy: "grace hopper"}, []string{"3"}},
		"depth at least one": {SearchFilters{DepthMin: intPointer(1)}, []string{"2", "3"}},
		"depth at most one":  {SearchFilters{DepthMax: intPointer(1)}, []string{"1", "2"}},
		"seed only":          {SearchFilters{SeedOnly: true}, []string{"1"}},
		"has attachments":    {SearchFilters{HasAttachments: true}, []string{"1", "3"}},
		"updated since":      {SearchFilters{UpdatedSince: "2026-01-01T00:00:00Z"}, []string{"1", "3"}},
		"page id":            {SearchFilters{PageID: "2"}, []string{"2"}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			hits, err := SearchLexical(ctx, database, "shared", tc.filters)
			if err != nil {
				t.Fatalf("lexical search: %v", err)
			}
			if got := candidateIDs(hits); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("document ids = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSearchVectorNarrowsByMetadata(t *testing.T) {
	database := newTestDatabase(t)
	seedFilterCorpus(t, database)
	ctx := context.Background()

	provider := embeddingtest.New(8)
	texts := []string{"shared term", "shared term", "shared term"}
	vectors, err := provider.Embed(ctx, embedding.KindDocument, texts)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	records := make([]EmbeddingRecord, 0, len(vectors))
	for i, vector := range vectors {
		records = append(records, EmbeddingRecord{
			ChunkID:   []string{"1:000000", "2:000000", "3:000000"}[i],
			Name:      provider.Name(),
			Dimension: len(vector),
			Vector:    vector,
		})
	}
	if _, err := UpsertEmbeddings(ctx, database, records); err != nil {
		t.Fatalf("upsert embeddings: %v", err)
	}

	queryVector := vectors[0]

	cases := map[string]struct {
		filters SearchFilters
		want    []string
	}{
		"no filters":  {SearchFilters{}, []string{"1", "2", "3"}},
		"space":       {SearchFilters{SpaceKey: "OPS"}, []string{"1", "2"}},
		"author":      {SearchFilters{Author: "Grace Hopper"}, []string{"2", "3"}},
		"seed only":   {SearchFilters{SeedOnly: true}, []string{"1"}},
		"host":        {SearchFilters{Host: "b.test"}, []string{"3"}},
		"depth range": {SearchFilters{DepthMin: intPointer(1), DepthMax: intPointer(1)}, []string{"2"}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			hits, err := SearchVector(ctx, database, queryVector, tc.filters)
			if err != nil {
				t.Fatalf("vector search: %v", err)
			}
			if got := candidateIDs(hits); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("document ids = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDocumentMetadataCoverageCountsStoredValues(t *testing.T) {
	database := newTestDatabase(t)
	seedFilterCorpus(t, database)

	// A page with links and no author exercises the remaining branches.
	seedSearchDocument(t, database, DocumentRecord{
		ID: "4", PageID: "4", Title: "Linked page", LocalPath: "4.md", SpaceKey: "OPS",
		SourceURL: "https://a.test/4", LastModifiedAt: "2026-06-01T10:00:00Z",
		ContentHash: "h-4", MetadataHash: "m-4", LinkIn: 3, LinkOut: 1,
	}, ChunkRecord{ID: "4:000000", ChunkIndex: 0, Text: "shared term", ChunkHash: "c-4"})

	coverage, err := DocumentMetadataCoverage(context.Background(), database)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}

	want := MetadataCoverage{
		Documents:       4,
		WithAuthors:     3,
		WithLinks:       1,
		WithAttachments: 2,
		WithComments:    0,
		Seeds:           1,
		Nested:          2,
		Hosts:           2,
	}
	if *coverage != want {
		t.Fatalf("coverage = %+v, want %+v", *coverage, want)
	}
}

func TestLatestCorpusSnapshotReportsTheNewestRun(t *testing.T) {
	database := newTestDatabase(t)
	ctx := context.Background()

	snapshot, err := LatestCorpusSnapshot(ctx, database)
	if err != nil {
		t.Fatalf("latest snapshot: %v", err)
	}
	if snapshot != nil {
		t.Fatalf("snapshot = %+v, want nil before any run records one", snapshot)
	}

	first, err := BeginRun(ctx, database, "rebuild")
	if err != nil {
		t.Fatalf("begin first run: %v", err)
	}
	if err := RecordCorpusSnapshot(ctx, database, first.ID, CorpusSnapshot{
		StartedAt: "2026-05-01T10:00:00Z", PageCount: 3, Mode: "full",
	}); err != nil {
		t.Fatalf("record first snapshot: %v", err)
	}
	if err := CompleteRun(ctx, database, first.ID); err != nil {
		t.Fatalf("complete first run: %v", err)
	}

	second, err := BeginRun(ctx, database, "incremental")
	if err != nil {
		t.Fatalf("begin second run: %v", err)
	}
	if err := RecordCorpusSnapshot(ctx, database, second.ID, CorpusSnapshot{
		StartedAt:   "2026-06-01T10:00:00Z",
		CompletedAt: "2026-06-01T10:05:00Z",
		PageCount:   5,
		SeedCount:   2,
		Mode:        "updates",
	}); err != nil {
		t.Fatalf("record second snapshot: %v", err)
	}
	if err := CompleteRun(ctx, database, second.ID); err != nil {
		t.Fatalf("complete second run: %v", err)
	}

	latest, err := LatestCorpusSnapshot(ctx, database)
	if err != nil {
		t.Fatalf("latest snapshot: %v", err)
	}
	if latest == nil {
		t.Fatal("expected the most recent snapshot")
	}
	if latest.RunID != second.ID || latest.PageCount != 5 || latest.Mode != "updates" {
		t.Fatalf("snapshot = %+v, want the second run's crawl", latest)
	}
	if latest.IndexedAt == "" {
		t.Fatal("IndexedAt must report when the run started, so staleness is comparable")
	}
}

func TestFilterClausesIgnoresBlankValues(t *testing.T) {
	where, args := filterClauses(SearchFilters{SpaceKey: "  ", Spaces: []string{"", " "}, Author: " ", Host: "\t"})

	if len(where) != 1 || len(args) != 0 {
		t.Fatalf("clauses = %v (args %v), want only the placeholder clause", where, args)
	}
}
