package db

import (
	"errors"
	"testing"
)

func TestWrapBusy(t *testing.T) {
	if WrapBusy(nil) != nil {
		t.Fatal("nil must stay nil")
	}

	other := errors.New("no such table: documents")
	if got := WrapBusy(other); got != other {
		t.Fatalf("an unrelated error must pass through unchanged, got %v", got)
	}

	for _, message := range []string{"database is locked (5) (SQLITE_BUSY)", "step: SQLITE_BUSY"} {
		wrapped := WrapBusy(errors.New(message))
		if !errors.Is(wrapped, ErrBusy) {
			t.Fatalf("%q was not wrapped as ErrBusy", message)
		}
	}
}
