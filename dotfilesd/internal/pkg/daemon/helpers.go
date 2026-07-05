package daemon

import (
	"bufio"
	"context"
	"fmt"
	"io"
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
