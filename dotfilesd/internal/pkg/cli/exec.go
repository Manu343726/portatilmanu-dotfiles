package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"connectrpc.com/connect"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
)

func RunExec(clients *Clients, sessionID, command string) error {
	req := connect.NewRequest(&dotfilesdv1.ExecRequest{
		Command: command,
	})
	if sessionID != "" {
		req.Header().Set("Session-Id", sessionID)
	}
	resp, err := clients.Exec.Exec(context.Background(), req)
	if err != nil {
		return fmt.Errorf("exec failed: %w", err)
	}
	if resp.Msg.Stdout != "" {
		fmt.Print(resp.Msg.Stdout)
	}
	if resp.Msg.Stderr != "" {
		fmt.Fprint(os.Stderr, resp.Msg.Stderr)
	}
	if resp.Msg.ExitCode != 0 {
		os.Exit(int(resp.Msg.ExitCode))
	}
	return nil
}

func RunSubmitFeedback(clients *Clients, sessionID, feedbackID, data string) (string, error) {
	req := connect.NewRequest(&dotfilesdv1.SubmitFeedbackRequest{
		FeedbackId: feedbackID,
		Data:       data,
	})
	if sessionID != "" {
		req.Header().Set("Session-Id", sessionID)
	}
	resp, err := clients.Exec.SubmitFeedback(context.Background(), req)
	if err != nil {
		return "", fmt.Errorf("submit feedback: %w", err)
	}
	if resp.Msg.FeedbackRequired != nil {
		return "", fmt.Errorf("feedback required: %s", resp.Msg.FeedbackRequired.Prompt)
	}
	return resp.Msg.Stdout, nil
}

func RunSudoExec(clients *Clients, sessionID, command string) error {
	method := dotfilesdv1.SudoMethod_SUDO_METHOD_TERMINAL
	if _, err := os.Stat("/dev/tty"); os.IsNotExist(err) {
		method = dotfilesdv1.SudoMethod_SUDO_METHOD_GRAPHICAL
	}

	req := connect.NewRequest(&dotfilesdv1.SudoExecRequest{
		Command: command, PreferredMethod: method,
	})
	if sessionID != "" {
		req.Header().Set("Session-Id", sessionID)
	}
	resp, err := clients.Exec.SudoExec(context.Background(), req)
	if err != nil {
		return fmt.Errorf("sudo exec: %w", err)
	}

	switch o := resp.Msg.Outcome.(type) {
	case *dotfilesdv1.SudoExecResponse_Result:
		r := o.Result
		if r.AuthCancelled {
			return fmt.Errorf("sudo failed: %s", r.Stderr)
		}
		if r.Stdout != "" {
			fmt.Print(r.Stdout)
		}
		if r.Stderr != "" {
			fmt.Fprint(os.Stderr, r.Stderr)
		}
		if r.ExitCode != 0 {
			os.Exit(int(r.ExitCode))
		}
		return nil

	case *dotfilesdv1.SudoExecResponse_AuthChallenge:
		challenge := o.AuthChallenge
		fmt.Fprint(os.Stderr, challenge.Prompt)
		var password string
		if fd := int(os.Stdin.Fd()); term.IsTerminal(fd) {
			raw, err := term.ReadPassword(fd)
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			password = string(raw)
			fmt.Fprintln(os.Stderr)
		} else if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
			raw, err := term.ReadPassword(int(tty.Fd()))
			tty.Close()
			if err != nil {
				return fmt.Errorf("read password from tty: %w", err)
			}
			password = string(raw)
			fmt.Fprintln(os.Stderr)
		} else {
			reader := bufio.NewReader(os.Stdin)
			password, err = reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			password = strings.TrimRight(password, "\n\r")
		}

		req := connect.NewRequest(&dotfilesdv1.SudoExecRequest{
			Command: command, Password: password, PreferredMethod: dotfilesdv1.SudoMethod_SUDO_METHOD_TERMINAL,
		})
		if sessionID != "" {
			req.Header().Set("Session-Id", sessionID)
		}
		resp, err = clients.Exec.SudoExec(context.Background(), req)
		if err != nil {
			return fmt.Errorf("sudo exec with password: %w", err)
		}

		r := resp.Msg.GetResult()
		if r == nil {
			return fmt.Errorf("unexpected response after auth")
		}
		if r.AuthCancelled {
			return fmt.Errorf("sudo failed: %s", r.Stderr)
		}
		if r.Stdout != "" {
			fmt.Print(r.Stdout)
		}
		if r.Stderr != "" {
			fmt.Fprint(os.Stderr, r.Stderr)
		}
		if r.ExitCode != 0 {
			os.Exit(int(r.ExitCode))
		}
		return nil

	default:
		return fmt.Errorf("unexpected response type from daemon")
	}
}
