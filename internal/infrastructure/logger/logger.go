package logger

import (
	"log/slog"
	"os"
	"strings"
)

var defaultLogger *slog.Logger

func init() {
	// Initialize with a simple text handler by default.
	// Can be overridden via InitLogger based on config.
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
	handler := slog.NewTextHandler(os.Stdout, opts)
	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// InitLogger configures the global slog instance based on specified format and level.
// Allowed formats: "json", "text".
// Allowed levels: "debug", "info", "warn", "error".
func InitLogger(levelStr, format string) {
	var level slog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	if strings.ToLower(format) == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// WithContext returns a new Logger that includes the provided key-value pairs
// in all of its log records.
func WithContext(args ...any) *slog.Logger {
	return defaultLogger.With(args...)
}
