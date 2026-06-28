package daemon

import (
	"os"
	"os/exec"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("buildShellEnv", func() {
	var savedHome string

	BeforeEach(func() {
		savedHome = os.Getenv("HOME")
		os.Setenv("HOME", "/home/testuser")
		SetDaemonPort("9105")
	})

	AfterEach(func() {
		os.Setenv("HOME", savedHome)
	})

	It("uses CLI env when provided", func() {
		env := buildShellEnv("ses_test", nil, &dotfilesdv1.Shell{
			Env: map[string]string{
				"PATH": "/usr/bin",
				"TERM": "xterm",
			},
		})
		Expect(env).To(ContainElement("PATH=/home/testuser/.local/bin:/usr/bin"))
		Expect(env).To(ContainElement("TERM=xterm"))
		Expect(env).To(ContainElement(HavePrefix("DOTFILESD_DAEMON=1")))
		Expect(env).To(ContainElement("DOTFILESD_PORT=9105"))
		Expect(env).To(ContainElement(HavePrefix("DOTFILESD_SESSION=ses_test")))
		Expect(env).To(ContainElement("HOME=/home/testuser"))
	})

	It("prepends ~/.local/bin to PATH", func() {
		env := buildShellEnv("ses_test", nil, &dotfilesdv1.Shell{
			Env: map[string]string{
				"PATH": "/usr/local/bin:/usr/bin",
			},
		})
		Expect(env).To(ContainElement("PATH=/home/testuser/.local/bin:/usr/local/bin:/usr/bin"))
	})

	It("does not duplicate ~/.local/bin if already in PATH", func() {
		env := buildShellEnv("ses_test", nil, &dotfilesdv1.Shell{
			Env: map[string]string{
				"PATH": "/home/testuser/.local/bin:/usr/bin",
			},
		})
		Expect(env).To(ContainElement("PATH=/home/testuser/.local/bin:/usr/bin"))
		Expect(strings.Count(strings.Join(env, " "), "/home/testuser/.local/bin")).To(Equal(1))
	})

	It("sets fallback HOME and PATH when no CLI env provided", func() {
		env := buildShellEnv("ses_test", nil, nil)
		Expect(env).To(ContainElement("HOME=/home/testuser"))
		Expect(env).To(ContainElement(HavePrefix("PATH=")))
	})

	It("overrides HOME to daemon value even when CLI provides it", func() {
		env := buildShellEnv("ses_test", nil, &dotfilesdv1.Shell{
			Env: map[string]string{
				"HOME": "/wrong/home",
				"PATH": "/usr/bin",
			},
		})
		Expect(env).To(ContainElement("HOME=/home/testuser"))
		Expect(env).NotTo(ContainElement("HOME=/wrong/home"))
	})

	It("includes DOTFILESD_DAEMON, PORT, and SESSION vars", func() {
		env := buildShellEnv("ses_abc123", nil, &dotfilesdv1.Shell{Env: map[string]string{"PATH": "/usr/bin"}})
		Expect(env).To(ContainElement("DOTFILESD_DAEMON=1"))
		Expect(env).To(ContainElement("DOTFILESD_PORT=9105"))
		Expect(env).To(ContainElement("DOTFILESD_SESSION=ses_abc123"))
	})

	It("appends session variables on top", func() {
		env := buildShellEnv("ses_test", map[string]string{
			"MY_VAR":  "my_value",
			"ANOTHER": "123",
		}, &dotfilesdv1.Shell{
			Env: map[string]string{
				"PATH": "/usr/bin",
				"TERM": "xterm",
			},
		})
		Expect(env).To(ContainElement("MY_VAR=my_value"))
		Expect(env).To(ContainElement("ANOTHER=123"))
		Expect(env).To(ContainElement("PATH=/home/testuser/.local/bin:/usr/bin"))
		Expect(env).To(ContainElement("TERM=xterm"))
	})

	It("handles empty session variables", func() {
		env := buildShellEnv("ses_test", map[string]string{}, &dotfilesdv1.Shell{
			Env: map[string]string{"PATH": "/usr/bin"},
		})
		Expect(env).To(ContainElement("PATH=/home/testuser/.local/bin:/usr/bin"))
	})

	It("handles nil shellInfo", func() {
		env := buildShellEnv("ses_test", map[string]string{"FOO": "bar"}, nil)
		Expect(env).To(ContainElement("FOO=bar"))
		Expect(env).To(ContainElement(HavePrefix("PATH=")))
		Expect(env).To(ContainElement("HOME=/home/testuser"))
	})
})

var _ = Describe("bashQuote", func() {
	It("wraps simple string in single quotes", func() {
		Expect(bashQuote("hello")).To(Equal("'hello'"))
	})

	It("escapes embedded single quotes", func() {
		Expect(bashQuote("it's")).To(Equal("'it'\\''s'"))
	})

	It("handles empty string", func() {
		Expect(bashQuote("")).To(Equal("''"))
	})

	It("handles strings with special chars", func() {
		Expect(bashQuote("$HOME")).To(Equal("'$HOME'"))
		Expect(bashQuote("`pwd`")).To(Equal("'`pwd`'"))
	})
})

var _ = Describe("newShellSession", func() {
	var savedExecCommand func(string, ...string) *exec.Cmd
	var savedHome string

	BeforeEach(func() {
		savedExecCommand = execCommand
		savedHome = os.Getenv("HOME")
		os.Setenv("HOME", "/home/testuser")
		SetDaemonPort("9105")
	})

	AfterEach(func() {
		execCommand = savedExecCommand
		os.Setenv("HOME", savedHome)
	})

	It("creates a shell session with bash", func() {
		execCommand = func(name string, args ...string) *exec.Cmd {
			Expect(name).To(Equal("bash"))
			Expect(args).To(Equal([]string{"--norc", "--noprofile"}))
			// Return a command that runs cat (which reads stdin and writes it back).
			return exec.Command("sh", "-c", "while IFS= read -r line; do echo \"$line\"; done")
		}

		sh, err := newShellSession("ses_test", nil, nil)
		Expect(err).To(Succeed())
		Expect(sh).ToNot(BeNil())
		Expect(sh.cwd).To(Equal(""))
		sh.Close()
	})

	It("sets cmd.Dir from shellInfo.Cwd", func() {
		execCommand = func(name string, args ...string) *exec.Cmd {
			Expect(name).To(Equal("bash"))
			Expect(args).To(Equal([]string{"--norc", "--noprofile"}))
			return exec.Command("sh", "-c", "while IFS= read -r line; do echo \"$line\"; done")
		}

		sh, err := newShellSession("ses_test", nil, &dotfilesdv1.Shell{
			Cwd: "/tmp",
		})
		Expect(err).To(Succeed())
		Expect(sh).ToNot(BeNil())
		Expect(sh.cwd).To(Equal("/tmp"))
		Expect(sh.cmd.Dir).To(Equal("/tmp"))
		sh.Close()
	})

	It("sets environment from buildShellEnv", func() {
		execCommand = func(name string, args ...string) *exec.Cmd {
			Expect(name).To(Equal("bash"))
			Expect(args).To(Equal([]string{"--norc", "--noprofile"}))
			return exec.Command("sh", "-c", "while IFS= read -r line; do echo \"$line\"; done")
		}

		sh, err := newShellSession("ses_xyz", nil, &dotfilesdv1.Shell{
			Env: map[string]string{"PATH": "/usr/bin", "MY_TEST": "yes"},
		})
		Expect(err).To(Succeed())
		Expect(sh).ToNot(BeNil())
		Expect(sh.cmd.Env).To(ContainElement("DOTFILESD_SESSION=ses_xyz"))
		Expect(sh.cmd.Env).To(ContainElement("MY_TEST=yes"))
		sh.Close()
	})
})

var _ = Describe("shellSession.Exec", func() {
	var (
		sh  *shellSession
		err error
	)

	BeforeEach(func() {
		SetDaemonPort("9105")
	})

	AfterEach(func() {
		if sh != nil {
			sh.Close()
		}
	})

	Context("with a real shell", func() {
		BeforeEach(func() {
			// Use execCommand pointing to a real sh process (same as bash for these tests).
			execCommand = exec.Command
			sh, err = newShellSession("ses_exec_test", nil, nil)
			Expect(err).To(Succeed())
		})

		It("executes a simple command", func() {
			stdout, stderr, code := sh.Exec("echo hello", nil)
			Expect(stderr).To(BeEmpty())
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal("hello"))
		})

		It("captures multi-line output", func() {
			stdout, _, code := sh.Exec("echo line1 && echo line2", nil)
			Expect(code).To(Equal(0))
			lines := strings.Split(strings.TrimSpace(stdout), "\n")
			Expect(lines).To(HaveLen(2))
			Expect(lines[0]).To(Equal("line1"))
			Expect(lines[1]).To(Equal("line2"))
		})

		It("reports non-zero exit code from subshell", func() {
			_, _, code := sh.Exec("sh -c 'exit 42'", nil)
			Expect(code).To(Equal(42))
		})

		It("exits with 0 for successful commands", func() {
			_, _, code := sh.Exec("true", nil)
			Expect(code).To(Equal(0))
		})

		It("exits with 1 for false", func() {
			_, _, code := sh.Exec("false", nil)
			Expect(code).To(Equal(1))
		})

		It("injects session variables as exports", func() {
			stdout, _, code := sh.Exec("echo \"$TEST_SESSION_VAR\"", map[string]string{
				"TEST_SESSION_VAR": "session_value",
			})
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal("session_value"))
		})

		It("injects multiple session variables", func() {
			stdout, _, code := sh.Exec("echo \"$A-$B-$C\"", map[string]string{
				"A": "1",
				"B": "2",
				"C": "3",
			})
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal("1-2-3"))
		})

		It("preserves shell state between calls", func() {
			_, _, code := sh.Exec("export PERSIST=yes", nil)
			Expect(code).To(Equal(0))

			stdout, _, code := sh.Exec("echo \"$PERSIST\"", nil)
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal("yes"))
		})

		It("cds to cwd before each command", func() {
			sh.Close()

			sh, err = newShellSession("ses_cwd_test", nil, &dotfilesdv1.Shell{
				Cwd: "/tmp",
			})
			Expect(err).To(Succeed())

			stdout, _, code := sh.Exec("pwd", nil)
			Expect(code).To(Equal(0))
			Expect(strings.TrimSpace(stdout)).To(Equal("/tmp"))
		})
	})
})

