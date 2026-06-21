package daemon

import (
	"fmt"
	"os/exec"
	"strings"
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

func fmtSscanf(str string, v any) (int, error) {
	return fmt.Sscanf(str, "%d", v)
}

// zeroBytes overwrites the backing array of b with zeroes.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
