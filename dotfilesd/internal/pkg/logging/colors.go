package logging

// ANSI color / style escape codes used by the formatter.
const (
	ColorReset = "\033[0m"
	ColorDim   = "\033[2m"
	ColorBold  = "\033[1m"

	ColorFgBlack   = "\033[30m"
	ColorFgRed     = "\033[31m"
	ColorFgGreen   = "\033[32m"
	ColorFgYellow  = "\033[33m"
	ColorFgBlue    = "\033[34m"
	ColorFgMagenta = "\033[35m"
	ColorFgCyan    = "\033[36m"
	ColorFgWhite   = "\033[37m"
	ColorFgGray    = "\033[90m"

	// Bright / bold variants (foreground).
	ColorFgBrightRed   = "\033[91m"
	ColorFgBrightGreen = "\033[92m"
	ColorFgBrightBlue  = "\033[94m"
	ColorFgBrightCyan  = "\033[96m"

	// Background.
	ColorBgRed    = "\033[41m"
	ColorBgYellow = "\033[43m"

	CombinedFatal = "\033[31;1;47m" // red bold on white background
)
