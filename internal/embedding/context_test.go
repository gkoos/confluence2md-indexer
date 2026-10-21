package embedding

import "testing"

func TestParseContextNormalizesTheSelection(t *testing.T) {
	selection, err := ParseContext(" Space , author ")
	if err == nil {
		t.Fatalf("expected an unknown dimension to fail, got %v", selection)
	}

	selection, err = ParseContext(" Section ,title, space,title ")
	if err != nil {
		t.Fatalf("parse context: %v", err)
	}
	want := []string{ContextTitle, ContextSpace, ContextSection}
	if len(selection) != len(want) {
		t.Fatalf("selection = %v, want %v", selection, want)
	}
	for i := range want {
		if selection[i] != want[i] {
			t.Fatalf("selection = %v, want canonical order %v", selection, want)
		}
	}

	if selection, err := ParseContext("  "); err != nil || len(selection) != 0 {
		t.Fatalf("expected an empty selection, got %v (err %v)", selection, err)
	}
}

func TestNormalizeContextRejectsUnknownDimensions(t *testing.T) {
	if _, err := NormalizeContext([]string{"title", "labels"}); err == nil {
		t.Fatal("expected an unknown dimension to fail")
	}
}

func TestAvailableContextIsInCanonicalOrder(t *testing.T) {
	want := []string{ContextTitle, ContextSpace, ContextSection}
	if len(AvailableContext) != len(want) {
		t.Fatalf("available context = %v, want %v", AvailableContext, want)
	}
	for i := range want {
		if AvailableContext[i] != want[i] {
			t.Fatalf("available context = %v, want %v", AvailableContext, want)
		}
	}
}
func TestDocumentTextBuildsTheSelectedHeader(t *testing.T) {
	fields := ContextFields{Title: "Deployment runbook", Space: "SRE", Section: "Deployment > Rollback"}

	cases := map[string]struct {
		selection []string
		want      string
	}{
		"all dimensions": {
			selection: []string{ContextTitle, ContextSpace, ContextSection},
			want:      "title: Deployment runbook\nspace: SRE\nsection: Deployment > Rollback\n\nchunk text",
		},
		"title only": {
			selection: []string{ContextTitle},
			want:      "title: Deployment runbook\n\nchunk text",
		},
		"space and section": {
			selection: []string{ContextSpace, ContextSection},
			want:      "space: SRE\nsection: Deployment > Rollback\n\nchunk text",
		},
		"no selection": {
			selection: nil,
			want:      "chunk text",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := DocumentText(tc.selection, fields, "chunk text"); got != tc.want {
				t.Fatalf("DocumentText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDocumentTextSkipsEmptyFields(t *testing.T) {
	// A page above the first heading has no section, so the header carries what it has
	// instead of an empty line.
	got := DocumentText(
		[]string{ContextTitle, ContextSpace, ContextSection},
		ContextFields{Title: "Runbook", Space: "  ", Section: ""},
		"chunk text",
	)
	if want := "title: Runbook\n\nchunk text"; got != want {
		t.Fatalf("DocumentText = %q, want %q", got, want)
	}

	if got := DocumentText([]string{ContextTitle}, ContextFields{}, "chunk text"); got != "chunk text" {
		t.Fatalf("a selection with no values must leave the text alone, got %q", got)
	}
}

func TestDocumentTextNeverDecoratesQueryText(t *testing.T) {
	// The same helper applied to a query string is the mistake this guards: query text
	// is embedded as it is, so a caller must not pass a selection for it.
	fields := ContextFields{Title: "Runbook"}
	if got := DocumentText(nil, fields, "how do I roll back"); got != "how do I roll back" {
		t.Fatalf("DocumentText decorated query text: %q", got)
	}
}
