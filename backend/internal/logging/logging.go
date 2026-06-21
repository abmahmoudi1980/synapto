// Package logging configures the standard library slog logger.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New returns a slog.Logger configured for the given level string.
// "debug" enables debug-level text output to stderr (dev-friendly);
// any other value uses info-level JSON output to stderr (prod-friendly).
func New(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     lvl,
		AddSource: lvl == slog.LevelDebug,
	}

	if lvl == slog.LevelDebug {
		handler := slog.NewTextHandler(os.Stderr, opts)
		return slog.New(handler)
	}
	handler := slog.NewJSONHandler(os.Stderr, opts)
	return slog.New(handler)
}
