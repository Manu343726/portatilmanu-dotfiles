package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Manager holds the logging configuration and creates named loggers.
// There is typically one global Manager per process.
type Manager struct {
	mu sync.RWMutex

	config    Config
	sinks     *sinkRegistry
	formatter *Formatter
	resolved  map[string]resolvedLogger // cache resolved loggers by name
	pluginDir string                    // plugin log directory for dedicated sinks
}

// NewManager creates a Manager from the given Config.
func NewManager(cfg Config) *Manager {
	m := &Manager{
		config:   cfg,
		resolved: make(map[string]resolvedLogger),
	}
	// Build sinks.
	sinks, err := buildSinks(cfg, "")
	if err != nil {
		// Fallback to stdout.
		sinks = newSinkRegistry()
		sinks.Add(NewStdoutSink("default"))
	}
	m.sinks = sinks

	// Build formatter.
	m.formatter = NewFormatter(cfg.Formatter)

	return m
}

// Configure creates a new global manager from the given config.
// This is the main entry point for setting up logging at startup.
func Configure(cfg Config) {
	m := NewManager(cfg)

	// Set up the plugin log directory.
	cfgLogDir := ""
	for _, sc := range cfg.Sinks {
		if sc.Type == "rotating_file" {
			if p := sc.Path; p != "" {
				cfgLogDir = filepath.Dir(p)
				cfgLogDir = strings.ReplaceAll(cfgLogDir, "{{LOG_DIR}}", "")
				break
			}
		}
	}
	if cfgLogDir == "" {
		home, _ := os.UserHomeDir()
		cfgLogDir = home + "/dotfilesd/logs"
	}
	m.pluginDir = cfgLogDir + "/plugins"
	_ = os.MkdirAll(m.pluginDir, 0755)

	globalMu.Lock()
	global = m
	globalMu.Unlock()
}

// ---------------------------------------------------------------------------
// Logger resolution
// ---------------------------------------------------------------------------

// Logger returns a named logger. The name is a dot-separated hierarchy
// (e.g. "daemon.server"). Loggers are resolved lazily and cached.
func (m *Manager) Logger(name string) Logger {
	if name == "" {
		name = "root"
	}

	m.mu.RLock()
	cached, ok := m.resolved[name]
	m.mu.RUnlock()

	if ok {
		return newNamedLogger(name, cached.level, cached.sinks, cached.source, m, nil)
	}

	// Resolve.
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check cache again (double-checked locking).
	if cached, ok := m.resolved[name]; ok {
		return newNamedLogger(name, cached.level, cached.sinks, cached.source, m, nil)
	}

	rl := m.resolve(name)
	m.resolved[name] = rl
	return newNamedLogger(name, rl.level, rl.sinks, rl.source, m, nil)
}

// resolve finds the best matching logger configuration for the given name.
// Resolution is by longest-prefix match against configured logger names.
func (m *Manager) resolve(name string) resolvedLogger {
	// Start with root defaults.
	level := LevelInfo
	sinks := m.defaultSinks()
	source := false

	// Find the best matching logger config by longest prefix match.
	var bestMatch string
	var bestCfg *LoggerConfig

	for _, lc := range m.config.Loggers {
		// Exact match or prefix match.
		if lc.Name == name || lc.Name == "root" || strings.HasPrefix(name, lc.Name+".") {
			// Prefer the longest match.
			if len(lc.Name) > len(bestMatch) || (lc.Name == name && bestMatch != name) {
				bestMatch = lc.Name
				cfg := lc
				bestCfg = &cfg
			}
		}
	}

	if bestCfg != nil {
		if lvl, ok := ParseLevel(bestCfg.Level); ok {
			level = lvl
		}
		source = bestCfg.Source

		// Resolve sink names to actual sinks.
		if len(bestCfg.Sinks) > 0 {
			var namedSinks []Sink
			for _, sName := range bestCfg.Sinks {
				if s, ok := m.sinks.Get(sName); ok {
					namedSinks = append(namedSinks, s)
				}
			}
			if len(namedSinks) > 0 {
				sinks = namedSinks
			}
		}
	}

	return resolvedLogger{level: level, sinks: sinks, source: source}
}

// defaultSinks returns the default sink set (all registered sinks).
func (m *Manager) defaultSinks() []Sink {
	names := m.sinks.Names()
	sinks := make([]Sink, 0, len(names))
	for _, n := range names {
		if s, ok := m.sinks.Get(n); ok {
			sinks = append(sinks, s)
		}
	}
	return sinks
}

// ---------------------------------------------------------------------------
// Plugin log sink management
// ---------------------------------------------------------------------------

