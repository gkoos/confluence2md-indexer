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
