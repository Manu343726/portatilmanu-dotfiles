package logging

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// Formatter produces colored, formatted log lines.
//
// Output format:
//
//	TIMESTAMP LEVEL [module] [file:line] message key1=val1 key2=val2 ...
//
// Each section uses a dedicated ANSI color when colour is enabled:
//   - TIMESTAMP: dim white
//   - LEVEL:     level-specific (FATAL=red bold, ERROR=red, WARN=yellow,
//     INFO=green, DEBUG=cyan, TRACE=gray)
//   - [module]:  bright blue
//   - [file:line]: yellow dim
//   - message:   default (reset)
//   - attrs:     dim white/gray
type Formatter struct {
	TimeFormat     string
	Color          bool
	SourceLocation bool
}

// NewFormatter creates a formatter from config.
func NewFormatter(cfg FormatterConfig) *Formatter {
	timeFormat := cfg.TimeFormat
	if timeFormat == "" {
		timeFormat = "2006-01-02 15:04:05"
	}
	color := cfg.Color == nil || *cfg.Color
	return &Formatter{
		TimeFormat:     timeFormat,
		Color:          color,
		SourceLocation: cfg.SourceLocation,
	}
}

// Format produces a single log line (without trailing newline).
func (f *Formatter) Format(level Level, module string, source bool, msg string, attrs []any) string {
	var b strings.Builder
	now := time.Now().Format(f.TimeFormat)

	// Timestamp.
	if f.Color {
		b.WriteString("\033[2m") // dim
	}
	b.WriteString(now)
	if f.Color {
		b.WriteString(ColorReset)
	}
	b.WriteByte(' ')

	// Level.
	if f.Color {
		b.WriteString(level.Color())
	}
	b.WriteString(level.String())
	if f.Color {
		b.WriteString(ColorReset)
	}
	b.WriteByte(' ')

	// Module: [name] in bright blue.
	if module != "" {
		if f.Color {
			b.WriteString("\033[34;1m") // bright blue
		}
		b.WriteByte('[')
		b.WriteString(module)
		b.WriteByte(']')
		if f.Color {
			b.WriteString(ColorReset)
		}
		b.WriteByte(' ')
	}

	// Source location: [file:line] in yellow dim.
	if source {
		_, file, line, ok := runtime.Caller(4) // 4 frames up: Format ← writeEntry ← namedLogger.Info ← caller
		if !ok {
			file = "???"
		}
		// Short file: just the base name + line.
		if idx := strings.LastIndexByte(file, '/'); idx >= 0 {
			file = file[idx+1:]
		}
		if f.Color {
			b.WriteString("\033[33;2m") // yellow dim
		}
		fmt.Fprintf(&b, "[%s:%d]", file, line)
		if f.Color {
			b.WriteString(ColorReset)
		}
		b.WriteByte(' ')
	}

	// Message.
	b.WriteString(msg)

	// Attributes.
	if len(attrs) > 0 {
		for i := 0; i < len(attrs); i += 2 {
			if f.Color {
				b.WriteString(ColorDim)
			}
			key := fmt.Sprint(attrs[i])
			var val string
			if i+1 < len(attrs) {
				val = fmt.Sprint(attrs[i+1])
			}
			b.WriteByte(' ')
			b.WriteString(key)
			b.WriteByte('=')
			b.WriteString(val)
			if f.Color {
				b.WriteString(ColorReset)
			}
		}
	}

	b.WriteByte('\n')
	return b.String()
}

// formatAttrsPlain serialises key-value pairs for non-colored output.
func formatAttrsPlain(attrs []any) string {
	if len(attrs) == 0 {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(attrs); i += 2 {
		key := fmt.Sprint(attrs[i])
		var val string
		if i+1 < len(attrs) {
			val = fmt.Sprint(attrs[i+1])
		}
		b.WriteByte(' ')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(val)
	}
	return b.String()
}
