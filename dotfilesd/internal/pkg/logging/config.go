package logging

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level logging configuration structure.
type Config struct {
	// Formatter settings.
	Formatter FormatterConfig `yaml:"formatter"`

	// Sinks are named output destinations.
	Sinks []SinkConfig `yaml:"sinks"`

	// Loggers are named logger configurations with level, sinks, and source.
	Loggers []LoggerConfig `yaml:"loggers"`

	// Defaults used when sink config omits certain fields.
	DefaultMaxSizeMB  int `yaml:"default_max_size_mb"`
	DefaultMaxBackups int `yaml:"default_max_backups"`
	DefaultMaxAgeDays int `yaml:"default_max_age_days"`
}

// FormatterConfig controls the log message format.
type FormatterConfig struct {
	// TimeFormat is the Go time layout (default: "2006-01-02 15:04:05").
	TimeFormat string `yaml:"time_format"`

	// Color enables ANSI color output (default: true).
	Color *bool `yaml:"color"`

	// SourceLocation enables [file:line] in output (default: false).
	// Can be overridden per-logger.
	SourceLocation bool `yaml:"source_location"`

	// NoColorPrefix disables color even if Color is true and terminal supports it.
	NoColorPrefix bool `yaml:"no_color_prefix"`
}

// SinkConfig describes a single log sink.
type SinkConfig struct {
	// Name is a unique identifier referenced by logger configs.
	Name string `yaml:"name"`

	// Type: "stdout", "stderr", "rotating_file", "syslog".
	Type string `yaml:"type"`

	// File path (for rotating_file). {{LOG_DIR}} expands to the configured dir.
	Path string `yaml:"path"`

	// Rotating file settings.
	MaxSizeMB  int `yaml:"max_size_mb"`
	MaxBackups int `yaml:"max_backups"`
	MaxAgeDays int `yaml:"max_age_days"`
}

// LoggerConfig describes a named logger's behaviour.
type LoggerConfig struct {
	// Name is the hierarchical name (e.g. "daemon", "daemon.session").
	// The name "root" is the root logger; all others are matched by prefix.
	Name string `yaml:"name"`

	// Level: trace, debug, info, warn, error, fatal.
	Level string `yaml:"level"`

	// Sinks list sink names that this logger writes to.
	Sinks []string `yaml:"sinks"`

	// Source enables [file:line] in output for this logger.
	Source bool `yaml:"source"`
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

// DefaultConfig returns a sensible default logging configuration.
func DefaultConfig() Config {
	color := true
	return Config{
		Formatter: FormatterConfig{
			TimeFormat:     "2006-01-02 15:04:05",
			Color:          &color,
			SourceLocation: false,
		},
		Sinks: []SinkConfig{
			{Name: "stdout", Type: "stdout"},
		},
		Loggers: []LoggerConfig{
			{Name: "root", Level: "info", Sinks: []string{"stdout"}},
		},
		DefaultMaxSizeMB:  10,
		DefaultMaxBackups: 5,
		DefaultMaxAgeDays: 30,
	}
}

// ---------------------------------------------------------------------------
// Load / merge
// ---------------------------------------------------------------------------

// LoadConfig reads a YAML logging config from a file path.
// If the file does not exist, returns DefaultConfig.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return Config{}, err
	}
	return ParseConfig(data)
}

// ParseConfig parses a YAML logging config blob, filling in defaults.
func ParseConfig(data []byte) (Config, error) {
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// expandLogDir replaces {{LOG_DIR}} placeholders in sink paths.
func expandConfigPaths(cfg *Config, logDir string) {
	for i := range cfg.Sinks {
		cfg.Sinks[i].Path = strings.ReplaceAll(cfg.Sinks[i].Path, "{{LOG_DIR}}", logDir)
	}
}
