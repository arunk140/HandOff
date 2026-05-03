package logging

import (
	"io"
	"log/slog"
	"os"
)

func New(level slog.Level, output io.Writer) *slog.Logger {
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(handler)
}

func Default() *slog.Logger {
	level := slog.LevelInfo
	if os.Getenv("DEBUG") != "" {
		level = slog.LevelDebug
	}
	return New(level, os.Stdout)
}
