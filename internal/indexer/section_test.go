package indexer

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeBreadcrumbCorpus writes a one-page corpus whose markdown is given.
func writeBreadcrumbCorpus(t *testing.T, markdown string) string {
	t.Helper()

	dir := t.TempDir()
	metadata := `{"pages":{"1":{"local_path":"page.md","title":"Deployment guide","space_key":"ENG",` +
		`"last_modified_at":"2026-01-10T00:00:00Z","source_url":"https://example.test/1"}}}`

	if err := os.WriteFile(filepath.Join(dir, MetadataFileName), []byte(metadata), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "page.md"), []byte(markdown), 0644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	return dir
}

func TestLoadDocumentsIndexesTheHeadingBreadcrumb(t *testing.T) {
	dir := writeBreadcrumbCorpus(t, "# Deployment\n\nintro text\n\n## Rollback\n\nkubectl rollout undo\n\n### Fast rollback\n\nskip the canary\n")

	docs, err := LoadDocuments(dir, DefaultChunkSize, DefaultChunkOverlap)
	if err != nil {
		t.Fatalf("load documents: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("documents = %d, want 1", len(docs))
	}

	sections := make([]string, 0, len(docs[0].Chunks))
	for _, chunk := range docs[0].Chunks {
		sections = append(sections, chunk.Section)
	}

	want := []string{"Deployment", "Deployment > Rollback", "Deployment > Rollback > Fast rollback"}
	if !reflect.DeepEqual(sections, want) {
		t.Fatalf("sections = %v, want %v", sections, want)
	}
}

func TestSplitSectionsResetsBreadcrumbOnAHigherHeading(t *testing.T) {
	sections := splitSections("# Deployment\n\ntext\n\n## Rollback\n\nsteps\n\n# Appendix\n\nmore\n")

	want := []contentSection{
		{Text: "# Deployment\n\ntext", Breadcrumb: "Deployment"},
		{Text: "## Rollback\n\nsteps", Breadcrumb: "Deployment > Rollback"},
		{Text: "# Appendix\n\nmore", Breadcrumb: "Appendix"},
	}

	if !reflect.DeepEqual(sections, want) {
		t.Fatalf("sections = %+v, want %+v", sections, want)
	}
}

func TestSplitSectionsKeepsTextBeforeTheFirstHeading(t *testing.T) {
	sections := splitSections("preamble text\n\n# Deployment\n\nbody\n")

	if len(sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(sections))
	}
	if sections[0].Breadcrumb != "" || !strings.Contains(sections[0].Text, "preamble") {
		t.Fatalf("first section = %+v, want a breadcrumb-free preamble", sections[0])
	}
	if sections[1].Breadcrumb != "Deployment" {
		t.Fatalf("second section breadcrumb = %q, want %q", sections[1].Breadcrumb, "Deployment")
	}
}

func TestChunkSectionsGivesEveryPieceTheBreadcrumb(t *testing.T) {
	sections := []contentSection{{Text: strings.Repeat("word ", 6), Breadcrumb: "Deployment > Rollback"}}

	drafts := chunkSections(sections, 12, 0)
	if len(drafts) < 2 {
		t.Fatalf("drafts = %d, want the text split into pieces", len(drafts))
	}
	for _, draft := range drafts {
		if draft.Section != "Deployment > Rollback" {
			t.Fatalf("draft section = %q, want the section breadcrumb", draft.Section)
		}
	}
}

func TestParseHeading(t *testing.T) {
	cases := map[string]struct {
		line  string
		level int
		title string
		ok    bool
	}{
		"h1":             {"# Deployment", 1, "Deployment", true},
		"h3 with spaces": {"###   Rollback  ", 3, "Rollback", true},
		"h6":             {"###### deep", 6, "deep", true},
		"deeper than h6": {"####### seven", 6, "seven", true},
		"bare hash":      {"#", 1, "", true},
		"body text":      {"text # not a heading", 0, "", false},
		"empty":          {"", 0, "", false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			level, title, ok := parseHeading(tc.line)
			if ok != tc.ok || level != tc.level || title != tc.title {
				t.Fatalf("parseHeading(%q) = (%d, %q, %t), want (%d, %q, %t)",
					tc.line, level, title, ok, tc.level, tc.title, tc.ok)
			}
		})
	}
}
