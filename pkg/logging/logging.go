// Package logging provides a small, concurrency-safe logging abstraction used
// across the RCUPC host, client, server and sessions.
//
// It wraps log/slog from the standard library. Loggers are created explicitly
// and passed to the code that needs them; there is no global logger state.
//
// Logger is an interface so that components can be tested with a recording
// implementation; production code uses the slog-backed logger from New.
package logging

import (
	"io"
	"log/slog"
)

// Level controls which entries are emitted.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) slog() slog.Level {
	switch l {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Logger is the logging interface used by RCUPC components. Implementations
// must be safe for concurrent use. Each entry carries a message and optional
// key/value fields.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// New returns a human-readable text logger that writes to w and emits entries
// at level and above.
func New(w io.Writer, level Level) Logger {
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{Level: level.slog()})
	return &slogLogger{logger: slog.New(handler)}
}

type slogLogger struct {
	logger *slog.Logger
}

func (s *slogLogger) Debug(msg string, args ...any) { s.logger.Debug(msg, args...) }
func (s *slogLogger) Info(msg string, args ...any)  { s.logger.Info(msg, args...) }
func (s *slogLogger) Warn(msg string, args ...any)  { s.logger.Warn(msg, args...) }
func (s *slogLogger) Error(msg string, args ...any) { s.logger.Error(msg, args...) }
