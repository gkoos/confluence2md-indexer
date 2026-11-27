package db

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSharedLocksCoexist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	first, err := acquireLock(path, false, time.Second)
	if err != nil {
		t.Fatalf("first shared: %v", err)
	}
	defer func() { _ = first.Release() }()

	second, err := acquireLock(path, false, time.Second)
	if err != nil {
		t.Fatalf("second shared: %v", err)
	}
	defer func() { _ = second.Release() }()
}

func TestExclusiveLockExcludesEveryoneElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	shared, err := acquireLock(path, false, time.Second)
	if err != nil {
		t.Fatalf("shared: %v", err)
	}
	if _, err := acquireLock(path, true, 100*time.Millisecond); !errors.Is(err, ErrBusy) {
		t.Fatalf("exclusive while shared held: err = %v, want ErrBusy", err)
	}
	if err := shared.Release(); err != nil {
		t.Fatalf("release shared: %v", err)
	}

	exclusive, err := acquireLock(path, true, time.Second)
	if err != nil {
		t.Fatalf("exclusive after release: %v", err)
	}
	if _, err := acquireLock(path, false, 100*time.Millisecond); !errors.Is(err, ErrBusy) {
		t.Fatalf("shared while exclusive held: err = %v, want ErrBusy", err)
	}
	if _, err := acquireLock(path, true, 100*time.Millisecond); !errors.Is(err, ErrBusy) {
		t.Fatalf("second exclusive: err = %v, want ErrBusy", err)
	}
	if err := exclusive.Release(); err != nil {
		t.Fatalf("release exclusive: %v", err)
	}

	again, err := acquireLock(path, true, time.Second)
	if err != nil {
		t.Fatalf("exclusive after release: %v", err)
	}
	_ = again.Release()
}

func TestWaiterGetsTheLockOnceItIsReleased(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	held, err := acquireLock(path, true, time.Second)
	if err != nil {
		t.Fatalf("exclusive: %v", err)
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = held.Release()
	}()

	waiter, err := acquireLock(path, false, 5*time.Second)
	if err != nil {
		t.Fatalf("waiter should have got the lock after release: %v", err)
	}
	_ = waiter.Release()
}

func TestReleaseOnNilLockIsSafe(t *testing.T) {
	var lock *Lock
	if err := lock.Release(); err != nil {
		t.Fatalf("nil release: %v", err)
	}
}

func TestAcquireLockRejectsEmptyPath(t *testing.T) {
	if _, err := AcquireLock("  ", false); err == nil {
		t.Fatal("expected an error for an empty path")
	}
}

// TestLockAcrossProcesses re-runs the test binary as the other process: the lock
// must hold between processes, not just between handles in one process.
func TestLockAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	cmd := exec.Command(os.Args[0], "-test.run=TestLockHelperProcess")
	cmd.Env = append(os.Environ(), "DB_LOCK_HELPER_PATH="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})

	// The helper prints "locked" once it holds the exclusive lock.
	scanner := bufio.NewScanner(stdout)
	ready := make(chan bool, 1)
	go func() {
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == "locked" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("helper exited without taking the lock")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("helper never took the lock")
	}

	if _, err := acquireLock(path, false, 150*time.Millisecond); !errors.Is(err, ErrBusy) {
		t.Fatalf("shared while another process holds exclusive: err = %v, want ErrBusy", err)
	}

	_ = stdin.Close()
	_ = cmd.Wait()

	lock, err := acquireLock(path, true, 5*time.Second)
	if err != nil {
		t.Fatalf("lock after the other process exited: %v", err)
	}
	_ = lock.Release()
}

func TestLockHelperProcess(t *testing.T) {
	path := os.Getenv("DB_LOCK_HELPER_PATH")
	if path == "" {
		t.Skip("only runs as the helper for TestLockAcrossProcesses")
	}

	lock, err := acquireLock(path, true, time.Second)
	if err != nil {
		os.Exit(2)
	}
	defer func() { _ = lock.Release() }()

	_, _ = os.Stdout.WriteString("locked\n")
	// Hold the lock until the parent closes our stdin.
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}
