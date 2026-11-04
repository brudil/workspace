// Package termpane runs a program in a pseudo-terminal and keeps an emulated
// screen of it, so a TUI can draw that program inside one of its own panes.
//
// Data flow:
//
//	program ──pty──▶ emulator (screen state) ──Render──▶ host TUI
//	host keys ──SendKey──▶ emulator encodes ──input pipe──▶ pty ──▶ program
//
// The emulator also answers the program's terminal queries (cursor position,
// device attributes) through the same input pipe.
package termpane

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// Pane is one program running in a pseudo-terminal.
type Pane struct {
	// emuMu guards every emulator call. vt's SafeEmulator leaves several
	// readers (String, CursorPosition) unlocked, so we lock ourselves.
	emuMu sync.Mutex
	emu   *vt.Emulator
	pty   *os.File
	cmd   *exec.Cmd

	toPty chan []byte // encoded input waiting to be written to the program

	// mirror, when set, also receives the program's raw output (for handing
	// the real terminal to the program, see SetMirror).
	mirrorMu sync.Mutex
	mirror   io.Writer

	updates chan struct{} // signalled (coalesced) when the screen changes
	done    chan struct{} // closed when the program exits

	mu      sync.Mutex
	width   int
	height  int
	title   string
	exitErr error
	closing bool
}

// Start runs cmd in a new pseudo-terminal of the given size. cmd's Stdin,
// Stdout and Stderr must be unset; the pane owns them.
func Start(cmd *exec.Cmd, width, height int) (*Pane, error) {
	width, height = max(width, 1), max(height, 1)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
	if err != nil {
		return nil, err
	}

	p := &Pane{
		emu:     vt.NewEmulator(width, height),
		pty:     f,
		cmd:     cmd,
		toPty:   make(chan []byte, 256),
		updates: make(chan struct{}, 1),
		done:    make(chan struct{}),
		width:   width,
		height:  height,
	}
	p.emu.SetCallbacks(vt.Callbacks{
		Title: func(t string) {
			p.mu.Lock()
			p.title = t
			p.mu.Unlock()
		},
	})

	go p.pumpOutput()
	go p.pumpInput()
	go p.writeInput()
	go p.wait()
	return p, nil
}

// pumpOutput feeds program output into the emulator.
func (p *Pane) pumpOutput() {
	buf := make([]byte, 32*1024)
	for {
		n, err := p.pty.Read(buf)
		if n > 0 {
			p.emuMu.Lock()
			p.emu.Write(buf[:n])
			p.emuMu.Unlock()
			p.mirrorMu.Lock()
			if p.mirror != nil {
				p.mirror.Write(buf[:n])
			}
			p.mirrorMu.Unlock()
			p.notify()
		}
		if err != nil {
			return
		}
	}
}

// pumpInput drains encoded keys and query replies from the emulator's input
// pipe into a queue. The pipe is synchronous and the emulator writes to it
// while we hold emuMu, so it must never wait on the program: writeInput does
// the possibly-blocking pty writes.
func (p *Pane) pumpInput() {
	defer close(p.toPty)
	buf := make([]byte, 4096)
	for {
		n, err := p.emu.Read(buf)
		if n > 0 {
			p.toPty <- append([]byte(nil), buf[:n]...)
		}
		if err != nil {
			return
		}
	}
}

func (p *Pane) writeInput() {
	for b := range p.toPty {
		if _, err := p.pty.Write(b); err != nil {
			// Program gone; keep draining so pumpInput never blocks.
			continue
		}
	}
}

func (p *Pane) wait() {
	err := p.cmd.Wait()
	p.mu.Lock()
	p.exitErr = err
	p.mu.Unlock()
	// Give pumpOutput a moment to drain the program's last output.
	time.Sleep(20 * time.Millisecond)
	p.pty.Close()
	// End pumpInput by closing the pipe's write side. Emulator.Close would
	// race with the pending Read.
	if c, ok := p.emu.InputPipe().(io.Closer); ok {
		c.Close()
	}
	close(p.done)
	p.notify()
}

func (p *Pane) notify() {
	select {
	case p.updates <- struct{}{}:
	default:
	}
}

// Updates is signalled whenever the screen may have changed. Signals are
// coalesced: a reader that falls behind sees one, not a backlog.
func (p *Pane) Updates() <-chan struct{} { return p.updates }

// Done is closed when the program exits.
func (p *Pane) Done() <-chan struct{} { return p.done }

// Exited reports whether the program has exited.
func (p *Pane) Exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// ExitErr is the program's exit error, valid once Done is closed.
func (p *Pane) ExitErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitErr
}

// ExitSummary describes how the program ended: "ok", "exit N" or the signal.
func (p *Pane) ExitSummary() string {
	err := p.ExitErr()
	if err == nil {
		return "ok"
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return ws.Signal().String()
		}
		return "exit " + strconv.Itoa(ee.ExitCode())
	}
	return err.Error()
}

// Resize changes the pane's size, telling the program via SIGWINCH.
func (p *Pane) Resize(width, height int) {
	width, height = max(width, 1), max(height, 1)
	p.mu.Lock()
	if width == p.width && height == p.height {
		p.mu.Unlock()
		return
	}
	p.width, p.height = width, height
	p.mu.Unlock()
	p.emuMu.Lock()
	p.emu.Resize(width, height)
	p.emuMu.Unlock()
	if !p.Exited() {
		pty.Setsize(p.pty, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
	}
	p.notify()
}

// Size returns the pane's size.
func (p *Pane) Size() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.width, p.height
}

