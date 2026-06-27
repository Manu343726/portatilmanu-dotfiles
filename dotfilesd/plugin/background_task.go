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
type BackgroundTask interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	Cancel() error
	Wait() (exitCode int, err error)
	Tee() (stdout, stderr io.Reader)
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

	teeMu       sync.Mutex
	teeStdoutR  *io.PipeReader
	teeStdoutW  *io.PipeWriter
	teeStderrR  *io.PipeReader
	teeStderrW  *io.PipeWriter
	teeActive   bool

	taskID     string
	cancelSent bool
	exitCode   int
	exitErr    error
	done       chan struct{}
}

// startBackgroundTask is the shared implementation.
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
		taskID:    started.TaskId,
		done:      make(chan struct{}),
	}

	// Start goroutine to read server→client messages.
	go task.readLoop(ctxStdout, ctxStderr)

	return task, nil
}

// readLoop reads messages from the server stream and fans them out.
func (t *backgroundTaskClient) readLoop(ctxStdout, ctxStderr io.Writer) {
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
			t.fanOut(ev.StdoutChunk, true, ctxStdout, ctxStderr)
		case *dotfilesdv1.BackgroundExecResponse_StderrChunk:
			t.fanOut(ev.StderrChunk, false, ctxStdout, ctxStderr)
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
func (t *backgroundTaskClient) fanOut(chunk []byte, isStdout bool, ctxStdout, ctxStderr io.Writer) {
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
		ctxW = ctxStdout
		teeW = t.teeStdoutW
	} else {
		ctxW = ctxStderr
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
// independent readers. May only be called once.
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
