package logging

import (
	"log/slog"
	"os"
)

func New() *slog.Logger {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(handler)
}
