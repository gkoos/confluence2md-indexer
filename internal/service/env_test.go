package service

import (
	"os"
	"testing"

	"github.com/gkoos/confluence2md-indexer/internal/embedding/embeddingtest"
)

// TestMain isolates embedding configuration so command tests always exercise the
// default provider regardless of the developer's shell or CI environment.
func TestMain(m *testing.M) {
	embeddingtest.IsolateEnv()
	os.Exit(m.Run())
}
