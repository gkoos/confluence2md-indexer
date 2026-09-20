package indexerapi

import (
	"os"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/embedding/embeddingtest"
)

// TestMain isolates embedding configuration so public API tests always exercise
// the default provider regardless of the developer's shell.
func TestMain(m *testing.M) {
	embeddingtest.IsolateEnv()
	os.Exit(m.Run())
}
