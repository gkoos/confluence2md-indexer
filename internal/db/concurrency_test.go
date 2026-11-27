package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOpenSetsWALAndBusyTimeoutOnEveryConnection(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()

	// Two connections held at once force the pool to open a second one, and
	// busy_timeout is a per-connection setting.
	first, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("first conn: %v", err)
	}
	defer func() { _ = first.Close() }()
	second, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("second conn: %v", err)
	}
	defer func() { _ = second.Close() }()

	for i, conn := range []*sql.Conn{first, second} {
		var mode string
		if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
			t.Fatalf("conn %d journal_mode: %v", i, err)
		}
		if mode != "wal" {
			t.Fatalf("conn %d journal_mode = %q, want wal", i, mode)
		}
		var timeout int
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatalf("conn %d busy_timeout: %v", i, err)
		}
		if timeout != busyTimeoutMillis {
			t.Fatalf("conn %d busy_timeout = %d, want %d", i, timeout, busyTimeoutMillis)
		}
	}
}

func TestSecondWriterWaitsForFirstInsteadOfFailing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	writerA, err := Open(path)
	if err != nil {
		t.Fatalf("open writer A: %v", err)
	}
	defer func() { _ = writerA.Close() }()
	if err := Migrate(ctx, writerA); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	writerB, err := Open(path)
	if err != nil {
		t.Fatalf("open writer B: %v", err)
	}
	defer func() { _ = writerB.Close() }()

	tx, err := writerA.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO runs (id, started_at, mode) VALUES ('a', 'now', 'full')`); err != nil {
		t.Fatalf("insert a: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var writeErr error
	go func() {
		defer wg.Done()
		_, writeErr = writerB.ExecContext(ctx, `INSERT INTO runs (id, started_at, mode) VALUES ('b', 'now', 'full')`)
	}()

	time.Sleep(300 * time.Millisecond)
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit a: %v", err)
	}
	wg.Wait()

	if writeErr != nil {
		t.Fatalf("second writer failed instead of waiting: %v", writeErr)
	}
}

func TestReaderIsNotBlockedByAnOpenWriteTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	writer, err := Open(path)
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	defer func() { _ = writer.Close() }()
	if err := Migrate(ctx, writer); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	defer func() { _ = reader.Close() }()

	tx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO runs (id, started_at, mode) VALUES ('a', 'now', 'full')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var count int
	if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs`).Scan(&count); err != nil {
		t.Fatalf("read during write: %v", err)
	}
	if count != 0 {
		t.Fatalf("reader saw %d uncommitted rows, want 0", count)
	}
}
