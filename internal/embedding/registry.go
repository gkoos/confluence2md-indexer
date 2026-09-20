package embedding

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Factory builds a provider from resolved options.
type Factory func(Options) (Provider, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Factory)
)

// Register adds a provider factory under a case-insensitive id.
func Register(id string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[normalizeID(id)] = factory
}

// MustRegister registers a provider factory and panics when the id is already
// taken, which can only happen through a programming error at init time.
func MustRegister(id string, factory Factory) {
	registryMu.RLock()
	_, exists := registry[normalizeID(id)]
	registryMu.RUnlock()
	if exists {
		panic(fmt.Sprintf("embedding: provider %q registered twice", id))
	}
	Register(id, factory)
}

// Lookup returns the factory registered for id.
func Lookup(id string) (Factory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	factory, ok := registry[normalizeID(id)]
	return factory, ok
}

// Available lists registered provider ids in sorted order.
func Available() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func normalizeID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}
