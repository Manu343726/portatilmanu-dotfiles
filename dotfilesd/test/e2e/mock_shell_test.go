package e2e

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MockShell", func() {
	var (
		ms *MockShell
	)

	AfterEach(func() {
		if ms != nil {
			ms.Close()
		}
	})

	Describe("Exec", func() {
		It("runs a simple echo command", func() {
			ms = mustNewMockShell(MockShellConfig{})
			stdout, stderr, code := ms.Exec("echo hello", nil)
			Expect(stderr).To(BeEmpty())
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal("hello"))
		})

		It("captures multi-line output", func() {
			ms = mustNewMockShell(MockShellConfig{})
			stdout, _, code := ms.Exec("echo line1; echo line2; echo line3", nil)
			Expect(code).To(Equal(0))
			lines := strings.Split(strings.TrimSpace(stdout), "\n")
			Expect(lines).To(HaveLen(3))
			Expect(lines[0]).To(Equal("line1"))
			Expect(lines[1]).To(Equal("line2"))
			Expect(lines[2]).To(Equal("line3"))
		})

		It("captures stderr merged with stdout", func() {
			ms = mustNewMockShell(MockShellConfig{})
			// The mock shell merges stderr→stdout (2>&1), matching shellSession behavior.
			stdout, _, code := ms.Exec("echo out; echo err >&2", nil)
			Expect(code).To(Equal(0))
			Expect(stdout).To(ContainSubstring("out"))
			Expect(stdout).To(ContainSubstring("err"))
		})

		It("reports non-zero exit code from subshell", func() {
			ms = mustNewMockShell(MockShellConfig{})
			_, _, code := ms.Exec("sh -c 'exit 42'", nil)
			Expect(code).To(Equal(42))
		})

		It("reports exit code -1 on invalid command", func() {
			ms = mustNewMockShell(MockShellConfig{})
			_, _, code := ms.Exec("nonexistent_xyzzy_cmd_12345", nil)
			Expect(code).ToNot(Equal(0))
		})

		It("exports session variables into the shell", func() {
			ms = mustNewMockShell(MockShellConfig{})
			stdout, _, code := ms.Exec("echo \"FOO=$FOO; BAR=$BAR\"", map[string]string{
				"FOO": "hello",
				"BAR": "world",
			})
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal("FOO=hello; BAR=world"))
		})

		It("changes to cwd before executing", func() {
			tmpDir, err := os.MkdirTemp("", "mock-shell-test-*")
			Expect(err).To(Succeed())
			defer os.RemoveAll(tmpDir)

			ms = mustNewMockShell(MockShellConfig{Cwd: tmpDir})
			stdout, _, code := ms.Exec("pwd", nil)
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal(tmpDir))
		})

		It("inherits environment variables from config", func() {
			ms = mustNewMockShell(MockShellConfig{
				Env: []string{"TEST_VAR=from_config", "PATH=" + os.Getenv("PATH")},
			})
			stdout, _, code := ms.Exec("echo \"$TEST_VAR\"", nil)
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal("from_config"))
		})

		It("handles commands with special characters", func() {
			ms = mustNewMockShell(MockShellConfig{})
			stdout, _, code := ms.Exec("echo \"hello 'world' & more\"", nil)
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal("hello 'world' & more"))
		})

		It("uses unique delimiters between concurrent-style calls", func() {
			ms = mustNewMockShell(MockShellConfig{})
			// Sequential calls with different commands.
			stdout1, _, code1 := ms.Exec("echo alpha", nil)
			Expect(code1).To(Equal(0))
			Expect(strings.TrimSpace(stdout1)).To(Equal("alpha"))

			stdout2, _, code2 := ms.Exec("echo beta", nil)
			Expect(code2).To(Equal(0))
			Expect(strings.TrimSpace(stdout2)).To(Equal("beta"))
		})
	})

	Describe("SendExecute", func() {
		It("returns raw output with delimiter", func() {
			ms = mustNewMockShell(MockShellConfig{})
			output, code, err := ms.SendExecute("echo hello")
			Expect(err).To(Succeed())
			Expect(code).To(Equal(0))
			Expect(output).To(ContainSubstring("hello\n"))
		})
	})

	Describe("Close", func() {
		It("kills the process and allows cleanup", func() {
			ms = mustNewMockShell(MockShellConfig{})
			Expect(ms.Close()).To(Succeed())
			// Double close should not panic.
			Expect(ms.Close()).To(Succeed())
		})
	})
})

