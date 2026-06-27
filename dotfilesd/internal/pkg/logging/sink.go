package logging

import (
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Sink is a destination for formatted log lines.
type Sink interface {
	// Name returns the sink's unique identifier.
	Name() string

	// Write writes a single formatted log line.
	Write(p []byte) (int, error)

	// Close flushes and closes the sink.
	Close() error
}

// ---------------------------------------------------------------------------
// stdout sink
// ---------------------------------------------------------------------------

type stdoutSink struct {
	name string
	w    io.Writer
}

func (s *stdoutSink) Name() string                { return s.name }
func (s *stdoutSink) Write(p []byte) (int, error) { return s.w.Write(p) }
func (s *stdoutSink) Close() error                { return nil }

// NewStdoutSink creates a sink that writes to os.Stdout.
func NewStdoutSink(name string) Sink {
	return &stdoutSink{name: name, w: os.Stdout}
}

// ---------------------------------------------------------------------------
// stderr sink
// ---------------------------------------------------------------------------

type stderrSink struct {
	name string
	w    io.Writer
}

func (s *stderrSink) Name() string                { return s.name }
func (s *stderrSink) Write(p []byte) (int, error) { return s.w.Write(p) }
func (s *stderrSink) Close() error                { return nil }

// NewStderrSink creates a sink that writes to os.Stderr.
func NewStderrSink(name string) Sink {
	return &stderrSink{name: name, w: os.Stderr}
}

// ---------------------------------------------------------------------------
// rotating file sink
// ---------------------------------------------------------------------------

type rotatingFileSink struct {
	name   string
	logger *lumberjack.Logger
}

func (s *rotatingFileSink) Name() string { return s.name }

func (s *rotatingFileSink) Write(p []byte) (int, error) {
	return s.logger.Write(p)
}

func (s *rotatingFileSink) Close() error {
	return s.logger.Close()
}

// NewRotatingFileSink creates a sink backed by a lumberjack rotating file.
// maxMB / backups / age are the lumberjack MaxSize / MaxBackups / MaxAge values.
func NewRotatingFileSink(name, path string, maxMB, backups, age int) Sink {
	return &rotatingFileSink{
		name: name,
		logger: &lumberjack.Logger{
			Filename:   path,
			MaxSize:    maxMB,
			MaxBackups: backups,
			MaxAge:     age,
			Compress:   true,
		},
	}
}

// ---------------------------------------------------------------------------
// sink registry — maps names to sinks
// ---------------------------------------------------------------------------

// sinkRegistry holds all registered sinks.
type sinkRegistry struct {
	sinks map[string]Sink
	order []string // insertion order
}

func newSinkRegistry() *sinkRegistry {
	return &sinkRegistry{sinks: make(map[string]Sink)}
}

func (r *sinkRegistry) Add(s Sink) {
	if _, exists := r.sinks[s.Name()]; !exists {
		r.order = append(r.order, s.Name())
	}
	r.sinks[s.Name()] = s
}

func (r *sinkRegistry) Get(name string) (Sink, bool) {
	s, ok := r.sinks[name]
	return s, ok
}

func (r *sinkRegistry) Names() []string { return r.order }

func (r *sinkRegistry) CloseAll() {
	for _, name := range r.order {
		if s, ok := r.sinks[name]; ok {
			_ = s.Close()
		}
	}
}

// ---------------------------------------------------------------------------
// Build sinks from config
// ---------------------------------------------------------------------------

func buildSinks(cfg Config, logDir string) (*sinkRegistry, error) {
	reg := newSinkRegistry()

	for _, sc := range cfg.Sinks {
		switch sc.Type {
		case "stdout":
			reg.Add(NewStdoutSink(sc.Name))
		case "stderr":
			reg.Add(NewStderrSink(sc.Name))
		case "rotating_file":
			path := strings.ReplaceAll(sc.Path, "{{LOG_DIR}}", logDir)
			reg.Add(NewRotatingFileSink(sc.Name, path,
				nonZero(sc.MaxSizeMB, cfg.DefaultMaxSizeMB),
				nonZero(sc.MaxBackups, cfg.DefaultMaxBackups),
				nonZero(sc.MaxAgeDays, cfg.DefaultMaxAgeDays),
			))
		case "syslog":
			// Syslog support is platform-dependent. For now, fall back to a
			// no-op sink with a warning.
			reg.Add(&syslogStubSink{name: sc.Name, msg: "syslog sink not yet implemented"})
		default:
			return nil, fmt.Errorf("unknown sink type %q", sc.Type)
		}
	}

	// Ensure at least the "default" stdout sink exists.
	if _, ok := reg.Get("default"); !ok {
		reg.Add(NewStdoutSink("default"))
	}

	return reg, nil
}

// syslogStubSink is a placeholder for syslog support.
type syslogStubSink struct {
	name string
	msg  string
}

func (s *syslogStubSink) Name() string                { return s.name }
func (s *syslogStubSink) Write(p []byte) (int, error) { return len(p), nil }
func (s *syslogStubSink) Close() error                { return nil }

func nonZero(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 10 // sensible default
}
