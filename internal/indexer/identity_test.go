package indexer

import "testing"

func TestChunkID(t *testing.T) {
	got := ChunkID("123", 7)
	if got != "123:000007" {
		t.Fatalf("expected 123:000007 got %s", got)
	}
}

func TestContentHashDeterministic(t *testing.T) {
	a := ContentHash("same content")
	b := ContentHash("same content")
	if a != b {
		t.Fatalf("expected deterministic hash, got %s and %s", a, b)
	}

	c := ContentHash("different")
	if c == a {
		t.Fatalf("expected different hash values")
	}
}
