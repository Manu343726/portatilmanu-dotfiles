package logging

import (
	"fmt"
	"os"
	"sync"
)

// Logger is the core logging interface. All methods accept a message and
// optional key-value attribute pairs (must be even in number: k1, v1, k2, v2…).
type Logger interface {
	// Trace logs at TRACE level (finest granularity).
	Trace(msg string, attrs ...any)
	// Debug logs at DEBUG level.
	Debug(msg string, attrs ...any)
	// Info logs at INFO level.
	Info(msg string, attrs ...any)
	// Warn logs at WARN level.
	Warn(msg string, attrs ...any)
	// Error logs at ERROR level.
	Error(msg string, attrs ...any)
	// Fatal logs at FATAL level and then calls os.Exit(1).
	Fatal(msg string, attrs ...any)

	// Child creates a sub-logger with the given name appended to the
	// current module path: "parent.child".
	Child(name string) Logger

	// WithAttrs returns a logger that includes the given attributes
	// on every log entry.
	WithAttrs(attrs ...any) Logger

	// Enabled reports whether this logger would output a message at the
	// given level.
	Enabled(level Level) bool
}

// ---------------------------------------------------------------------------
// Package-level helpers
// ---------------------------------------------------------------------------

// NewPackageLogger creates a package-level named logger.
// Usage:
//
//	// internal/pkg/daemon/logger.go
//	var log = logging.NewPackageLogger("daemon")
//
// Types can then create child loggers:
//
//	func (d *Daemon) log() logging.Logger { return log.Child("server") }
func NewPackageLogger(name string) Logger {
	return global.Logger(name)
}

// ---------------------------------------------------------------------------
// namedLogger
// ---------------------------------------------------------------------------

// namedLogger is the standard Logger implementation.
type namedLogger struct {
	mu sync.Mutex

	// Full hierarchical name (e.g. "daemon.server").
	name string

	// resolved config at creation time.
	level  Level
	sinks  []Sink
	source bool

	// Optional fixed attributes added by WithAttrs.
	fixedAttrs []any

	// Reference to the manager for child creation.
	mgr *Manager
}

// newNamedLogger creates a logger with the given resolved settings.
func newNamedLogger(name string, level Level, sinks []Sink, source bool, mgr *Manager, fixedAttrs []any) *namedLogger {
	return &namedLogger{
		name:       name,
		level:      level,
		sinks:      sinks,
		source:     source,
		mgr:        mgr,
		fixedAttrs: fixedAttrs,
	}
}

// Child creates a sub-logger inheriting the parent's settings.
func (l *namedLogger) Child(name string) Logger {
	childName := name
	if l.name != "" && l.name != "root" {
		childName = l.name + "." + name
	}

	// Look up from manager for config-based overrides (level, sinks, source).
	if l.mgr != nil {
		cfg := l.mgr.resolve(childName)
		return newNamedLogger(childName, cfg.level, cfg.sinks, cfg.source, l.mgr, l.fixedAttrs)
	}

	// Fall back to inheriting parent settings.
	return newNamedLogger(childName, l.level, l.sinks, l.source, l.mgr, l.fixedAttrs)
}

// WithAttrs returns a logger that attaches fixed attributes to every log entry.
func (l *namedLogger) WithAttrs(attrs ...any) Logger {
	newFixed := make([]any, 0, len(l.fixedAttrs)+len(attrs))
	newFixed = append(newFixed, l.fixedAttrs...)
	newFixed = append(newFixed, attrs...)
	return newNamedLogger(l.name, l.level, l.sinks, l.source, l.mgr, newFixed)
}

// Enabled reports whether this logger would produce output at the given level.
func (l *namedLogger) Enabled(level Level) bool {
	return level >= l.level
}

// ---------------------------------------------------------------------------
// Core log method
// ---------------------------------------------------------------------------

func (l *namedLogger) log(level Level, msg string, attrs ...any) {
	if !l.Enabled(level) {
		return
	}

	entry := l.buildEntry(level, msg, attrs)
	for _, sink := range l.sinks {
		_, _ = sink.Write([]byte(entry))
	}
}

