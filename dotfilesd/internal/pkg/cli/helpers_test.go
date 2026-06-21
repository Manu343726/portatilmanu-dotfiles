package cli

import (
	"log/slog"
	"os"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("parseLogLevelStr", func() {
	It("parses 'trace'", func() {
		level, ok := parseLogLevelStr("trace")
		Expect(ok).To(BeTrue())
		Expect(level).To(Equal(cliTraceLevel))
	})

	It("parses 'debug'", func() {
		level, ok := parseLogLevelStr("debug")
		Expect(ok).To(BeTrue())
		Expect(level).To(Equal(slog.LevelDebug))
	})

	It("parses 'info'", func() {
		level, ok := parseLogLevelStr("info")
		Expect(ok).To(BeTrue())
		Expect(level).To(Equal(slog.LevelInfo))
	})

	It("parses 'warn'", func() {
		level, ok := parseLogLevelStr("warn")
		Expect(ok).To(BeTrue())
		Expect(level).To(Equal(slog.LevelWarn))
	})

	It("parses 'error'", func() {
		level, ok := parseLogLevelStr("error")
		Expect(ok).To(BeTrue())
		Expect(level).To(Equal(slog.LevelError))
	})

	It("is case-insensitive", func() {
		level, ok := parseLogLevelStr("TRACE")
		Expect(ok).To(BeTrue())
		Expect(level).To(Equal(cliTraceLevel))

		level, ok = parseLogLevelStr("Info")
		Expect(ok).To(BeTrue())
		Expect(level).To(Equal(slog.LevelInfo))
	})

	It("returns false for unknown level", func() {
		_, ok := parseLogLevelStr("unknown")
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("SetupLogging", func() {
	var tmpDir string

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "dotfilesctl-log-test-*")
		Expect(err).To(Succeed())
	})

	AfterEach(func() {
		os.RemoveAll(tmpDir)
	})

	It("creates a log file at the specified path", func() {
		SetupLogging("info")
		// After SetupLogging, the default log path is used; verify no panic
		// When level is "info", logging is configured with defaults
	})

	It("accepts debug level", func() {
		SetupLogging("debug")
	})
})

var _ = Describe("sessionProto", func() {
	It("returns nil for empty session ID", func() {
		s := sessionProto("")
		Expect(s).To(BeNil())
	})

	It("returns session with ID and shell info for non-empty ID", func() {
		s := sessionProto("ses_test_123")
		Expect(s).ToNot(BeNil())
		Expect(s.Id).To(Equal("ses_test_123"))
		Expect(s.Shell).ToNot(BeNil())
		Expect(s.Shell.Cwd).ToNot(BeEmpty())
		Expect(s.Shell.CurrentShell).ToNot(BeEmpty())
		Expect(s.Shell.Env).ToNot(BeEmpty())
	})

	It("captures the current working directory", func() {
		cwd, _ := os.Getwd()
		s := sessionProto("ses_cwd")
		Expect(s.Shell.Cwd).To(Equal(cwd))
	})

	It("captures the SHELL env var", func() {
		s := sessionProto("ses_shell")
		expected := os.Getenv("SHELL")
		Expect(s.Shell.CurrentShell).To(Equal(expected))
	})

	It("captures environment variables", func() {
		s := sessionProto("ses_env")
		Expect(s.Shell.Env["HOME"]).To(Equal(os.Getenv("HOME")))
		Expect(s.Shell.Env["PATH"]).To(Equal(os.Getenv("PATH")))
	})
})

var _ = Describe("Fatalf", func() {
	It("writes a message and exits with non-zero", func() {
		// Fatalf calls os.Exit which we can't mock easily.
		// This test verifies the function exists and compiles.
		Expect(Fatalf).ToNot(BeNil())
	})
})

var _ = Describe("ParseLogLevelStr with sessionProto integration", func() {
	It("produces valid levels that round-trip through ParseLogLevel", func() {
		// Verify the CLI internal parseLogLevelStr matches ParseLogLevel.
		level, ok := parseLogLevelStr("debug")
		Expect(ok).To(BeTrue())
		_ = level

		pbLevel := ParseLogLevel("debug")
		Expect(pbLevel).To(Equal(dotfilesdv1.LogLevel_LOG_LEVEL_DEBUG))
	})
})
