package main

import "testing"

// TestRunPasses exercises the whole gate in process: it indexes a fixture corpus
// with the default provider and checks the vector, hybrid and lexical paths plus
// the embedding identity guard.
func TestRunPasses(t *testing.T) {
	if err := run(false); err != nil {
		t.Fatalf("vector smoke gate failed: %v", err)
	}
}
