package plugin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
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
//
// Debug mode: If DOTFILESD_PLUGIN_DEBUG is set, the plugin process is launched
// under Delve's headless debugger. Set DOTFILESD_PLUGIN_DLV_PORT to specify
// the Delve listen port (default: 23450). Use a VS Code "attach" config
// (mode: remote) to connect to that port and set breakpoints.
func Launch(binaryPath, ctxURL, ctxToken, sessionID string, envVars map[string]string) (*Process, error) {
	slog.Debug("launching plugin process", "binary", binaryPath, "ctx_url", ctxURL, "session_id", sessionID)

	// Debug mode: if DOTFILESD_PLUGIN_DEBUG matches this plugin's name (or is "*"),
	// wrap the binary with `dlv exec --headless` so you can attach a debugger.
	debugTarget := os.Getenv("DOTFILESD_PLUGIN_DEBUG")
	pluginName := strings.TrimPrefix(sessionID, "plugin-")

	cmd := exec.Command(binaryPath)
	if debugTarget != "" && (debugTarget == pluginName || debugTarget == "*") {
		dlvPort := os.Getenv("DOTFILESD_PLUGIN_DLV_PORT")
		if dlvPort == "" {
			dlvPort = "23450"
		}
		// Validate port so we don't pass garbage to dlv.
		if _, err := strconv.Atoi(dlvPort); err != nil {
			return nil, fmt.Errorf("invalid DOTFILESD_PLUGIN_DLV_PORT %q: %w", dlvPort, err)
		}
		dlvPath, err := exec.LookPath("dlv")
		if err != nil {
			return nil, fmt.Errorf("DOTFILESD_PLUGIN_DEBUG is set but dlv not found in PATH: %w", err)
		}
		slog.Info("plugin debug mode enabled via dlv", "plugin", binaryPath, "dlv", dlvPath, "port", dlvPort)
		cmd = exec.Command(dlvPath,
			"exec", "--headless",
			"--listen=:"+dlvPort,
			"--api-version=2",
			"--accept-multiclient",
			"--continue",
			"--check-go-version=false",
			binaryPath,
		)
	}

	// Build environment.
	cmd.Env = append(os.Environ(),
		"EXECUTION_CONTEXT_URL="+ctxURL,
		"EXECUTION_CONTEXT_TOKEN="+ctxToken,
		"SESSION_ID="+sessionID,
	)
	for k, v := range envVars {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	slog.Debug("plugin env set", "binary", binaryPath, "env_count", len(cmd.Env), "extra_vars", len(envVars))

	// Capture stdout for handshake parsing.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start plugin: %w", err)
	}
	slog.Debug("plugin process started", "binary", binaryPath, "pid", cmd.Process.Pid)

	// Read one JSON line for the handshake.
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("read handshake: %w", err)
	}
	slog.Debug("plugin handshake line received", "binary", binaryPath, "line", line[:min(len(line), 200)])

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
	slog.Debug("plugin handshake validated", "binary", binaryPath, "protocol", hs.Protocol, "url", hs.URL, "session_id", hs.SessionID)

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
		ps, _ := cmd.Process.Wait()
		slog.Debug("plugin process exited", "binary", binaryPath, "pid", cmd.Process.Pid, "state", ps.String())
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

// Done returns a channel that is closed when the plugin process exits.
// This allows the daemon to wait for or detect process termination
// without polling.
func (p *Process) Done() <-chan struct{} {
	return p.done
}
