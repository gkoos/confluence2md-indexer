package embedding

import (
	"os"
	"strings"
	"testing"
)

// TestMain clears embedding environment configuration so provider resolution
// tests are hermetic regardless of the developer's shell or CI runner.
func TestMain(m *testing.M) {
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, EnvPrefix) {
			_ = os.Unsetenv(name)
		}
	}
	_ = os.Unsetenv("OPENAI_API_KEY")
	_ = os.Unsetenv("OPENAI_EMBED_MODEL")

	os.Exit(m.Run())
}
