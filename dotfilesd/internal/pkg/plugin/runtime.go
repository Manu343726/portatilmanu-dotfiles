package plugin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

// Handshake is the JSON message a plugin writes to stdout after startup.
type Handshake struct {
	Protocol  string `json:"protocol"`
	URL       string `json:"url"`
	SessionID string `json:"session_id"`
}

// Process wraps a running plugin subprocess.
type Process struct {
	Cmd       *exec.Cmd
	URL       string // plugin's Extension Service URL
	SessionID string
	cleanup   func()
	mu        sync.Mutex
	done      chan struct{}
}

// Launch starts a plugin binary and performs the handshake.
//
// envVars are additional environment variables passed to the plugin (beyond
// EXECUTION_CONTEXT_URL, EXECUTION_CONTEXT_TOKEN, and SESSION_ID which are
// handled by Launch automatically).
func Launch(binaryPath, ctxURL, ctxToken, sessionID string, envVars map[string]string) (*Process, error) {
	cmd := exec.Command(binaryPath)

	// Build environment.
	cmd.Env = append(os.Environ(),
		"EXECUTION_CONTEXT_URL="+ctxURL,
		"EXECUTION_CONTEXT_TOKEN="+ctxToken,
		"SESSION_ID="+sessionID,
	)
	for k, v := range envVars {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Capture stdout for handshake parsing.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start plugin: %w", err)
	}

	// Read one JSON line for the handshake.
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("read handshake: %w", err)
	}

	var hs Handshake
	if err := json.Unmarshal([]byte(line), &hs); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("parse handshake: %w", err)
	}

	if hs.Protocol != "dotfilesd-extension-v1" {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("unknown protocol %q", hs.Protocol)
	}

	if hs.URL == "" {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("handshake missing URL")
	}

	done := make(chan struct{})
	proc := &Process{
		Cmd:       cmd,
		URL:       hs.URL,
		SessionID: hs.SessionID,
		done:      done,
	}

	// Monitor process completion.
	go func() {
		cmd.Wait()
		close(done)
	}()

	proc.cleanup = func() {
		proc.mu.Lock()
		defer proc.mu.Unlock()
		if proc.Cmd != nil && proc.Cmd.Process != nil {
			proc.Cmd.Process.Kill()
		}
		<-proc.done
	}

	return proc, nil
}

// Kill terminates the plugin process.
func (p *Process) Kill() {
	if p.cleanup != nil {
		p.cleanup()
	}
}

// Alive reports whether the plugin process is still running.
func (p *Process) Alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}
