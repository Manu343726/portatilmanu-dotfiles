// Package color provides styled terminal output using the Monokai palette.
//
// All functions return plain strings with ANSI escape sequences so they can
// be used with any writer (fmt.Print, fmt.Fprintf, etc.). When NO_COLOR is
// set or the output is not a terminal, colours degrade gracefully.
package color

import (
	"fmt"
	"os"
)

// Monokai palette (standard ANSI 16-color codes).
// True colour codes didn't render in all terminals, so we use the closest
// standard ANSI equivalents that work everywhere.
const (
	Green  = "\033[32m" // #A6E22E (bright green)
	Red    = "\033[31m" // #F92672 (red)
	Blue   = "\033[34m" // #66D9EF (blue)
	Orange = "\033[33m" // #FD971F (yellow/orange)
	Yellow = "\033[93m" // #E6DB74 (bright yellow)
	Dim    = "\033[2m"  // #75715E (dim/bold off)
	Pink   = "\033[35m" // #F92672 (magenta)
	Cyan   = "\033[36m" // #AE81FF (cyan)

	Bold     = "\033[1m"
	DimStyle = "\033[2m"
	Italic   = "\033[3m"
	resetStr = "\033[0m"
)

var noColor bool

func init() {
	// Disable colours when NO_COLOR is set (https://no-color.org) or when
	// stdout is not a terminal.
	_, noColor = os.LookupEnv("NO_COLOR")
	if !noColor {
		if fi, _ := os.Stdout.Stat(); fi != nil && (fi.Mode()&os.ModeCharDevice) == 0 {
			noColor = true
		}
	}
}

// apply wraps s in ANSI escape seqs if colours are enabled.
func apply(s, style string) string {
	if noColor || style == "" {
		return s
	}
	return style + s + resetStr
}

// Reset returns the ANSI reset sequence, or empty if colours are disabled.
func Reset() string {
	if noColor {
		return ""
	}
	return resetStr
}

// Styled returns s wrapped with the given ANSI style sequence.
func Styled(s, style string) string { return apply(s, style) }

// Greenf formats with green colour.
func Greenf(format string, a ...any) string { return apply(Colorf(format, a...), Green) }

// Redf formats with red colour.
func Redf(format string, a ...any) string { return apply(Colorf(format, a...), Red) }

// Bluef formats with blue colour.
func Bluef(format string, a ...any) string { return apply(Colorf(format, a...), Blue) }

// Orangef formats with orange colour.
func Orangef(format string, a ...any) string { return apply(Colorf(format, a...), Orange) }

// Yellowf formats with yellow colour.
func Yellowf(format string, a ...any) string { return apply(Colorf(format, a...), Yellow) }

// Dimf formats with dim colour.
func Dimf(format string, a ...any) string { return apply(Colorf(format, a...), Dim) }

// Pinkf formats with pink/red colour.
func Pinkf(format string, a ...any) string { return apply(Colorf(format, a...), Pink) }

// Cyanf formats with cyan/purple colour.
func Cyanf(format string, a ...any) string { return apply(Colorf(format, a...), Cyan) }

// Boldf formats with bold weight.
func Boldf(format string, a ...any) string { return apply(Colorf(format, a...), Bold) }

// Colorf is like fmt.Sprintf but uses Sprint internally.
func Colorf(format string, a ...any) string {
	return fmt.Sprintf(format, a...)
}

// StatusColor returns the colour appropriate for a resource status.
func StatusColor(status string) string {
	switch status {
	case "active":
		return Green
	case "finished":
		return Dim
	case "crashed":
		return Red
	case "pending":
		return Yellow
	default:
		return ""
	}
}

// TypeColor returns the colour for a node type tag.
func TypeColor(t string) string {
	switch t {
	case "runtime":
		return Bold + Cyan
	case "daemon":
		return Bold + Blue
	case "plugin":
		return Bold + Green
	case "client":
		return Bold + Yellow
	case "session":
		return Dim
	case "exec", "script":
		return Cyan
	default:
		return ""
	}
}
