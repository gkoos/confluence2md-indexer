package indexer

import (
	"strings"
	"testing"
)

func TestParseSourceSpec(t *testing.T) {
	source, err := ParseSourceSpec(" eng = ./exports/eng ")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if source.ID != "eng" || source.Folder != "./exports/eng" || source.Type != TypeConfluence {
		t.Fatalf("unexpected source: %+v", source)
	}

	// Only the first '=' separates the id, so a folder may contain one.
	source, err = ParseSourceSpec("a=dir/with=equals")
	if err != nil || source.Folder != "dir/with=equals" {
		t.Fatalf("folder with '=': %+v, %v", source, err)
	}

	if _, err := ParseSourceSpec("no-separator"); err == nil {
		t.Fatal("a value without '=' must be rejected")
	}
}

func TestNormalizeSourcesRejectsBadInput(t *testing.T) {
	cases := map[string]struct {
		sources []Source
		want    string
	}{
		"empty list":     {nil, "at least one source"},
		"missing id":     {[]Source{{Folder: "x"}}, "no id"},
		"slash in id":    {[]Source{{ID: "a/b", Folder: "x"}}, "may only use"},
		"equals in id":   {[]Source{{ID: "a=b", Folder: "x"}}, "may only use"},
		"leading dash":   {[]Source{{ID: "-a", Folder: "x"}}, "may only use"},
		"too long":       {[]Source{{ID: strings.Repeat("a", 65), Folder: "x"}}, "longer than"},
		"duplicate id":   {[]Source{{ID: "a", Folder: "x"}, {ID: "a", Folder: "y"}}, "more than once"},
		"unknown type":   {[]Source{{ID: "a", Type: "sharepoint", Folder: "x"}}, "unsupported type"},
		"missing folder": {[]Source{{ID: "a"}}, "no folder"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NormalizeSources(tc.sources)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestNormalizeSourcesDefaultsTypeAndAbsolutizesFolders(t *testing.T) {
	sources, err := NormalizeSources([]Source{{ID: "eng.v2_x-1", Folder: "relative"}})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if sources[0].Type != TypeConfluence {
		t.Fatalf("type = %q, want %q", sources[0].Type, TypeConfluence)
	}
	if !strings.HasSuffix(strings.ReplaceAll(sources[0].Folder, "\\", "/"), "/relative") || sources[0].Folder == "relative" {
		t.Fatalf("folder was not made absolute: %q", sources[0].Folder)
	}
}
