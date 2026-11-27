package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gkoos/confluence2md-indexer/internal/db"
	"github.com/gkoos/confluence2md-indexer/internal/indexer"
)

func writeLockFixture(t *testing.T) (folder, dbPath string) {
	t.Helper()
	dir := t.TempDir()

	metadata := `{"pages": {"1": {"local_path": "a.md", "title": "Alpha", "space_key": "ENG", "last_modified_at": "2026-01-15T12:00:00Z", "source_url": "https://example.test/1"}}}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# heading\n\nbanana text"), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	return dir, filepath.Join(dir, "index.db")
}

func shortenLockWait(t *testing.T) {
	t.Helper()
	previous := db.LockWait
	db.LockWait = 100 * time.Millisecond
	t.Cleanup(func() { db.LockWait = previous })
}

func TestRebuildRefusesToDeleteAnIndexInUse(t *testing.T) {
	shortenLockWait(t)
	folder, dbPath := writeLockFixture(t)
	ctx := context.Background()

	if _, err := Index(ctx, IndexRequest{Sources: indexer.SingleSource(folder), DBPath: dbPath}); err != nil {
		t.Fatalf("initial index: %v", err)
	}

	reader, err := db.AcquireLock(dbPath, false)
	if err != nil {
		t.Fatalf("reader lock: %v", err)
	}

	_, err = Index(ctx, IndexRequest{Sources: indexer.SingleSource(folder), DBPath: dbPath, Rebuild: true})
	if !errors.Is(err, db.ErrBusy) {
		t.Fatalf("rebuild while the index is in use: err = %v, want ErrBusy", err)
	}
	if _, statErr := os.Stat(dbPath); statErr != nil {
		t.Fatalf("a refused rebuild must leave the database in place: %v", statErr)
	}

	if err := reader.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := Index(ctx, IndexRequest{Sources: indexer.SingleSource(folder), DBPath: dbPath, Rebuild: true}); err != nil {
		t.Fatalf("rebuild after the reader left: %v", err)
	}
}

func TestQueriesWaitOutARebuild(t *testing.T) {
	shortenLockWait(t)
	folder, dbPath := writeLockFixture(t)
	ctx := context.Background()

	if _, err := Index(ctx, IndexRequest{Sources: indexer.SingleSource(folder), DBPath: dbPath}); err != nil {
		t.Fatalf("initial index: %v", err)
	}

	rebuild, err := db.AcquireLock(dbPath, true)
	if err != nil {
		t.Fatalf("rebuild lock: %v", err)
	}
	defer func() { _ = rebuild.Release() }()

	if _, err := Stats(ctx, dbPath); !errors.Is(err, db.ErrBusy) {
		t.Fatalf("stats during a rebuild: err = %v, want ErrBusy", err)
	}
}
