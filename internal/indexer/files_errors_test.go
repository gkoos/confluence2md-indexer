package indexer

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// skipIfPermissionsAreNotEnforced skips a test that relies on a file mode denying access:
// Windows ignores the mode bits, and root reads whatever it likes.
func skipIfPermissionsAreNotEnforced(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file permissions are not enforced here")
	}
}

func TestScanFilesFailsOnAnUnreadableSubfolderInsteadOfListingLess(t *testing.T) {
	skipIfPermissionsAreNotEnforced(t)

	root := t.TempDir()
	writeSourceFile(t, root, "kept.md", "# Kept")
	writeSourceFile(t, root, "locked/inside.md", "# Inside")
	if err := os.Chmod(filepath.Join(root, "locked"), 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "locked"), 0o755) })

	// A listing without locked/inside.md would make the index delete that document.
	if _, _, err := scanFiles(root); err == nil {
		t.Fatal("an unreadable subfolder must stop the scan, not shrink the listing")
	}
}

func TestLoadFilesFailsOnAnUnreadableFileInsteadOfDroppingItsDocument(t *testing.T) {
	skipIfPermissionsAreNotEnforced(t)

	root := t.TempDir()
	writeSourceFile(t, root, "kept.md", "# Kept")
	locked := writeSourceFile(t, root, "locked.md", "# Locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })

	_, err := LoadSource(Source{ID: "notes", Type: TypeFiles, Folder: root}, 0, 0)
	if err == nil || !strings.Contains(err.Error(), "locked.md") {
		t.Fatalf("err = %v, want a failure naming the unreadable file", err)
	}
}

func TestLoadFilesFailsWhenNothingIsIndexable(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, "empty.md", "")
	writeSourceFile(t, root, "binary.txt", "a\x00b")

	// The preflight sees two text files and passes; the read finds nothing to index.
	if _, err := PreflightFiles(root); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	_, err := LoadSource(Source{ID: "notes", Type: TypeFiles, Folder: root}, 0, 0)
	if err == nil || !strings.Contains(err.Error(), "no indexable text files") {
		t.Fatalf("err = %v, want a corpus with no documents rejected", err)
	}
}

func TestReadConfinedRefusesPathsThatLeaveTheRoot(t *testing.T) {
	outside := t.TempDir()
	writeSourceFile(t, outside, "secret.md", "# Secret")

	root := t.TempDir()
	writeSourceFile(t, root, "inside.md", "# Inside")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "link.md")); err != nil {
		t.Skipf("cannot create symbolic links here: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-dir")); err != nil {
		t.Skipf("cannot create directory links here: %v", err)
	}

	confined, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer func() { _ = confined.Close() }()

	if content, err := readConfined(confined, "inside.md"); err != nil || !strings.Contains(string(content), "Inside") {
		t.Fatalf("a file inside the root must be readable: %q, %v", content, err)
	}

	// These are the paths a link swapped in after the scan would produce. Neither may
	// yield the outside file's content.
	for _, rel := range []string{"link.md", "linked-dir/secret.md"} {
		content, err := readConfined(confined, rel)
		if err == nil || strings.Contains(string(content), "Secret") {
			t.Fatalf("%s: read %q with err %v, want a refusal", rel, content, err)
		}
		if errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: a refused escape must not look like a vanished file", rel)
		}
	}
}

func TestReadConfinedSkipsOversizedFilesThatGrewAfterTheScan(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, "grew.txt", strings.Repeat("a", maxFileSize+1))

	confined, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open root: %v", err)
	}
	defer func() { _ = confined.Close() }()

	if _, err := readConfined(confined, "grew.txt"); !errors.Is(err, errSkipFile) {
		t.Fatalf("err = %v, want errSkipFile", err)
	}
}
