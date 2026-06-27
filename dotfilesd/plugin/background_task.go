package plugin

import (
	"context"
	"fmt"
	"io"
	"sync"

	dotfilesdv1 "dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

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
//	// Tee: stream output to the plugin's own writers AND get readers
//	// for the plugin to process the output programmatically.
//	stdoutR, stderrR := task.Tee()
//	go processStdout(stdoutR)
//	go processStderr(stderrR)
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

	// Tee pipes the command's stdout and stderr to the plugin's
	// Stdout()/Stderr() writers in real time AND returns independent
	// readers for each stream. The plugin can read from the returned
	// readers to process output programmatically while it also appears
	// in the plugin's output.
	//
	// Tee may only be called once. Subsequent calls return the same
	// readers. If Tee is not called, output only goes to Stdout().
	Tee() (stdout, stderr io.Reader)

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
	stream *connect.BidiStreamForClient[
		dotfilesdv1.BackgroundExecRequest,
		dotfilesdv1.BackgroundExecResponse,
	]

	stdinW  *io.PipeWriter
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter

	// Writers for the plugin's output channels (set at construction).
	// When Tee() is called, chunks are also written here.
	ctxStdout io.Writer
	ctxStderr io.Writer

	// Tee state.
	teeMu      sync.Mutex
	teeStdoutR *io.PipeReader
	teeStdoutW *io.PipeWriter
	teeStderrR *io.PipeReader
	teeStderrW *io.PipeWriter
	teeActive  bool

	taskID     string
	cancelSent bool
	exitCode   int
	exitErr    error
	done       chan struct{}
}

// BackgroundExec starts a command in the background on the daemon and
// returns a BackgroundTask for controlling and monitoring it.
func (c *contextClient) BackgroundExec(cmd string, sudo bool) (BackgroundTask, error) {
	return startBackgroundTask(c.execClient, c.token, c.buildSession(), c.Stdout(), c.Stderr(), cmd, sudo)
}

// startBackgroundTask is the shared implementation. It is also called
// by streamingContext with real stdout/stderr writers.
func startBackgroundTask(
	execClient dotfilesdv1connect.ExecServiceClient,
	token string,
	session *dotfilesdv1.Session,
	ctxStdout, ctxStderr io.Writer,
	cmd string,
	sudo bool,
) (BackgroundTask, error) {
	stream := execClient.BackgroundExec(context.Background())
	if token != "" {
		stream.RequestHeader().Set("X-Dotfiles-Context-Token", token)
	}

	// Send the start message.
	if err := stream.Send(&dotfilesdv1.BackgroundExecRequest{
		Session: session,
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

	// Set up pipes for the merged stdout (used by Stdout()).
	stdoutR, stdoutW := io.Pipe()

	task := &backgroundTaskClient{
		stream:    stream,
		stdoutR:   stdoutR,
		stdoutW:   stdoutW,
		ctxStdout: ctxStdout,
		ctxStderr: ctxStderr,
		taskID:    started.TaskId,
		done:      make(chan struct{}),
	}

	// Start goroutine to read server→client messages.
	go task.readLoop()

	return task, nil
}

// readLoop reads messages from the server stream and fans them out:
//   - Always to mergedW (for Stdout()).
//   - If Tee() has been called, also to ctx writers and tee pipes.
func (t *backgroundTaskClient) readLoop() {
	defer close(t.done)
	defer t.stdoutW.Close()
	defer func() {
		t.teeMu.Lock()
		if t.teeStdoutW != nil {
			t.teeStdoutW.Close()
		}
		if t.teeStderrW != nil {
			t.teeStderrW.Close()
		}
		t.teeMu.Unlock()
	}()

	for {
		msg, err := t.stream.Receive()
		if err != nil {
			t.exitErr = fmt.Errorf("background exec stream: %w", err)
			return
		}

		switch ev := msg.Event.(type) {
		case *dotfilesdv1.BackgroundExecResponse_StdoutChunk:
			t.fanOut(ev.StdoutChunk, true)
		case *dotfilesdv1.BackgroundExecResponse_StderrChunk:
			t.fanOut(ev.StderrChunk, false)
		case *dotfilesdv1.BackgroundExecResponse_Exit:
			t.exitCode = int(ev.Exit.ExitCode)
			if ev.Exit.ErrorMessage != "" {
				t.exitErr = fmt.Errorf("%s", ev.Exit.ErrorMessage)
			}
			return
		}
	}
}

// fanOut writes a chunk to the merged pipe and, if tee is active, to the
// context writer and tee pipe.
func (t *backgroundTaskClient) fanOut(chunk []byte, isStdout bool) {
	if len(chunk) == 0 {
		return
	}

	// Always write to the merged pipe.
	if _, err := t.stdoutW.Write(chunk); err != nil {
		return // reader closed
	}

	// If tee is active, also write to context + tee pipe.
	t.teeMu.Lock()
	active := t.teeActive
	var ctxW io.Writer
	var teeW *io.PipeWriter
	if isStdout {
		ctxW = t.ctxStdout
		teeW = t.teeStdoutW
	} else {
		ctxW = t.ctxStderr
		teeW = t.teeStderrW
	}
	t.teeMu.Unlock()

	if active {
		if ctxW != nil {
			ctxW.Write(chunk)
		}
		if teeW != nil {
			teeW.Write(chunk)
		}
	}
}

func (t *backgroundTaskClient) Stdin() io.WriteCloser {
	stdinR, stdinW := io.Pipe()
	t.stdinW = stdinW

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
				return
			}
		}
	}()

	return stdinW
}

func (t *backgroundTaskClient) Stdout() io.Reader {
	return t.stdoutR
}

// Tee pipes stdout and stderr to both the plugin's output writers and
// independent readers. May only be called once; subsequent calls return
// the same readers.
func (t *backgroundTaskClient) Tee() (io.Reader, io.Reader) {
	t.teeMu.Lock()
	defer t.teeMu.Unlock()

	if t.teeActive {
		return t.teeStdoutR, t.teeStderrR
	}

	t.teeStdoutR, t.teeStdoutW = io.Pipe()
	t.teeStderrR, t.teeStderrW = io.Pipe()
	t.teeActive = true

	return t.teeStdoutR, t.teeStderrR
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