var _ = Describe("Session", func() {
	var (
		s *Session
	)

	BeforeEach(func() {
		s = newSession("ses_unit_001")
	})

	It("creates a new session with the given ID", func() {
		Expect(s.id).To(Equal("ses_unit_001"))
		Expect(s.finalized).To(BeFalse())
		Expect(s.requestCount).To(Equal(0))
		Expect(s.data).ToNot(BeNil())
	})

	It("touch increments request count and updates lastActive", func() {
		oldActive := s.lastActive
		s.touch()
		Expect(s.requestCount).To(Equal(1))
		Expect(s.lastActive).To(BeTemporally(">=", oldActive))
	})

	It("SetVariables stores and Variables retrieves", func() {
		s.SetVariables(map[string]string{"FOO": "bar", "BAZ": "qux"})
		vars := s.Variables()
		Expect(vars).To(HaveLen(2))
		Expect(vars["FOO"]).To(Equal("bar"))
		Expect(vars["BAZ"]).To(Equal("qux"))
	})

	It("SetVariables merges with existing variables", func() {
		s.SetVariables(map[string]string{"A": "1"})
		s.SetVariables(map[string]string{"B": "2"})
		vars := s.Variables()
		Expect(vars).To(HaveLen(2))
		Expect(vars["A"]).To(Equal("1"))
		Expect(vars["B"]).To(Equal("2"))
	})

	It("SetVariables with empty map does not change existing vars", func() {
		s.SetVariables(map[string]string{"A": "1"})
		s.SetVariables(nil)
		vars := s.Variables()
		Expect(vars).To(HaveLen(1))
		Expect(vars["A"]).To(Equal("1"))
	})

	It("Variables returns a copy of the map", func() {
		s.SetVariables(map[string]string{"KEY": "val"})
		vars := s.Variables()
		vars["KEY"] = "modified"
		Expect(s.Variables()["KEY"]).To(Equal("val"))
	})

	It("toProto returns a protobuf representation", func() {
		s.SetVariables(map[string]string{"X": "y"})
		s.SetShellInfo(&dotfilesdv1.Shell{
			CurrentShell: "/bin/zsh",
			Cwd:          "/home/user",
			Env:          map[string]string{"PATH": "/usr/bin"},
		})
		s.touch()

		pb := s.toProto()
		Expect(pb.Id).To(Equal("ses_unit_001"))
		Expect(pb.Finalized).To(BeFalse())
		Expect(pb.RequestCount).To(Equal(int32(1)))
		Expect(pb.Variables["X"]).To(Equal("y"))
		Expect(pb.Shell.CurrentShell).To(Equal("/bin/zsh"))
		Expect(pb.Shell.Cwd).To(Equal("/home/user"))
		Expect(pb.Shell.Env["PATH"]).To(Equal("/usr/bin"))
	})

	It("toProto returns empty shell when not set", func() {
		pb := s.toProto()
		Expect(pb.Shell).To(BeNil())
	})

	It("SetShellInfo stores a deep copy", func() {
		orig := &dotfilesdv1.Shell{
			CurrentShell: "/bin/zsh",
			Cwd:          "/home/user",
			Env:          map[string]string{"K": "v"},
		}
		s.SetShellInfo(orig)

		orig.CurrentShell = "/bin/bash"
		orig.Cwd = "/tmp"
		orig.Env["K"] = "modified"

		Expect(s.shellInfo.CurrentShell).To(Equal("/bin/zsh"))
		Expect(s.shellInfo.Cwd).To(Equal("/home/user"))
		Expect(s.shellInfo.Env["K"]).To(Equal("v"))
	})

	It("SetShellInfo with nil does nothing", func() {
		s.SetShellInfo(nil)
		Expect(s.shellInfo).To(BeNil())
	})

	It("SetCallbackURL stores and HasCallbackURL checks", func() {
		Expect(s.HasCallbackURL()).To(BeFalse())
		s.SetCallbackURL("http://127.0.0.1:12345")
		Expect(s.HasCallbackURL()).To(BeTrue())
	})

	It("ensureShell creates a new shell session lazily", func() {
		execCommand = exec.Command
		sh, err := s.ensureShell()
		Expect(err).To(Succeed())
		Expect(sh).ToNot(BeNil())
		Expect(s.shell).ToNot(BeNil())

		// Second call reuses the same shell.
		sh2, err := s.ensureShell()
		Expect(err).To(Succeed())
		Expect(sh2).To(Equal(sh))
	})

	It("ensureShell returns error if session is finalized", func() {
		s.finalized = true
		_, err := s.ensureShell()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("finalized"))
	})

	It("closeShell closes and nils the shell", func() {
		execCommand = exec.Command
		_, err := s.ensureShell()
		Expect(err).To(Succeed())
		Expect(s.shell).ToNot(BeNil())

		s.closeShell()
		Expect(s.shell).To(BeNil())
	})
})

