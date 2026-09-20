package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// newTestDatabase opens a migrated database in a temporary directory.
func newTestDatabase(t *testing.T) *sql.DB {
	t.Helper()

	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if err := Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return database
}

func TestMigrateStampsSchemaVersion(t *testing.T) {
	database := newTestDatabase(t)

	version, err := schemaVersionOf(context.Background(), database)
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("user_version = %d, want %d", version, schemaVersion)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	database := newTestDatabase(t)

	if err := Migrate(context.Background(), database); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestVerifyAcceptsCurrentSchema(t *testing.T) {
	database := newTestDatabase(t)

	if err := Verify(context.Background(), database); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyRefusesUnversionedDatabase(t *testing.T) {
	// A database written before the schema was versioned holds tables but carries
	// no version stamp, and this build refuses it instead of upgrading in place.
	database := newTestDatabase(t)
	if _, err := database.Exec("PRAGMA user_version = 0;"); err != nil {
		t.Fatalf("reset schema version: %v", err)
	}

	err := Verify(context.Background(), database)
	if err == nil || !strings.Contains(err.Error(), "--rebuild") {
		t.Fatalf("Verify error = %v, want rebuild guidance", err)
	}

	err = Migrate(context.Background(), database)
	if err == nil || !strings.Contains(err.Error(), "--rebuild") {
		t.Fatalf("Migrate error = %v, want rebuild guidance", err)
	}
}

func TestVerifyRefusesNewerSchema(t *testing.T) {
	database := newTestDatabase(t)
	if _, err := database.Exec("PRAGMA user_version = 99;"); err != nil {
		t.Fatalf("set schema version: %v", err)
	}

	err := Verify(context.Background(), database)
	if err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("error = %v, want a newer-schema message", err)
	}
}

func TestVerifyReportsAFileWithoutAnIndex(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	err = Verify(context.Background(), database)
	if err == nil || !strings.Contains(err.Error(), "no index yet") {
		t.Fatalf("error = %v, want a no-index message", err)
	}
}