func (l *namedLogger) buildEntry(level Level, msg string, attrs []any) string {
	l.mu.Lock()
	mgr := l.mgr
	l.mu.Unlock()

	// Merge fixed attrs with call attrs.
	merged := make([]any, 0, len(l.fixedAttrs)+len(attrs))
	merged = append(merged, l.fixedAttrs...)
	merged = append(merged, attrs...)

	formatter := mgr.formatter
	return formatter.Format(level, l.name, l.source, msg, merged)
}

// ---------------------------------------------------------------------------
// Level methods
// ---------------------------------------------------------------------------

func (l *namedLogger) Trace(msg string, attrs ...any) { l.log(LevelTrace, msg, attrs...) }
func (l *namedLogger) Debug(msg string, attrs ...any) { l.log(LevelDebug, msg, attrs...) }
func (l *namedLogger) Info(msg string, attrs ...any)  { l.log(LevelInfo, msg, attrs...) }
func (l *namedLogger) Warn(msg string, attrs ...any)  { l.log(LevelWarn, msg, attrs...) }
func (l *namedLogger) Error(msg string, attrs ...any) { l.log(LevelError, msg, attrs...) }
func (l *namedLogger) Fatal(msg string, attrs ...any) {
	l.log(LevelFatal, msg, attrs...)
	os.Exit(1)
}

// ---------------------------------------------------------------------------
// NopLogger — discards everything
// ---------------------------------------------------------------------------

type nopLogger struct{}

func (nopLogger) Trace(string, ...any)           {}
func (nopLogger) Debug(string, ...any)           {}
func (nopLogger) Info(string, ...any)            {}
func (nopLogger) Warn(string, ...any)            {}
func (nopLogger) Error(string, ...any)           {}
func (nopLogger) Fatal(string, ...any)           { os.Exit(1) }
func (nopLogger) Child(string) Logger            { return nopLogger{} }
func (nopLogger) WithAttrs(...any) Logger        { return nopLogger{} }
func (nopLogger) Enabled(Level) bool             { return false }

// NopLogger is a logger that discards all output.
var NopLogger Logger = nopLogger{}

// ---------------------------------------------------------------------------
// Resolved logger config (internal)
// ---------------------------------------------------------------------------

type resolvedLogger struct {
	level  Level
	sinks  []Sink
	source bool
}

// ---------------------------------------------------------------------------
// Globals for quick access
// ---------------------------------------------------------------------------

// Globals for standard library interop
func Trace(msg string, attrs ...any) { global.Logger("root").Trace(msg, attrs...) }
func Debug(msg string, attrs ...any)  { global.Logger("root").Debug(msg, attrs...) }
func Info(msg string, attrs ...any)   { global.Logger("root").Info(msg, attrs...) }
func Warn(msg string, attrs ...any)   { global.Logger("root").Warn(msg, attrs...) }
func Error(msg string, attrs ...any)  { global.Logger("root").Error(msg, attrs...) }
func Fatal(msg string, attrs ...any)  { global.Logger("root").Fatal(msg, attrs...) }

// ---------------------------------------------------------------------------
// Helper: merge attrs
// ---------------------------------------------------------------------------

// mergeAttrs merges a set of key-value pairs, later values overriding earlier ones.
func mergeAttrs(base, extra []any) []any {
	n := len(base) + len(extra)
	result := make([]any, 0, n)
	seen := make(map[string]int)

	add := func(k, v any) {
		ks := fmt.Sprint(k)
		if idx, ok := seen[ks]; ok {
			result[idx+1] = v
		} else {
			seen[ks] = len(result)
			result = append(result, k, v)
		}
	}

	for i := 0; i < len(base); i += 2 {
		if i+1 < len(base) {
			add(base[i], base[i+1])
		}
	}
	for i := 0; i < len(extra); i += 2 {
		if i+1 < len(extra) {
			add(extra[i], extra[i+1])
		}
	}

	return result
}

// ensure even number of attrs — panics if odd.
func checkAttrs(attrs []any) {
	if len(attrs)%2 != 0 {
		panic(fmt.Sprintf("logging: odd number of attributes (%d)", len(attrs)))
	}
}

var _ Logger = (*namedLogger)(nil)
