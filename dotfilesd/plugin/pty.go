package plugin

import (
	"os"
	"sync"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// PtyTtyConn returns a TTYConn backed by a real OS-level PTY pair.
// The plugin's TUI code (e.g. tcell/tview) reads from and writes to
// the PTY slave, while the PTY master is bridged to the CLI terminal
// through the daemon's TtySession.
//
// Usage with tview/tcell:
//
//	ttyConn, err := ctx.PtyTtyConn()
//	screen, err := tcell.NewTerminfoScreenFromTtyTerminfo(ttyConn, ti)
//	app.SetScreen(screen)
func (c *contextClient) PtyTtyConn() (TTYConn, error) {
	// Open a real PTY pair and set a reasonable default size.
	// Most modern terminals are at least 132x43; the PTY will be
	// resized dynamically via Resize() when SIGWINCH arrives from
	// the CLI terminal.
	master, slave, err := pty.Open()
	if err != nil {
		return nil, err
	}
	_ = pty.Setsize(master, &pty.Winsize{Rows: 43, Cols: 132})

	// Set the PTY slave to raw mode so tcell receives keypresses
	// immediately (no line-buffering, no echo, no signal processing).
	// A TUI application reads individual keystrokes through the PTY
	// slave; without this, the terminal line discipline would buffer
	// input until Enter is pressed.
	//
	// We re-enable OPOST+ONLCR so that tcell's \n is converted to
	// \r\n for proper terminal output. Without this, the display
	// would show a staircase effect (newlines without carriage returns).
	_, _ = term.MakeRaw(int(slave.Fd()))
	rawFixTermios(int(slave.Fd()))

	// Get the existing TtyConn for CLI terminal communication.
	tty, err := c.TtyConn()
	if err != nil {
		master.Close()
		slave.Close()
		return nil, err
	}

	// When a resize event arrives from the CLI terminal (SIGWINCH →
	// WindowSize → TtyPacket.WindowWidth/Height), update the PTY and
	// deliver SIGWINCH to ourselves so that any TUI framework (tcell,
	// tview, etc.) monitoring the signal picks it up naturally.
	if tc, ok := tty.(*ttyConn); ok {
		tc.OnResize(func(w, h int) {
			_ = pty.Setsize(master, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
			_ = syscall.Kill(syscall.Getpid(), syscall.SIGWINCH)
		})
	}

	pc := &ptyTtyConn{
		tty:    tty,
		master: master,
		slave:  slave,
	}

	// Bridge: CLI terminal → PTY master → PTY slave → tcell.
	// User keystrokes arriving on TtyConn are written to the PTY master,
	// which flows through to the PTY slave where tcell reads them.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := tty.Read(buf)
			if n > 0 {
				// Best-effort write; errors likely mean the PTY closed.
				_, _ = master.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// Bridge: tcell → PTY slave → PTY master → CLI terminal.
	// tcell writes rendered output to the PTY slave, which appears on
	// the PTY master. We read from the master and send to TtyConn.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				_, _ = tty.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	return pc, nil
}

// ptyTtyConn implements TTYConn backed by a real PTY.
// The plugin's TUI library reads/writes the PTY slave, while
// bridge goroutines connect the PTY master to the CLI terminal.
type ptyTtyConn struct {
	tty    TTYConn
	master *os.File
	slave  *os.File
	closed bool
	mu     sync.Mutex
}

func (p *ptyTtyConn) Read(buf []byte) (int, error) {
	return p.slave.Read(buf)
}

func (p *ptyTtyConn) Write(buf []byte) (int, error) {
	return p.slave.Write(buf)
}

func (p *ptyTtyConn) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	_ = p.slave.Close()
	_ = p.master.Close()
	return p.tty.Close()
}

// Getsize returns the current PTY window dimensions (width, height).
func (p *ptyTtyConn) Getsize() (int, int, error) {
	if p.master == nil {
		return 0, 0, nil
	}
	// pty.Getsize returns (rows, cols). Convert to (width, height).
	rows, cols, err := pty.Getsize(p.master)
	if err != nil {
		return 0, 0, err
	}
	return cols, rows, nil
}

// Resize notifies the PTY of terminal dimension changes.
// This propagates SIGWINCH to the PTY slave, which tcell monitors.
func (p *ptyTtyConn) Resize(width, height int) error {
	return pty.Setsize(p.master, &pty.Winsize{
		Rows: uint16(height),
		Cols: uint16(width),
	})
}

// rawFixTermios re-enables output processing after term.MakeRaw.
// term.MakeRaw clears OPOST, which disables \n→\r\n conversion.
// TUIs written with tview/tcell write \n for newlines and depend
// on the terminal driver to convert to \r\n. Without this fix,
// output in raw mode shows a staircase effect.
func rawFixTermios(fd int) {
	var t syscall.Termios
	if _, _, err := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd),
		syscall.TCGETS, uintptr(unsafe.Pointer(&t)), 0, 0, 0); err != 0 {
		return
	}
	// Re-enable output processing (OPOST) and NL→CR+NL mapping (ONLCR).
	t.Oflag |= syscall.OPOST | syscall.ONLCR
	_, _, _ = syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd),
		syscall.TCSETS, uintptr(unsafe.Pointer(&t)), 0, 0, 0)
}

// Compile-time check that ptyTtyConn implements TTYConn.
var _ TTYConn = (*ptyTtyConn)(nil)
