package e2e

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MockShell exec flow", func() {
	var (
		ms *MockShell
	)

	AfterEach(func() {
		if ms != nil {
			ms.Close()
		}
	})

	Describe("session variable injection", func() {
		It("injects variables with special characters", func() {
			ms = mustNewMockShell(MockShellConfig{})
			stdout, _, code := ms.Exec(`echo "PATH=$PATH"`, map[string]string{
				"PATH": "/custom/bin:/usr/bin",
			})
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal("PATH=/custom/bin:/usr/bin"))
		})

		It("persists variables across successive Exec calls", func() {
			ms = mustNewMockShell(MockShellConfig{})

			// First call has variables
			stdout, _, code := ms.Exec(`echo "$A"`, map[string]string{"A": "first"})
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal("first"))

			// Second call with no variables — A persists because the shell process
			// retains exports from the previous call.
			stdout, _, code = ms.Exec(`echo "$A"`, nil)
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal("first"))
		})

		It("overwrites existing env with injected variables", func() {
			ms = mustNewMockShell(MockShellConfig{
				Env: []string{"HOME=/original/home", "PATH=" + os.Getenv("PATH")},
			})
			stdout, _, code := ms.Exec(`echo "HOME=$HOME"`, map[string]string{
				"HOME": "/injected/home",
			})
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal("HOME=/injected/home"))
		})
	})

	Describe("cd behavior", func() {
		It("changes to cwd before each command", func() {
			tmpDir, err := os.MkdirTemp("", "mock-shell-cd-*")
			Expect(err).To(Succeed())
			defer os.RemoveAll(tmpDir)

			ms = mustNewMockShell(MockShellConfig{Cwd: tmpDir})
			stdout, _, code := ms.Exec("pwd", nil)
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal(tmpDir))
		})

		It("cd is prepended before variable exports", func() {
			tmpDir, err := os.MkdirTemp("", "mock-shell-cd-*")
			Expect(err).To(Succeed())
			defer os.RemoveAll(tmpDir)

			ms = mustNewMockShell(MockShellConfig{Cwd: tmpDir})

			// When cwd is set, cd is prepended before export commands.
			// Running "pwd && echo $VAR" verifies both are applied.
			stdout, _, code := ms.Exec("pwd && echo \"VAR=$MYVAR\"", map[string]string{
				"MYVAR": "tested",
			})
			Expect(code).To(Equal(0))
			lines := strings.Split(strings.TrimSpace(stdout), "\n")
			Expect(lines[0]).To(Equal(tmpDir))
			Expect(lines[1]).To(Equal("VAR=tested"))
		})
	})

	Describe("edge cases", func() {
		It("handles empty command", func() {
			ms = mustNewMockShell(MockShellConfig{})
			stdout, stderr, code := ms.Exec("", nil)
			Expect(code).To(Equal(0))
			Expect(stdout).To(BeEmpty())
			Expect(stderr).To(BeEmpty())
		})

		It("handles command with only whitespace", func() {
			ms = mustNewMockShell(MockShellConfig{})
			_, _, code := ms.Exec("   ", nil)
			Expect(code).To(Equal(0))
		})

		It("handles very long output", func() {
			ms = mustNewMockShell(MockShellConfig{})
			longStr := strings.Repeat("a", 10000)
			stdout, _, code := ms.Exec("echo "+longStr, nil)
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(HaveLen(10000))
		})

		It("handles output with embedded delimiters", func() {
			ms = mustNewMockShell(MockShellConfig{})
			// The delimiter looks like __GS_<hex>__=exitcode.
			// This test verifies actual delimiters in output are handled.
			stdout, _, code := ms.Exec("echo \"__GS_1234__=0\"", nil)
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal("__GS_1234__=0"))
		})

		It("handles concurrent Exec calls", func() {
			ms = mustNewMockShell(MockShellConfig{})
			results := make(chan string, 3)

			for i := 0; i < 3; i++ {
				go func(n int) {
					defer GinkgoRecover()
					stdout, _, code := ms.Exec("echo \"task_"+string(rune('0'+n))+"\"", nil)
					Expect(code).To(Equal(0))
					results <- strings.TrimSpace(stdout)
				}(i)
			}

			outputs := []string{}
			for i := 0; i < 3; i++ {
				outputs = append(outputs, <-results)
			}
			Expect(outputs).To(ConsistOf("task_0", "task_1", "task_2"))
		})
	})
})
