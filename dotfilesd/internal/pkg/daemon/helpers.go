package daemon

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// execCommand is used instead of exec.Command directly so tests can replace it.
var execCommand = exec.Command

func hasSudo() bool {
	_, err := exec.LookPath("sudo")
	return err == nil
}

func hasPkexec() bool {
	_, err := exec.LookPath("pkexec")
	return err == nil
}

// elicitationUnavailable reports whether the session should skip the
// elicitation prompt: either it already failed once, or the client is known to
// advertise elicitation but never render the form (opencode, see opencode issue
// #51856). Skipping avoids paying the bounded elicitation timeout on every new
// session for these clients; they go straight to the graphical/terminal path.
func elicitationUnavailable(vars map[string]string) bool {
	if vars["_elicitation_unavailable"] == "true" {
		return true
	}
	name := strings.ToLower(vars["_cap_client_name"])
	return strings.Contains(name, "opencode")
}

// Authentication sentinel errors.
var (
	// errAuthCancelled means the user dismissed the authentication dialog.
	errAuthCancelled = errors.New("authentication cancelled")
	// errNoGraphicalAskpass means no GUI password-prompt tool is available.
	errNoGraphicalAskpass = errors.New("no graphical askpass available")
)

// graphicalAskpassCandidates lists GUI password-prompt tools in preference
// order. Each reads the password into a dialog and writes it to stdout, which
// lets the daemon capture it and cache it per session — unlike pkexec, whose
// polkit authentication happens out-of-band and never reaches the daemon.
var graphicalAskpassCandidates = []string{
	"zenity", "yad", "kdialog", "ksshaskpass", "ssh-askpass", "lxqt-openssh-askpass",
}

// findGraphicalAskpass returns the first available GUI askpass tool, or "".
func findGraphicalAskpass() string {
	for _, c := range graphicalAskpassCandidates {
		if _, err := exec.LookPath(c); err == nil {
			return c
		}
	}
	return ""
}

// hasGraphicalAskpass reports whether the daemon can show its own GUI password
// dialog: it needs a display in its environment (the systemd user service
// inherits DISPLAY/XAUTHORITY) and one of the askpass tools.
func hasGraphicalAskpass() bool {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	return findGraphicalAskpass() != ""
}

// promptGraphicalPassword shows a GUI password dialog on the user's session
// display and returns the entered password. Returns errAuthCancelled if the
// user cancels and errNoGraphicalAskpass if no tool is available.
func promptGraphicalPassword(ctx context.Context, prompt string) ([]byte, error) {
	tool := findGraphicalAskpass()
	if tool == "" {
		return nil, errNoGraphicalAskpass
	}
	title := "dotfilesd: sudo authentication"
	var args []string
	switch tool {
	case "zenity", "yad":
		args = []string{"--password", "--title=" + title}
	case "kdialog":
		args = []string{"--password", prompt, "--title", title}
	default: // ssh-askpass style: the prompt is the argument.
		args = []string{prompt}
	}
	cmd := exec.CommandContext(ctx, tool, args...)
	var out strings.Builder
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, errAuthCancelled
	}
	pwd := []byte(strings.TrimRight(out.String(), "\r\n"))
	if len(pwd) == 0 {
		return nil, errAuthCancelled
	}
	return pwd, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func runCmd(name string, args ...string) (string, error) {
	out, err := execCommand(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func runCmdFull(name string, args ...string) (string, string, int) {
	var stdout, stderr strings.Builder
	cmd := execCommand(name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return stdout.String(), stderr.String(), exitCode
}

// runCmdFullWithStdin runs a command with a stdin string and returns
// stdout, stderr, and exit code.
func runCmdFullWithStdin(stdin, name string, args ...string) (string, string, int) {
	var stdout, stderr strings.Builder
	cmd := execCommand(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return stdout.String(), stderr.String(), exitCode
}

// runCmdStream runs a command and streams stdout/stderr chunks to a
// Connect server stream. Each chunk is sent as an ExecStreamResponse.
// Stderr is merged with stdout (both go to stdout_chunk) since there's
// no clean cross-platform way to interleave two pipes without deadlocks.
func runCmdStream(
	ctx context.Context,
	stream *connect.ServerStream[dotfilesdv1.ExecStreamResponse],
	command string,
) error {
	cmd := execCommand("sh", "-c", command)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout // stderr merged into stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}

	// Read chunks in a goroutine, send them on the stream.
	reader := bufio.NewReader(stdout)
	buf := make([]byte, 4096)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if err := stream.Send(&dotfilesdv1.ExecStreamResponse{
				StdoutChunk: chunk,
			}); err != nil {
				// Client disconnected; kill the command.
				_ = cmd.Process.Kill()
				return err
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			// Pipe error — send as stderr, treat as done.
			_ = stream.Send(&dotfilesdv1.ExecStreamResponse{
				Done:         true,
				ExitCode:     -1,
				ErrorMessage: readErr.Error(),
			})
			_ = cmd.Process.Kill()
			return nil
		}
	}

	err = cmd.Wait()
	exitCode := int32(0)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		} else {
			exitCode = -1
		}
	}

	return stream.Send(&dotfilesdv1.ExecStreamResponse{
		Done:     true,
		ExitCode: exitCode,
	})
}

// runCmdStreamWithSudo runs a command with pkexec, streaming output.
func runCmdStreamWithSudo(
	ctx context.Context,
	stream *connect.ServerStream[dotfilesdv1.ExecStreamResponse],
	command string,
) error {
	escaped := strings.ReplaceAll(command, "'", "'\\''")
	return runCmdStream(ctx, stream, fmt.Sprintf("pkexec sh -c '%s'", escaped))
}

func fmtSscanf(str string, v any) (int, error) {
	return fmt.Sscanf(str, "%d", v)
}

// zeroBytes overwrites the backing array of b with zeroes.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
