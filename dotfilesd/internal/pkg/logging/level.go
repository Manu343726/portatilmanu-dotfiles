// Package logging provides a structured, hierarchical logging system with
// YAML configuration, colored output, and multiple sink support.
package logging

import (
	"fmt"
	"strings"
)

// Level represents a log severity level.
// The numeric values align with slog levels for interoperability.
type Level int

const (
	// LevelTrace is the finest granularity (slog Level -8).
	LevelTrace Level = -8
	// LevelDebug (slog LevelDebug = -4).
	LevelDebug Level = -4
	// LevelInfo (slog LevelInfo = 0).
	LevelInfo Level = 0
	// LevelWarn (slog LevelWarn = 4).
	LevelWarn Level = 4
	// LevelError (slog LevelError = 8).
	LevelError Level = 8
	// LevelFatal is the highest severity, above Error.
	LevelFatal Level = 12
)

// String returns the upper-case level name.
func (l Level) String() string {
	switch l {
	case LevelTrace:
		return "TRACE"
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return fmt.Sprintf("LEVEL(%d)", l)
	}
}

// ParseLevel parses a level string (case-insensitive).
// Returns LevelInfo and false if unrecognised.
func ParseLevel(s string) (Level, bool) {
	switch strings.ToUpper(s) {
	case "TRACE":
		return LevelTrace, true
	case "DEBUG":
		return LevelDebug, true
	case "INFO":
		return LevelInfo, true
	case "WARN", "WARNING":
		return LevelWarn, true
	case "ERROR":
		return LevelError, true
	case "FATAL":
		return LevelFatal, true
	default:
		return LevelInfo, false
	}
}

// Color returns the ANSI color code for this level.
func (l Level) Color() string {
	switch l {
	case LevelFatal:
		return "\033[31;1m" // red bold
	case LevelError:
		return "\033[31m" // red
	case LevelWarn:
		return "\033[33m" // yellow
	case LevelInfo:
		return "\033[32m" // green
	case LevelDebug:
		return "\033[36m" // cyan
	case LevelTrace:
		return "\033[90m" // bright black / gray
	default:
		return "\033[0m"
	}
}
