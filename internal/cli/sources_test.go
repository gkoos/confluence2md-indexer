package cli

import (
	"strings"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/config"
	"github.com/gkoos/confluence2md-indexer/internal/indexer"
)

func TestResolveIndexSourcesPrecedence(t *testing.T) {
	configured := []config.Source{{ID: "from-config", Path: "cfg-folder"}}

	cases := []struct {
		name    string
		folders []string
		specs   []string
		config  []config.Source
		wantIDs []string
		wantErr string
	}{
		{name: "nothing given indexes the working directory", wantIDs: []string{indexer.DefaultSourceID}},
		{name: "a folder argument", folders: []string{"docs"}, wantIDs: []string{indexer.DefaultSourceID}},
		{name: "source flags", specs: []string{"a=one", "b=two"}, wantIDs: []string{"a", "b"}},
		{name: "config sources", config: configured, wantIDs: []string{"from-config"}},
		{name: "a folder argument beats config", folders: []string{"docs"}, config: configured, wantIDs: []string{indexer.DefaultSourceID}},
		{name: "source flags beat config", specs: []string{"flag=one"}, config: configured, wantIDs: []string{"flag"}},
		{name: "folder and flags conflict", folders: []string{"docs"}, specs: []string{"a=one"}, wantErr: "not both"},
		{name: "malformed flag", specs: []string{"nofolder"}, wantErr: "id=folder"},
		{name: "duplicate ids", specs: []string{"a=one", "a=two"}, wantErr: "more than once"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources, err := resolveIndexSources(tc.folders, tc.specs, tc.config)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}

			var got []string
			for _, source := range sources {
				got = append(got, source.ID)
			}
			if strings.Join(got, ",") != strings.Join(tc.wantIDs, ",") {
				t.Fatalf("ids = %v, want %v", got, tc.wantIDs)
			}
		})
	}
}

func TestResolveIndexDBPathNeedsAnExplicitDBForSeveralSources(t *testing.T) {
	several := []indexer.Source{{ID: "a", Folder: "/x/a"}, {ID: "b", Folder: "/x/b"}}

	if _, err := resolveIndexDBPath(several, "", ""); err == nil || !strings.Contains(err.Error(), "--db or db.path") {
		t.Fatalf("err = %v, want a request for --db or db.path", err)
	}
	if got, err := resolveIndexDBPath(several, "idx.db", ""); err != nil || !strings.HasSuffix(got, "idx.db") {
		t.Fatalf("with --db: %q, %v", got, err)
	}
	if got, err := resolveIndexDBPath(several, "", "cfg.db"); err != nil || !strings.HasSuffix(got, "cfg.db") {
		t.Fatalf("with db.path: %q, %v", got, err)
	}

	single, err := resolveIndexDBPath(several[:1], "", "")
	if err != nil || !strings.HasSuffix(strings.ReplaceAll(single, "\\", "/"), "/x/a/"+defaultDBFileName) {
		t.Fatalf("a single source keeps the database in its folder: %q, %v", single, err)
	}
}

func TestReindexHintNamesTheSources(t *testing.T) {
	if got := reindexHint(nil, "idx.db", ""); got != "confluence2md-indexer index <folder> --db idx.db" {
		t.Fatalf("no sources: %q", got)
	}
	if got := reindexHint(indexer.SingleSource("docs"), "idx.db", ""); got != "confluence2md-indexer index docs --db idx.db" {
		t.Fatalf("single default source: %q", got)
	}

	got := reindexHint([]indexer.Source{{ID: "a", Folder: "one"}, {ID: "b", Folder: "two"}}, "idx.db", "")
	want := "confluence2md-indexer index --source a=one --source b=two --db idx.db"
	if got != want {
		t.Fatalf("several sources: %q, want %q", got, want)
	}
}
