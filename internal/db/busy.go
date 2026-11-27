package db

import (
	"errors"
	"fmt"
	"strings"
)

// ErrBusy marks a failure caused by another process holding the index locked
// for longer than the busy timeout. The same call is expected to succeed later.
var ErrBusy = errors.New("index is busy: another process is writing to it, retry shortly")

// WrapBusy returns err wrapped with ErrBusy when SQLite reported a lock timeout,
// and err unchanged otherwise.
func WrapBusy(err error) error {
	if err == nil || errors.Is(err, ErrBusy) {
		return err
	}
	// The driver exposes the condition only through its message.
	message := err.Error()
	if strings.Contains(message, "SQLITE_BUSY") || strings.Contains(message, "database is locked") {
		return fmt.Errorf("%w: %v", ErrBusy, err)
	}
	return err
}