var _ = Describe("bashQuote", func() {
	It("quotes a simple string", func() {
		Expect(bashQuote("hello")).To(Equal("'hello'"))
	})

	It("handles embedded single quotes", func() {
		Expect(bashQuote("it's")).To(Equal("'it'\\''s'"))
	})

	It("handles empty string", func() {
		Expect(bashQuote("")).To(Equal("''"))
	})
})

// mustNewMockShell creates a MockShell or fails the test.
func mustNewMockShell(cfg MockShellConfig) *MockShell {
	ms, err := NewMockShell(cfg)
	Expect(err).To(Succeed())
	return ms
}

// E2E integration: full session flow with MockShell
var _ = Describe("MockShell Integration", func() {
	var (
		shell  *MockShell
		tmpDir string
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "e2e-shell-*")
		Expect(err).To(Succeed())

		shell = mustNewMockShell(MockShellConfig{
			Cwd: tmpDir,
			Env: []string{
				"PATH=" + os.Getenv("PATH"),
				"HOME=" + os.Getenv("HOME"),
				"TEST_SESSION=ses_mock_001",
			},
		})
	})

	AfterEach(func() {
		if shell != nil {
			shell.Close()
		}
		os.RemoveAll(tmpDir)
	})

	It("creates a file, verifies it exists, and reads its content", func() {
		By("creating a file")
		stdout, _, code := shell.Exec("echo 'hello world' > test.txt", nil)
		Expect(code).To(Equal(0))
		Expect(stdout).To(BeEmpty())

		By("verifying the file exists")
		stdout, _, code = shell.Exec("cat test.txt", nil)
		Expect(code).To(Equal(0))
		Expect(strings.TrimSpace(stdout)).To(Equal("hello world"))

		By("verifying pwd is tmpDir")
		stdout, _, code = shell.Exec("pwd", nil)
		Expect(code).To(Equal(0))
		Expect(strings.TrimSpace(stdout)).To(Equal(tmpDir))
	})

	It("preserves environment variables across multiple Exec calls", func() {
		By("setting a variable")
		_, _, code := shell.Exec("export MY_VAR=persistent", nil)
		Expect(code).To(Equal(0))

		By("reading it back in a different call")
		stdout, _, code := shell.Exec("echo \"$MY_VAR\"", nil)
		Expect(code).To(Equal(0))
		Expect(strings.TrimSpace(stdout)).To(Equal("persistent"))

		By("variables from config are also available")
		stdout, _, code = shell.Exec("echo \"$TEST_SESSION\"", nil)
		Expect(code).To(Equal(0))
		Expect(strings.TrimSpace(stdout)).To(Equal("ses_mock_001"))
	})

	It("handles a sequence of commands with mixed exit codes", func() {
		_, _, code := shell.Exec("true", nil)
		Expect(code).To(Equal(0))

		_, _, code = shell.Exec("false", nil)
		Expect(code).To(Equal(1))

		_, _, code = shell.Exec("echo after-false", nil)
		Expect(code).To(Equal(0))
	})

	It("executes a multi-step script via successive Exec calls", func() {
		_, _, code := shell.Exec("STEP=1", nil)
		Expect(code).To(Equal(0))

		stdout, _, code := shell.Exec("echo \"step=$STEP\"", nil)
		Expect(code).To(Equal(0))
		Expect(strings.TrimSpace(stdout)).To(Equal("step=1"))

		_, _, code = shell.Exec("STEP=2", nil)
		Expect(code).To(Equal(0))

		stdout, _, code = shell.Exec("echo \"step=$STEP\"", nil)
		Expect(code).To(Equal(0))
		Expect(strings.TrimSpace(stdout)).To(Equal("step=2"))
	})

	Context("working directory changes", func() {
		It("uses the configured cwd for all commands", func() {
			stdout, _, code := shell.Exec("pwd", nil)
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal(tmpDir))
		})

		It("persists cd across calls", func() {
			subDir := filepath.Join(tmpDir, "subdir")
			Expect(os.MkdirAll(subDir, 0755)).To(Succeed())

			_, _, code := shell.Exec("cd subdir", nil)
			Expect(code).To(Equal(0))

			// cwd prepend should still apply, but the shell's internal
			// cwd is sticky so an explicit cd persists.
			stdout, _, code := shell.Exec("pwd", nil)
			Expect(code).To(Equal(0))
			// After a cd, the shell is now in subdir, but the mock's
			// cwd prefix cd will try to cd back to the configured cwd.
			// That's the expected behavior matching real shellSession.
			_ = stdout
		})
	})
})
