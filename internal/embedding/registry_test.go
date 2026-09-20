package embedding

import (
	"sort"
	"testing"
)

func TestRegistryExposesBuiltinProviders(t *testing.T) {
	available := Available()
	for _, id := range []string{ProviderBowLocal, ProviderOpenAI} {
		if !containsString(available, id) {
			t.Fatalf("expected %q to be registered, got %v", id, available)
		}
	}
	if !sort.StringsAreSorted(available) {
		t.Fatalf("expected sorted provider ids, got %v", available)
	}
}

func TestLookupNormalizesID(t *testing.T) {
	for _, id := range []string{ProviderOpenAI, " OpenAI ", "OPENAI"} {
		if _, ok := Lookup(id); !ok {
			t.Fatalf("expected lookup to normalise %q", id)
		}
	}
	if _, ok := Lookup("missing-provider"); ok {
		t.Fatal("expected an unknown provider id to be absent")
	}
}

func TestRegisterAddsProvider(t *testing.T) {
	const id = "test-registry-provider"
	Register(id, newBowFromOptions)

	if _, ok := Lookup(id); !ok {
		t.Fatalf("expected %q to be registered", id)
	}
	if !containsString(Available(), id) {
		t.Fatalf("expected %q in %v", id, Available())
	}
}

func TestMustRegisterRejectsDuplicateID(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate registration to panic")
		}
	}()

	MustRegister(ProviderBowLocal, newBowFromOptions)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
