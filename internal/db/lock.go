package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const lockPollInterval = 25 * time.Millisecond

// LockWait is how long AcquireLock waits for a contended lock. It is a variable so
// tests can shorten it.
var LockWait = busyTimeoutMillis * time.Millisecond

// Lock is a cross-process lock on one index, held through a sidecar file named
// after the database. SQLite's own locks cannot protect deleting the database
// file, so rebuild takes it exclusively and every other user takes it shared.
type Lock struct {
	file *os.File
}

// AcquireLock waits up to the busy timeout for the lock on the index at dbPath.
// Query, stats and incremental indexing take it shared; a rebuild takes it
// exclusive. A timeout returns an error wrapping ErrBusy.
func AcquireLock(dbPath string, exclusive bool) (*Lock, error) {
	return acquireLock(dbPath, exclusive, LockWait)
}

func acquireLock(dbPath string, exclusive bool, timeout time.Duration) (*Lock, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, fmt.Errorf("db path is empty")
	}
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("resolve db path %q: %w", dbPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return nil, fmt.Errorf("create db parent directory: %w", err)
	}

	lockPath := absPath + ".lock"
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open index lock file: %w", err)
	}

	kind := "shared"
	if exclusive {
		kind = "exclusive"
	}

	deadline := time.Now().Add(timeout)
	for {
		locked, err := tryLock(file, exclusive)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("lock %s: %w", lockPath, err)
		}
		if locked {
			return &Lock{file: file}, nil
		}
		if !time.Now().Before(deadline) {
			_ = file.Close()
			return nil, fmt.Errorf("%w: %s lock on %s was not available within %s", ErrBusy, kind, lockPath, timeout)
		}
		time.Sleep(lockPollInterval)
	}
}

// Release drops the lock. It is safe to call on a nil Lock.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	unlockErr := unlock(file)
	if err := file.Close(); err != nil && unlockErr == nil {
		return err
	}
	return unlockErr
}
