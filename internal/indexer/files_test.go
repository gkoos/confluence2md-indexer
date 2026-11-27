package indexer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeSourceFile(t *testing.T, root, rel, content string) string {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create folder for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	return path
}

func loadFilesSource(t *testing.T, root string) Corpus {
	t.Helper()

	corpus, err := LoadSource(Source{ID: "notes", Type: TypeFiles, Folder: root}, 0, 0)
	if err != nil {
		t.Fatalf("load files source: %v", err)
	}
	return corpus
}

func TestLoadFilesIdentifiesDocumentsByPath(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, "README.md", "---\ntitle: ignored\n---\n# Project\n\nbanana text")
	writeSourceFile(t, root, "docs/deep/a.MD", "no heading here, just words")
	writeSourceFile(t, root, "notes/todo.txt", "# not a heading\nplain text")
	writeSourceFile(t, root, "image.png", "\x89PNG not text")
	writeSourceFile(t, root, "config.yaml", "key: value")
	writeSourceFile(t, root, ".hidden/secret.md", "hidden folder")
	writeSourceFile(t, root, ".dotfile.md", "hidden file")

	corpus := loadFilesSource(t, root)

	var ids []string
	for _, doc := range corpus.Documents {
		ids = append(ids, doc.ID)
	}
	want := []string{"notes/README.md", "notes/docs/deep/a.MD", "notes/notes/todo.txt"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("document ids = %v, want %v", ids, want)
	}

	readme := corpus.Documents[0]
	if readme.PageID != "README.md" || readme.LocalPath != "README.md" || readme.SourceID != "notes" || readme.SourceType != TypeFiles {
		t.Fatalf("README identity = %+v", readme)
	}
	if readme.Title != "Project" {
		t.Fatalf("title = %q, want the first heading", readme.Title)
	}
	if strings.Contains(readme.Chunks[0].Text, "title: ignored") {
		t.Fatal("front matter must not be indexed")
	}
	if _, err := time.Parse(time.RFC3339, readme.ModifiedAt); err != nil {
		t.Fatalf("modified time %q is not RFC 3339: %v", readme.ModifiedAt, err)
	}
	if !strings.HasPrefix(readme.Chunks[0].ID, "notes/README.md:") {
		t.Fatalf("chunk id %q must start with the document id", readme.Chunks[0].ID)
	}

	if title := corpus.Documents[1].Title; title != "a" {
		t.Fatalf("a file without a heading takes its name: %q", title)
	}

	todo := corpus.Documents[2]
	if todo.Title != "todo" || todo.Chunks[0].Section != "" {
		t.Fatalf("plain text has no headings: title = %q, section = %q", todo.Title, todo.Chunks[0].Section)
	}
}

func TestLoadFilesLeavesOutWhatIsNotText(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, "good.md", "# Good\n\ntext")
	writeSourceFile(t, root, "binary.txt", "looks like text\x00but is not")
	writeSourceFile(t, root, "latin1.md", "caf\xe9 not utf-8")
	writeSourceFile(t, root, "empty.md", "")
	writeSourceFile(t, root, "blank.txt", " \n\t\n")
	writeSourceFile(t, root, "bom.md", "\ufeff# Bom Title\n\nbody")

	corpus := loadFilesSource(t, root)

	var titles []string
	for _, doc := range corpus.Documents {
		titles = append(titles, doc.Title)
	}
	if strings.Join(titles, ",") != "Bom Title,Good" {
		t.Fatalf("titles = %v, want the BOM file and the good file only", titles)
	}
	if corpus.Skipped != 4 {
		t.Fatalf("skipped = %d, want the binary, non-UTF-8, empty and blank files counted", corpus.Skipped)
	}
}

func TestLoadFilesSkipsFilesOverTheSizeLimit(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, "small.md", "# Small\n\ntext")
	writeSourceFile(t, root, "huge.txt", strings.Repeat("a", maxFileSize+1))

	corpus := loadFilesSource(t, root)

	if len(corpus.Documents) != 1 || corpus.Documents[0].Title != "Small" || corpus.Skipped != 1 {
		t.Fatalf("documents = %d, skipped = %d, want only the small file with the huge one counted", len(corpus.Documents), corpus.Skipped)
	}
}

func TestLoadFilesNeverFollowsSymbolicLinksOutOfTheFolder(t *testing.T) {
	outside := t.TempDir()
	writeSourceFile(t, outside, "secret.md", "# Secret\n\nmust not be indexed")

	root := t.TempDir()
	writeSourceFile(t, root, "inside.md", "# Inside\n\ntext")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "link.md")); err != nil {
		t.Skipf("cannot create symbolic links here: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-dir")); err != nil {
		t.Skipf("cannot create directory links here: %v", err)
	}

	corpus := loadFilesSource(t, root)

	for _, doc := range corpus.Documents {
		if strings.Contains(doc.Title, "Secret") || strings.Contains(doc.ID, "link") {
			t.Fatalf("a symbolic link was followed: %+v", doc)
		}
	}
	if len(corpus.Documents) != 1 || corpus.Skipped < 1 {
		t.Fatalf("documents = %d, skipped = %d, want the inside file and the links counted as skipped", len(corpus.Documents), corpus.Skipped)
	}
}

func TestLoadFilesContentHashFollowsTheContent(t *testing.T) {
	root := t.TempDir()
	path := writeSourceFile(t, root, "a.md", "# A\n\nfirst")

	first := loadFilesSource(t, root).Documents[0]
	again := loadFilesSource(t, root).Documents[0]
	if first.ContentHash != again.ContentHash {
		t.Fatal("the same content must hash the same")
	}

	if err := os.WriteFile(path, []byte("# A\n\nchanged"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if changed := loadFilesSource(t, root).Documents[0]; changed.ContentHash == first.ContentHash {
		t.Fatal("changed content must hash differently")
	}
}

func TestFirstHeadingIgnoresFencedCode(t *testing.T) {
	content := "```sh\n# a comment, not a title\n```\n\n# Real Title\n"
	if got := firstHeading(content); got != "Real Title" {
		t.Fatalf("firstHeading = %q, want Real Title", got)
	}
	if got := firstHeading("no headings\njust text"); got != "" {
		t.Fatalf("firstHeading = %q, want none", got)
	}
}

func TestPreflightFiles(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, "a.md", "# A")
	writeSourceFile(t, root, "sub/b.txt", "b")
	writeSourceFile(t, root, "skip.png", "x")

	summary, err := PreflightFiles(root)
	if err != nil || summary.PageCount != 2 || summary.MarkdownChecked != 2 || summary.MetadataPath != "" {
		t.Fatalf("summary = %+v, err = %v, want two files and no metadata path", summary, err)
	}

	if _, err := PreflightFiles(filepath.Join(root, "missing")); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing folder: err = %v", err)
	}
	if _, err := PreflightFiles(filepath.Join(root, "a.md")); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("a file instead of a folder: err = %v", err)
	}

	_, err = PreflightFiles(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), MetadataFileName) {
		t.Fatalf("an empty folder must be rejected with a hint about %s: %v", MetadataFileName, err)
	}
}