var _ = Describe("SessionStore", func() {
	var (
		store *SessionStore
	)

	BeforeEach(func() {
		store = NewSessionStore()
	})

	It("Create creates a new session with a non-empty ID", func() {
		s := store.Create("")
		Expect(s.id).ToNot(BeEmpty())
		Expect(store.Get(s.id)).To(Equal(s))
	})

	It("CreateEphemeral creates a session with empty ID", func() {
		s := store.CreateEphemeral()
		Expect(s.id).To(BeEmpty())
	})

	It("Get returns nil for unknown session", func() {
		Expect(store.Get("nonexistent")).To(BeNil())
	})

	It("Finalize marks a session as finalized and closes its shell", func() {
		s := store.Create("")
		execCommand = exec.Command
		_, err := s.ensureShell()
		Expect(err).To(Succeed())

		ok := store.Finalize(s.id)
		Expect(ok).To(BeTrue())
		Expect(s.finalized).To(BeTrue())
		Expect(s.shell).To(BeNil())
	})

	It("Finalize returns false for unknown session", func() {
		Expect(store.Finalize("nonexistent")).To(BeFalse())
	})

	It("List returns only non-finalized sessions", func() {
		s1 := store.Create("")
		s2 := store.Create("")
		store.Create("") // s3 — will remain active
		store.Finalize(s2.id)

		list := store.List()
		Expect(list).To(HaveLen(2))
		Expect(list).To(ContainElement(s1))
		Expect(list).NotTo(ContainElement(s2))
	})

	It("List returns empty slice when no sessions", func() {
		list := store.List()
		Expect(list).To(BeEmpty())
	})

	Describe("ResolveSession", func() {
		It("creates ephemeral for nil message", func() {
			s := store.ResolveSession(nil)
			Expect(s.id).To(BeEmpty())
		})

		It("creates ephemeral for empty ID", func() {
			s := store.ResolveSession(&dotfilesdv1.Session{})
			Expect(s.id).To(BeEmpty())
		})

		It("resolves an existing session by ID", func() {
			existing := store.Create("")
			s := store.ResolveSession(&dotfilesdv1.Session{Id: existing.id})
			Expect(s.id).To(Equal(existing.id))
		})

		It("creates ephemeral for unknown ID", func() {
			s := store.ResolveSession(&dotfilesdv1.Session{Id: "unknown"})
			Expect(s.id).To(BeEmpty())
		})

		It("creates ephemeral for finalized session", func() {
			existing := store.Create("")
			store.Finalize(existing.id)
			s := store.ResolveSession(&dotfilesdv1.Session{Id: existing.id})
			Expect(s.id).To(BeEmpty())
		})

		It("sets variables from session message", func() {
			s := store.ResolveSession(&dotfilesdv1.Session{
				Variables: map[string]string{"FROM": "msg"},
			})
			Expect(s.id).To(BeEmpty())
			Expect(s.Variables()).To(HaveKeyWithValue("FROM", "msg"))
		})

		It("sets shell info from session message", func() {
			// First call with unknown ID creates ephemeral — shell info not set on ephemerals.
			store.ResolveSession(&dotfilesdv1.Session{
				Id: "custom",
				Shell: &dotfilesdv1.Shell{
					CurrentShell: "/bin/zsh",
					Cwd:          "/home/user",
				},
			})
			existing := store.Create("")
			s2 := store.ResolveSession(&dotfilesdv1.Session{
				Id: existing.id,
				Shell: &dotfilesdv1.Shell{
					CurrentShell: "/bin/zsh",
					Cwd:          "/home/user",
				},
			})
			Expect(s2.id).To(Equal(existing.id))
			Expect(s2.shellInfo).ToNot(BeNil())
			Expect(s2.shellInfo.CurrentShell).To(Equal("/bin/zsh"))
			Expect(s2.shellInfo.Cwd).To(Equal("/home/user"))
		})
	})
})
