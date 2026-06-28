package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"dotfilesd/internal/pkg/diagnostics"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"

	"connectrpc.com/connect"
)

// backgroundTaskManager tracks running background tasks.
type backgroundTaskManager struct {
	mu    sync.Mutex
	tasks map[string]*backgroundTask
	diag  *diagnostics.Engine
}

func newBackgroundTaskManager() *backgroundTaskManager {
	return &backgroundTaskManager{tasks: make(map[string]*backgroundTask)}
}

// SetDiagEngine configures the diagnostics engine for background task events.
func (m *backgroundTaskManager) SetDiagEngine(eng *diagnostics.Engine) {
	m.diag = eng
}

// ListTasks returns a snapshot of all active background tasks.
func (m *backgroundTaskManager) ListTasks() []*backgroundTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	nodes := make([]*backgroundTask, 0, len(m.tasks))
	for _, t := range m.tasks {
		nodes = append(nodes, t)
	}
	return nodes
}

// start launches a command in the background and streams its output
// on the bidi stream. The stream is owned by this task until the
// command exits or the client cancels.
func (m *backgroundTaskManager) start(
	ctx context.Context,
	stream *connect.BidiStream[dotfilesdv1.BackgroundExecRequest, dotfilesdv1.BackgroundExecResponse],
	cmd *exec.Cmd,
) {
	task := &backgroundTask{
		id:     fmt.Sprintf("bg-%d", time.Now().UnixNano()),
		cmd:    cmd,
		stream: stream,
		done:   make(chan struct{}),
	}

	m.mu.Lock()
	m.tasks[task.id] = task
	m.mu.Unlock()

	if m.diag != nil {
		m.diag.PushEvent(diagnostics.Event{
			Type:      diagnostics.EventBgTaskStart,
			Resource:  "bg_task:" + task.id,
			Timestamp: time.Now(),
			Message:   cmd.String(),
		})
	}

	task.run(ctx)

	if m.diag != nil {
		m.diag.PushEvent(diagnostics.Event{
			Type:      diagnostics.EventBgTaskStop,
			Resource:  "bg_task:" + task.id,
			Timestamp: time.Now(),
			Message:   cmd.String(),
		})
	}

	m.mu.Lock()
	delete(m.tasks, task.id)
	m.mu.Unlock()
}

// backgroundTask wraps a single running command with its bidi stream.
type backgroundTask struct {
	id     string
	cmd    *exec.Cmd
	stream *connect.BidiStream[dotfilesdv1.BackgroundExecRequest, dotfilesdv1.BackgroundExecResponse]

	mu       sync.Mutex
	stdin    io.WriteCloser
	cancelFn context.CancelFunc // set after start
	done     chan struct{}
	exited   atomic.Bool
}

// run starts the command, sends the started event, then pumps stdout/stderr
// to the client stream while reading stdin/cancel from the client.
func (t *backgroundTask) run(ctx context.Context) {
	slog.Info("background task starting", "task_id", t.id, "command", t.cmd.String())

	stdout, err := t.cmd.StdoutPipe()
	if err != nil {
		t.sendExit(-1, fmt.Sprintf("stdout pipe: %v", err))
		return
	}
	stderrPipe, err := t.cmd.StderrPipe()
	if err != nil {
		t.sendExit(-1, fmt.Sprintf("stderr pipe: %v", err))
		return
	}
	stdin, err := t.cmd.StdinPipe()
	if err != nil {
		t.sendExit(-1, fmt.Sprintf("stdin pipe: %v", err))
		return
	}
	t.stdin = stdin

	if err := t.cmd.Start(); err != nil {
		t.sendExit(-1, fmt.Sprintf("start command: %v", err))
		return
	}

	// Send started event.
	if err := t.stream.Send(&dotfilesdv1.BackgroundExecResponse{
		Event: &dotfilesdv1.BackgroundExecResponse_Started{
			Started: &dotfilesdv1.StartedEvent{TaskId: t.id},
		},
	}); err != nil {
		slog.Warn("background task: send started failed", "task_id", t.id, "error", err)
		_ = t.cmd.Process.Kill()
		return
	}

	// Wait group for the two output goroutines.
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: read stdout, send chunks.
	go func() {
		defer wg.Done()
		t.pipeOutput(stdout, true)
	}()

	// Goroutine 2: read stderr, send chunks.
	go func() {
		defer wg.Done()
		t.pipeOutput(stderrPipe, false)
	}()

	// Goroutine 3: read client stream for stdin/cancel.
	go func() {
		t.readClientStream(ctx)
	}()

	// Wait for the command to finish.
	err = t.cmd.Wait()
	t.exited.Store(true)

	// Wait for output goroutines to flush.
	wg.Wait()

	exitCode := int32(0)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		} else {
			exitCode = -1
		}
	}

	t.sendExit(exitCode, "")
	close(t.done)

	slog.Info("background task finished", "task_id", t.id, "exit_code", exitCode)
}

// pipeOutput reads from the given reader and sends stdout_chunk or
// stderr_chunk messages.
func (t *backgroundTask) pipeOutput(r io.Reader, isStdout bool) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			var resp dotfilesdv1.BackgroundExecResponse
			if isStdout {
				resp.Event = &dotfilesdv1.BackgroundExecResponse_StdoutChunk{StdoutChunk: chunk}
			} else {
				resp.Event = &dotfilesdv1.BackgroundExecResponse_StderrChunk{StderrChunk: chunk}
			}
			if sendErr := t.stream.Send(&resp); sendErr != nil {
				// Client disconnected; kill the command.
				_ = t.cmd.Process.Kill()
				return
			}
		}
		if err != nil {
			return // EOF or pipe error
		}
	}
}

// readClientStream reads stdin_chunk and cancel messages from the client.
func (t *backgroundTask) readClientStream(ctx context.Context) {
	for {
		msg, err := t.stream.Receive()
		if err != nil {
			// Stream closed — kill the command if still running.
			if !t.exited.Load() {
				_ = t.cmd.Process.Kill()
			}
			return
		}

		switch action := msg.Action.(type) {
		case *dotfilesdv1.BackgroundExecRequest_StdinChunk:
			if t.stdin != nil {
				_, _ = t.stdin.Write(action.StdinChunk)
			}
		case *dotfilesdv1.BackgroundExecRequest_Cancel:
			slog.Info("background task cancelled", "task_id", t.id)
			if !t.exited.Load() {
				_ = t.cmd.Process.Kill()
			}
			return
		}
	}
}

// sendExit sends the final exit event. Safe to call multiple times
// (only the first one is actually sent).
func (t *backgroundTask) sendExit(exitCode int32, errMsg string) {
	if t.exited.Swap(true) {
		return // already sent
	}
	_ = t.stream.Send(&dotfilesdv1.BackgroundExecResponse{
		Event: &dotfilesdv1.BackgroundExecResponse_Exit{
			Exit: &dotfilesdv1.ExitEvent{
				ExitCode:     exitCode,
				ErrorMessage: errMsg,
			},
		},
	})
}