// AddPluginSink creates a dedicated rotating file sink for a plugin.
// The sink is named "plugin.<name>" and writes to pluginDir/<name>.log.
func (m *Manager) AddPluginSink(pluginName string) {
	path := fmt.Sprintf("%s/%s.log", m.pluginDir, pluginName)
	sink := NewRotatingFileSink("plugin."+pluginName, path, 10, 5, 30)
	m.sinks.Add(sink)

	// Also register a logger config for this plugin.
	m.mu.Lock()
	m.config.Loggers = append(m.config.Loggers, LoggerConfig{
		Name:   "plugin." + pluginName,
		Level:  "debug",
		Sinks:  []string{"plugin." + pluginName, "stdout"},
		Source: false,
	})
	m.mu.Unlock()
}

// PluginLogger creates a named logger for a plugin. The plugin's logs appear
// under the "plugin.<name>" hierarchy.
func (m *Manager) PluginLogger(pluginName string) Logger {
	return m.Logger("plugin." + pluginName)
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

// Close flushes and closes all sinks.
func (m *Manager) Close() {
	if m.sinks != nil {
		m.sinks.CloseAll()
	}
}

// ---------------------------------------------------------------------------
// Global manager
// ---------------------------------------------------------------------------

var (
	global   *Manager
	globalMu sync.Mutex
)

// init creates the default global manager (stdout only, INFO level).
func init() {
	global = NewManager(DefaultConfig())
}

// Global returns the current global Manager.
func Global() *Manager {
	globalMu.Lock()
	defer globalMu.Unlock()
	return global
}

// ---------------------------------------------------------------------------
// Attr helper — builds key-value pairs for structured logging
// ---------------------------------------------------------------------------

// A is a shorthand for building key-value attributes.
//
// Usage: logging.A("key1", val1, "key2", val2)
func A(pairs ...any) []any {
	if len(pairs)%2 != 0 {
		panic(fmt.Sprintf("logging.A: odd number of arguments (%d)", len(pairs)))
	}
	return pairs
}

// ---------------------------------------------------------------------------
// slog interop — bridges the new logging package with slog so existing
// code that uses slog can route through our system.
// ---------------------------------------------------------------------------

type slogHandler struct {
	logger Logger
	attrs  []any
}

// NewSlogHandler creates a slog.Handler that routes to a Logger.
// Use this when you need to pass a slog.Handler to third-party libraries.
func NewSlogHandler(logger Logger) slog.Handler {
	return &slogHandler{logger: logger}
}

func (h *slogHandler) Enabled(_ context.Context, level slog.Level) bool {
	switch {
	case level <= slog.Level(-8):
		return h.logger.Enabled(LevelTrace)
	case level < slog.LevelInfo:
		return h.logger.Enabled(LevelDebug)
	case level == slog.LevelInfo:
		return h.logger.Enabled(LevelInfo)
	case level == slog.LevelWarn:
		return h.logger.Enabled(LevelWarn)
	default:
		return h.logger.Enabled(LevelError)
	}
}

func (h *slogHandler) Handle(_ context.Context, r slog.Record) error {
	var lvl Level
	switch {
	case r.Level <= slog.Level(-8):
		lvl = LevelTrace
	case r.Level < slog.LevelInfo:
		lvl = LevelDebug
	case r.Level == slog.LevelInfo:
		lvl = LevelInfo
	case r.Level == slog.LevelWarn:
		lvl = LevelWarn
	case r.Level >= slog.LevelError:
		lvl = LevelError
	default:
		lvl = LevelInfo
	}

	// Collect attributes from record.
	attrs := make([]any, 0, len(h.attrs)+r.NumAttrs()*2)
	attrs = append(attrs, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a.Key, a.Value.Any())
		return true
	})

	// Route to the right level method.
	switch lvl {
	case LevelTrace:
		h.logger.Trace(r.Message, attrs...)
	case LevelDebug:
		h.logger.Debug(r.Message, attrs...)
	case LevelInfo:
		h.logger.Info(r.Message, attrs...)
	case LevelWarn:
		h.logger.Warn(r.Message, attrs...)
	case LevelError:
		h.logger.Error(r.Message, attrs...)
	default:
		h.logger.Info(r.Message, attrs...)
	}
	return nil
}

func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]any, 0, len(h.attrs)+len(attrs)*2)
	newAttrs = append(newAttrs, h.attrs...)
	for _, a := range attrs {
		newAttrs = append(newAttrs, a.Key, a.Value.Any())
	}
	return &slogHandler{logger: h.logger.WithAttrs(newAttrs...), attrs: newAttrs}
}

func (h *slogHandler) WithGroup(name string) slog.Handler {
	return h // groups not supported in the simple bridge
}
