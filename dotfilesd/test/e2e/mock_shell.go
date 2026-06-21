// Package e2e provides a mock shell runtime for testing shellSession.Exec
// and related execution flows without depending on a real bash process.
//
// The mock shell reads lines from stdin, executes them via exec.Command("sh", "-c", ...),
// and writes output with the delimiter protocol that shellSession.Exec expects.
package e2e

import (
	"bufio"
	"fmt"
	"io"
	"math/rand"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// MockShell simulates the daemon's shellSession by running a real sh process
// with stdin/stdout pipes and the same delimiter-based output capture protocol.
// Unlike the real shellSession which uses bash --norc --noprofile, MockShell
// uses /bin/sh for portability in tests.
type MockShell struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	mu     sync.Mutex
	cwd    string
	env    []string
}

// MockShellConfig configures a MockShell instance.
type MockShellConfig struct {
	Cwd string
	Env []string
}

// NewMockShell creates and starts a mock shell process.
// The shell reads commands from stdin and writes delimited output to stdout.
func NewMockShell(cfg MockShellConfig) (*MockShell, error) {
	cmd := exec.Command("sh")
	if cfg.Env != nil {
		cmd.Env = cfg.Env
	}
	if cfg.Cwd != "" {
		cmd.Dir = cfg.Cwd
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mock shell stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mock shell stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mock shell start: %w", err)
	}

	return &MockShell{
		cmd:    cmd,
		stdin:  stdin,
		reader: bufio.NewReader(stdout),
		cwd:    cfg.Cwd,
	}, nil
}

// Exec mimics shellSession.Exec: writes a command to the mock shell's stdin
// and reads output until the delimiter line, then returns stdout, stderr, exitCode.
func (ms *MockShell) Exec(command string, variables map[string]string) (string, string, int) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	delim := fmt.Sprintf("__GS_%x__", rand.Int63())

	// Prepend cd to cwd.
	var prefix string
	if ms.cwd != "" {
		prefix += fmt.Sprintf("cd %s\n", bashQuote(ms.cwd))
	}
	// Export variables.
	for k, v := range variables {
		prefix += fmt.Sprintf("export %s=%s; ", k, bashQuote(v))
	}
	cmdLine := fmt.Sprintf("%s%s 2>&1\necho \"%s=$?\"\n", prefix, command, delim)

	if _, err := io.WriteString(ms.stdin, cmdLine); err != nil {
		return "", "", -1
	}

	var output strings.Builder
	for {
		line, err := ms.reader.ReadString('\n')
		if err != nil {
			return output.String(), "", -1
		}
		line = strings.TrimSuffix(line, "\n")
		if strings.HasPrefix(line, delim+"=") {
			codeStr := strings.TrimPrefix(line, delim+"=")
			code, err := strconv.Atoi(strings.TrimSpace(codeStr))
			if err != nil {
				return output.String(), "", -1
			}
			return output.String(), "", code
		}
		output.WriteString(line)
		output.WriteByte('\n')
	}
}

// SendExecute writes a command to the mock shell and returns the raw output
// up to and including the delimiter line. Useful for testing the protocol layer.
func (ms *MockShell) SendExecute(command string) (string, int, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	delim := fmt.Sprintf("__GS_%x__", rand.Int63())
	cmdLine := fmt.Sprintf("%s 2>&1\necho \"%s=$?\"\n", command, delim)

	if _, err := io.WriteString(ms.stdin, cmdLine); err != nil {
		return "", -1, fmt.Errorf("write stdin: %w", err)
	}

	var output strings.Builder
	for {
		line, err := ms.reader.ReadString('\n')
		if err != nil {
			return output.String(), -1, fmt.Errorf("read stdout: %w", err)
		}
		line = strings.TrimSuffix(line, "\n")
		if strings.HasPrefix(line, delim+"=") {
			codeStr := strings.TrimPrefix(line, delim+"=")
			code, err := strconv.Atoi(strings.TrimSpace(codeStr))
			if err != nil {
				return output.String(), -1, fmt.Errorf("parse exit code %q: %w", codeStr, err)
			}
			return output.String(), code, nil
		}
		output.WriteString(line)
		output.WriteByte('\n')
	}
}

// Close kills the mock shell process.
func (ms *MockShell) Close() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.cmd != nil && ms.cmd.Process != nil {
		return ms.cmd.Process.Kill()
	}
	return nil
}

func bashQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
