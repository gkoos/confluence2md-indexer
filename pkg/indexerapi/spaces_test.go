package indexerapi

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/db"
)

func TestSpacesListsDistinctNonEmptyKeysSorted(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "spaces.db")
	seedStatsIndex(t, dbPath)

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// A second page in ENG and a page without a space must not change the list.
	for _, doc := range []db.DocumentRecord{
		{ID: "p3", PageID: "p3", Title: "Gamma", LocalPath: "p3.md", SpaceKey: "ENG", ContentHash: "h3"},
		{ID: "p4", PageID: "p4", Title: "Notes", LocalPath: "p4.md", SpaceKey: "", ContentHash: "h4"},
	} {
		if _, _, err := db.UpsertDocumentWithChunks(context.Background(), database, doc, nil, ""); err != nil {
			t.Fatalf("seed %s: %v", doc.ID, err)
		}
	}
	_ = database.Close()

	got, err := Spaces(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Spaces: %v", err)
	}
	if want := []string{"ENG", "OPS"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("spaces = %v, want %v", got, want)
	}
}

func TestSpacesIsEmptyNotNilWithoutSpaces(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nospaces.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = database.Close()

	got, err := Spaces(context.Background(), dbPath)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("spaces = %v, err = %v, want an empty non-nil list", got, err)
	}
}

func TestSpacesReportsBusyWhileARebuildHoldsTheLock(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "busy.db")
	seedStatsIndex(t, dbPath)

	lock, err := db.AcquireLock(dbPath, true)
	if err != nil {
		t.Fatalf("take exclusive lock: %v", err)
	}
	defer func() { _ = lock.Release() }()

	if _, err := Spaces(context.Background(), dbPath); !errors.Is(err, ErrBusy) {
		t.Fatalf("error = %v, want ErrBusy", err)
	}
}

func TestSpacesRejectsAnEmptyDBPathAndAnUnreadableIndex(t *testing.T) {
	if _, err := Spaces(context.Background(), " "); err == nil {
		t.Fatal("an empty path must be an error")
	}
	if _, err := Spaces(context.Background(), filepath.Join(t.TempDir(), "absent.db")); err == nil {
		t.Fatal("a file that holds no index must be an error")
	}
}
