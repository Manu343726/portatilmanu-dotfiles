package plugin

import (
	"context"
	"fmt"
	"io"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// BackgroundTask represents a long-running command executing on the daemon.
// It provides io-based access to the command's stdin, stdout, and stderr,
// plus cancellation and wait-for-completion.
//
// Usage:
//
//	task, err := ctx.BackgroundExec("pacman -Syu --noconfirm", true)
//	if err != nil { ... }
//
//	// Stream stdout to the plugin's own output.
//	go func() { io.Copy(ctx.Stdout(), task.Stdout()) }()
//
//	// Wait for completion.
//	exitCode, err := task.Wait()
type BackgroundTask interface {
	// Stdin returns a writer connected to the command's stdin.
	// Close it to send EOF (or the command may hang waiting for input).
	Stdin() io.WriteCloser

	// Stdout returns a reader for the command's merged stdout+stderr.
	// Read from it to consume output as it's produced. If the task
	// completes, the reader returns EOF after all buffered data.
	Stdout() io.Reader

	// Cancel kills the running command. Subsequent calls to Wait() will
	// return with the killed exit code.
	Cancel() error

	// Wait blocks until the command exits. Returns the exit code.
	// If the command could not be started, err is non-nil with the
	// daemon's error message.
	Wait() (exitCode int, err error)
}

// backgroundTaskClient implements BackgroundTask over a Connect bidi stream.
type backgroundTaskClient struct {
	stream   *connect.BidiStreamForClient[
		dotfilesdv1.BackgroundExecRequest,
		dotfilesdv1.BackgroundExecResponse,
	]

	stdinW *io.PipeWriter
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter

	taskID     string
	cancelSent bool
	exitCode   int
	exitErr    error
	done       chan struct{}
}

// BackgroundExec starts a command in the background on the daemon and
// returns a BackgroundTask for controlling and monitoring it.
func (c *contextClient) BackgroundExec(cmd string, sudo bool) (BackgroundTask, error) {
	stream := c.execClient.BackgroundExec(context.Background())
	if c.token != "" {
		stream.RequestHeader().Set("X-Dotfiles-Context-Token", c.token)
	}

	// Send the start message.
	if err := stream.Send(&dotfilesdv1.BackgroundExecRequest{
		Session: c.buildSession(),
		Action: &dotfilesdv1.BackgroundExecRequest_Start{
			Start: &dotfilesdv1.StartCommand{
				Command: cmd,
				Sudo:    sudo,
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("background exec start: %w", err)
	}

	// Read the started event from the server.
	msg, err := stream.Receive()
	if err != nil {
		return nil, fmt.Errorf("background exec receive started: %w", err)
	}
	started := msg.GetStarted()
	if started == nil {
		return nil, fmt.Errorf("expected started event, got %T", msg.Event)
	}

	// Set up pipes for stdout.
	stdoutR, stdoutW := io.Pipe()

	task := &backgroundTaskClient{
		stream:   stream,
		stdoutR:  stdoutR,
		stdoutW:  stdoutW,
		taskID:   started.TaskId,
		done:     make(chan struct{}),
	}

	// Start goroutine to read server→client messages.
	go task.readLoop()

	return task, nil
}

// readLoop reads messages from the server stream and routes them to the
// stdout pipe or captures the exit event.
func (t *backgroundTaskClient) readLoop() {
	defer close(t.done)
	defer t.stdoutW.Close()

	for {
		msg, err := t.stream.Receive()
		if err != nil {
			t.exitErr = fmt.Errorf("background exec stream: %w", err)
			return
		}

		switch ev := msg.Event.(type) {
		case *dotfilesdv1.BackgroundExecResponse_StdoutChunk:
			if len(ev.StdoutChunk) > 0 {
				if _, werr := t.stdoutW.Write(ev.StdoutChunk); werr != nil {
					return // reader closed
				}
			}
		case *dotfilesdv1.BackgroundExecResponse_StderrChunk:
			if len(ev.StderrChunk) > 0 {
				if _, werr := t.stdoutW.Write(ev.StderrChunk); werr != nil {
					return
				}
			}
		case *dotfilesdv1.BackgroundExecResponse_Exit:
			t.exitCode = int(ev.Exit.ExitCode)
			if ev.Exit.ErrorMessage != "" {
				t.exitErr = fmt.Errorf("%s", ev.Exit.ErrorMessage)
			}
			return
		}
	}
}

func (t *backgroundTaskClient) Stdin() io.WriteCloser {
	stdinR, stdinW := io.Pipe()
	t.stdinW = stdinW

	// Goroutine to read from the pipe and send to the server.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdinR.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				if sendErr := t.stream.Send(&dotfilesdv1.BackgroundExecRequest{
					Action: &dotfilesdv1.BackgroundExecRequest_StdinChunk{StdinChunk: chunk},
				}); sendErr != nil {
					return
				}
			}
			if err != nil {
				return // EOF or error
			}
		}
	}()

	return stdinW
}

func (t *backgroundTaskClient) Stdout() io.Reader {
	return t.stdoutR
}

func (t *backgroundTaskClient) Cancel() error {
	t.cancelSent = true
	return t.stream.Send(&dotfilesdv1.BackgroundExecRequest{
		Action: &dotfilesdv1.BackgroundExecRequest_Cancel{Cancel: true},
	})
}

func (t *backgroundTaskClient) Wait() (int, error) {
	<-t.done
	return t.exitCode, t.exitErr
}