// SendKey sends a key press, encoded for the modes the program has set.
func (p *Pane) SendKey(k uv.KeyPressEvent) {
	if p.Exited() {
		return
	}
	// The emulator matches control keys by comparing the whole event, so
	// drop the extras an enhanced-keyboard host terminal may report.
	k.IsRepeat = false
	k.BaseCode = 0
	k.ShiftedCode = 0
	p.emuMu.Lock()
	defer p.emuMu.Unlock()
	p.emu.SendKey(k)
}

// SendText sends raw text as if typed.
func (p *Pane) SendText(s string) {
	if !p.Exited() {
		p.emuMu.Lock()
		defer p.emuMu.Unlock()
		p.emu.SendText(s)
	}
}

// Paste sends text, bracketed if the program asked for bracketed paste.
func (p *Pane) Paste(s string) {
	if !p.Exited() {
		p.emuMu.Lock()
		defer p.emuMu.Unlock()
		p.emu.Paste(s)
	}
}

// Render returns the screen as lines styled with ANSI escapes, exactly
// width×height cells.
func (p *Pane) Render() string {
	p.emuMu.Lock()
	defer p.emuMu.Unlock()
	return p.emu.Render()
}

// RenderScrolled is Render looking back offset lines into the scrollback:
// offset 0 is the live screen. It is clamped to the scrollback length.
func (p *Pane) RenderScrolled(offset int) string {
	p.emuMu.Lock()
	defer p.emuMu.Unlock()
	screen := p.emu.Render()
	sb := p.emu.Scrollback()
	if offset <= 0 || p.emu.IsAltScreen() || sb == nil || sb.Len() == 0 {
		return screen
	}
	offset = min(offset, sb.Len())
	h := p.emu.Height()
	lines := make([]string, 0, h)
	// The view starts offset lines above the top of the live screen.
	for i := sb.Len() - offset; i < sb.Len() && len(lines) < h; i++ {
		lines = append(lines, sb.Line(i).Render())
	}
	for _, l := range strings.Split(screen, "\n") {
		if len(lines) == h {
			break
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n")
}

// ScrollbackLen is how many lines of history are above the screen. It is 0
// while the program is on the alternate screen, which has no history.
func (p *Pane) ScrollbackLen() int {
	p.emuMu.Lock()
	defer p.emuMu.Unlock()
	if p.emu.IsAltScreen() {
		return 0
	}
	return p.emu.ScrollbackLen()
}

// AltScreen reports whether the program is using the alternate screen (as
// full-screen programs do).
func (p *Pane) AltScreen() bool {
	p.emuMu.Lock()
	defer p.emuMu.Unlock()
	return p.emu.IsAltScreen()
}

// SendMouse forwards a mouse event, encoded for the mouse mode the program
// asked for. It does nothing if the program didn't ask for mouse events.
func (p *Pane) SendMouse(m uv.MouseEvent) {
	if p.Exited() {
		return
	}
	p.emuMu.Lock()
	defer p.emuMu.Unlock()
	p.emu.SendMouse(m)
}

// SetMirror copies the program's raw output to w as well as the emulator, or
// stops copying when w is nil. With WriteRaw this lets a host hand its real
// terminal to the program and take it back with the emulated screen intact.
func (p *Pane) SetMirror(w io.Writer) {
	p.mirrorMu.Lock()
	defer p.mirrorMu.Unlock()
	p.mirror = w
}

// WriteRaw sends bytes to the program unmodified, as if typed on a real
// terminal.
func (p *Pane) WriteRaw(b []byte) {
	if p.Exited() || len(b) == 0 {
		return
	}
	defer func() { recover() }() // toPty closed as the program exits
	p.toPty <- append([]byte(nil), b...)
}

// Text returns the screen's plain text, trailing blanks trimmed. Useful for
// tests and logs.
func (p *Pane) Text() string {
	p.emuMu.Lock()
	defer p.emuMu.Unlock()
	return p.emu.String()
}

// Cursor returns the cursor position and whether the program shows it.
func (p *Pane) Cursor() (x, y int, visible bool) {
	p.emuMu.Lock()
	defer p.emuMu.Unlock()
	pos := p.emu.CursorPosition()
	return pos.X, pos.Y, !p.emu.CursorHidden()
}

// Title is the window title the program last set, if any.
func (p *Pane) Title() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.title
}

// Pid is the program's process id.
func (p *Pane) Pid() int { return p.cmd.Process.Pid }

// Close asks the program to exit with SIGHUP (as closing a terminal would),
// then kills it if it hasn't exited within grace.
func (p *Pane) Close(grace time.Duration) {
	p.mu.Lock()
	if p.closing {
		p.mu.Unlock()
		<-p.done
		return
	}
	p.closing = true
	p.mu.Unlock()

	if p.Exited() {
		return
	}
	// Signal the whole process group: the pty made the program a session
	// leader, and agents often run helpers of their own.
	syscall.Kill(-p.cmd.Process.Pid, syscall.SIGHUP)
	select {
	case <-p.done:
	case <-time.After(grace):
		syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		<-p.done
	}
}
