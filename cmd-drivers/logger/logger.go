package logger

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	loggerKey contextKey = "logger"
)

// InitLogger initializes and returns a zerolog logger with the specified level
func InitLogger(level string) zerolog.Logger {
	// Set time format
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// Parse log level using zerolog's ParseLevel
	logLevel, err := zerolog.ParseLevel(level)
	if err != nil || level == "" {
		// Default to warn if parsing fails or level is empty
		logLevel = zerolog.WarnLevel
	}

	// Create console writer for human-readable output
	output := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
	}

	return zerolog.New(output).
		Level(logLevel).
		With().
		Timestamp().
		Logger()
}

// WithLogger returns a new context with the logger attached
func WithLogger(ctx context.Context, logger zerolog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext retrieves the logger from the context
// If no logger is found, it returns a default disabled logger
func FromContext(ctx context.Context) zerolog.Logger {
	if ctx == nil {
		return zerolog.Nop()
	}

	if logger, ok := ctx.Value(loggerKey).(zerolog.Logger); ok {
		return logger
	}

	// Return a no-op logger if not found
	return zerolog.Nop()
}

// Get retrieves the logger from the context
// Panics if no logger is found (use for initialization errors)
func Get(ctx context.Context) zerolog.Logger {
	if ctx == nil {
		panic("context is nil")
	}

	if logger, ok := ctx.Value(loggerKey).(zerolog.Logger); ok {
		return logger
	}

	panic("logger not found in context")
}
