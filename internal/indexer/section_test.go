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

func TestFenceRunDetection(t *testing.T) {
	cases := map[string]struct {
		trim   string
		char   byte
		count  int
		isOpen bool
	}{
		"backtick fence":        {"```", '`', 3, true},
		"fence with info":       {"````sh", '`', 4, true},
		"tilde fence":           {"~~~", '~', 3, true},
		"tilde fence with info": {"~~~text", '~', 3, true},
		"too short":             {"``", 0, 0, false},
		"inline code":           {"`one`", 0, 0, false},
		"backtick inside info":  {"```a`b", 0, 0, false},
		"heading":               {"# Heading", 0, 0, false},
		"prose":                 {"prose", 0, 0, false},
		"empty":                 {"", 0, 0, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			char, count, ok := fenceRun(tc.trim)
			if ok != tc.isOpen || char != tc.char || count != tc.count {
				t.Fatalf("fenceRun(%q) = (%q, %d, %t), want (%q, %d, %t)",
					tc.trim, char, count, ok, tc.char, tc.count, tc.isOpen)
			}
		})
	}
}

func TestFenceTrackerFollowsOpenAndClose(t *testing.T) {
	cases := map[string]struct {
		lines    []string
		delimits []bool
		open     bool
	}{
		"opened then closed": {
			lines:    []string{"```sh", "# not a heading", "```"},
			delimits: []bool{true, false, true},
			open:     false,
		},
		"content while open": {
			lines:    []string{"```", "## also code", "# comment"},
			delimits: []bool{true, false, false},
			open:     true,
		},
		"different marker does not close": {
			lines:    []string{"```", "~~~", "# still code"},
			delimits: []bool{true, false, false},
			open:     true,
		},
		"shorter run does not close": {
			lines:    []string{"````", "```", "# still code"},
			delimits: []bool{true, false, false},
			open:     true,
		},
		"longer run closes": {
			lines:    []string{"````", "`````"},
			delimits: []bool{true, true},
			open:     false,
		},
		"closing fence with text is content": {
			lines:    []string{"```", "``` end", "# still code"},
			delimits: []bool{true, false, false},
			open:     true,
		},
		"reopened after closing": {
			lines:    []string{"```", "```", "# a heading again"},
			delimits: []bool{true, true, false},
			open:     false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fence := &fenceTracker{}
			for i, line := range tc.lines {
				got := fence.observe(line)
				if got != tc.delimits[i] {
					t.Fatalf("observe(%q) = %t, want %t", line, got, tc.delimits[i])
				}
			}
			if fence.open() != tc.open {
				t.Fatalf("open = %t, want %t", fence.open(), tc.open)
			}
		})
	}
}

func TestSplitSectionsIgnoresHeadingsInsideFences(t *testing.T) {
	content := "# Deployment\n\nprose\n\n```sh\n# install steps\nkubectl apply -f deploy.yaml\n```\n\nmore prose\n\n## Rollback\n\nundo\n"

	want := []contentSection{
		{
			Text:       "# Deployment\n\nprose\n\n```sh\n# install steps\nkubectl apply -f deploy.yaml\n```\n\nmore prose",
			Breadcrumb: "Deployment",
		},
		{Text: "## Rollback\n\nundo", Breadcrumb: "Deployment > Rollback"},
	}

	if got := splitSections(content); !reflect.DeepEqual(got, want) {
		t.Fatalf("sections = %+v, want %+v", got, want)
	}
}

func TestSplitSectionsHandlesTildeFences(t *testing.T) {
	content := "~~~yaml\n# not a heading\nkey: value\n~~~\n\n## Real\n\nx\n"

	want := []contentSection{
		{Text: "~~~yaml\n# not a heading\nkey: value\n~~~", Breadcrumb: ""},
		{Text: "## Real\n\nx", Breadcrumb: "Real"},
	}

	if got := splitSections(content); !reflect.DeepEqual(got, want) {
		t.Fatalf("sections = %+v, want %+v", got, want)
	}
}

func TestSplitSectionsTreatsAnUnclosedFenceAsContent(t *testing.T) {
	sections := splitSections("# Deployment\n\n```sh\n# still code\n## also code\n")

	if len(sections) != 1 {
		t.Fatalf("sections = %d, want the unclosed fence to swallow the rest", len(sections))
	}
	if sections[0].Breadcrumb != "Deployment" {
		t.Fatalf("breadcrumb = %q, want %q", sections[0].Breadcrumb, "Deployment")
	}
}

func TestLoadDocumentsKeepsCodeBlocksWithTheirSection(t *testing.T) {
	dir := writeBreadcrumbCorpus(t, "# Deployment\n\nintro\n\n```yaml\n# config\nkey: value\n```\n\n## Rollback\n\nundo\n")

	docs, err := LoadDocuments(dir, DefaultChunkSize, DefaultChunkOverlap)
	if err != nil {
		t.Fatalf("load documents: %v", err)
	}
	if len(docs) != 1 || len(docs[0].Chunks) != 2 {
		t.Fatalf("chunks = %d, want the code block and the second section", len(docs[0].Chunks))
	}

	sections := []string{docs[0].Chunks[0].Section, docs[0].Chunks[1].Section}
	want := []string{"Deployment", "Deployment > Rollback"}
	if !reflect.DeepEqual(sections, want) {
		t.Fatalf("sections = %v, want %v", sections, want)
	}
	if !strings.Contains(docs[0].Chunks[0].Text, "# config") {
		t.Fatalf("chunk text %q, want the code block kept in the text it belongs to", docs[0].Chunks[0].Text)
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
