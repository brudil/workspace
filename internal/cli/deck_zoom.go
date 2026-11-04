package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/muesli/cancelreader"
	"golang.org/x/term"
)

// Zoom hands the real terminal to one crew: no emulation, no deck chrome,
// every byte straight through, until ctrl+g z. It is the escape hatch for
// anything the emulated pane renders or encodes wrongly.

// deckZoomDoneMsg is sent when zoom hands the terminal back.
type deckZoomDoneMsg struct {
	run *deckCrewRun
	err error
}

func (m deckModel) zoom(r *deckCrewRun) (tea.Model, tea.Cmd) {
	m.prefix, m.scrollMode = false, false
	r.scroll = 0
	z := &zoomCmd{run: r}
	return m, tea.Exec(z, func(err error) tea.Msg { return deckZoomDoneMsg{run: r, err: err} })
}

func (m deckModel) handleZoomDone(msg deckZoomDoneMsg) (tea.Model, tea.Cmd) {
	m.resizePanes()
	if msg.err != nil {
		m.flash = "zoom: " + msg.err.Error()
	}
	if msg.run.term.Exited() {
		return m.handlePaneMsg(deckPaneMsg{run: msg.run})
	}
	return m, nil
}

// zoomCmd is a tea.ExecCommand: Bubble Tea releases the terminal, runs it,
// and takes the terminal back when it returns.
type zoomCmd struct {
	run    *deckCrewRun
	stdin  io.Reader
	stdout io.Writer
}

func (z *zoomCmd) SetStdin(r io.Reader)  { z.stdin = r }
func (z *zoomCmd) SetStdout(w io.Writer) { z.stdout = w }
func (z *zoomCmd) SetStderr(io.Writer)   {}

type fdFile interface{ Fd() uintptr }

// zoomResetModes undoes terminal modes the crew may have switched on, so
// Bubble Tea gets back the terminal it handed over: mouse reporting,
// bracketed paste, focus events, kitty keyboard, application cursor keys
// and keypad, hidden cursor, attributes, alternate screen.
const zoomResetModes = "\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l" +
	"\x1b[?2004l\x1b[?1004l\x1b[<99u\x1b[>4m\x1b[?1l\x1b>" +
	"\x1b[?25h\x1b[m\x1b[?1049l"

func (z *zoomCmd) Run() error {
	t := z.run.term
	if t.Exited() {
		return nil
	}
	out := z.stdout
	if out == nil {
		out = os.Stderr
	}

	// Raw input, so every byte (ctrl+c included) goes to the crew.
	if f, ok := z.stdin.(fdFile); ok && term.IsTerminal(int(f.Fd())) {
		old, err := term.MakeRaw(int(f.Fd()))
		if err != nil {
			return err
		}
		defer term.Restore(int(f.Fd()), old)
	}

	// Paint what the crew shows now, then follow its raw output. Resizing to
	// the full terminal makes it redraw everything properly itself.
	if f, ok := out.(fdFile); ok {
		if w, h, err := term.GetSize(int(f.Fd())); err == nil {
			t.Resize(w, h)
		}
	}
	var mu sync.Mutex // the mirror and our own writes share out
	write := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		io.WriteString(out, s)
	}
	var paint strings.Builder
	if t.AltScreen() {
		paint.WriteString("\x1b[?1049h")
	}
	paint.WriteString("\x1b[m\x1b[H\x1b[2J")
	paint.WriteString(strings.ReplaceAll(t.RenderScrolled(0), "\n", "\r\n"))
	if x, y, visible := t.Cursor(); visible {
		fmt.Fprintf(&paint, "\x1b[%d;%dH\x1b[?25h", y+1, x+1)
	} else {
		paint.WriteString("\x1b[?25l")
	}
	write(paint.String())
	t.SetMirror(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return out.Write(p)
	}))
	defer func() {
		t.SetMirror(nil)
		write(zoomResetModes)
	}()

	if z.stdin == nil {
		<-t.Done()
		return nil
	}
	cr, err := cancelreader.NewReader(z.stdin)
	if err != nil {
		return err
	}
	defer cr.Close()
	go func() {
		<-t.Done()
		cr.Cancel()
	}()

	var f zoomFilter
	buf := make([]byte, 4096)
	for {
		n, err := cr.Read(buf)
		if n > 0 {
			fwd, exit := f.feed(buf[:n])
			t.WriteRaw(fwd)
			if exit {
				cr.Cancel()
				return nil
			}
		}
		if err != nil {
			return nil // cancelled (crew exited) or input closed
		}
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// Ctrl+g as the legacy control byte, and as the kitty keyboard protocol
// encodes it (in case the crew turned that protocol on).
var (
	zoomPrefixLegacy = []byte{0x07}
	zoomPrefixKitty  = []byte("\x1b[103;5u")
)

// zoomFilter passes input through, watching for ctrl+g z (leave zoom). A
// ctrl+g followed by anything else goes through as typed; ctrl+g ctrl+g
// sends one ctrl+g.
type zoomFilter struct {
	pending []byte // a prefix seen at the end of the last chunk
}

func (f *zoomFilter) feed(in []byte) (forward []byte, exit bool) {
	data := append(f.pending, in...)
	f.pending = nil
	var out []byte
	for len(data) > 0 {
		prefix := matchPrefix(data)
		if prefix == 0 {
			// A partial kitty sequence at the end waits for the next chunk.
			if bytes.HasPrefix(zoomPrefixKitty, data) {
				f.pending = append([]byte(nil), data...)
				return out, false
			}
			out = append(out, data[0])
			data = data[1:]
			continue
		}
		rest := data[prefix:]
		if len(rest) == 0 {
			f.pending = append([]byte(nil), data[:prefix]...)
			return out, false
		}
		switch {
		case rest[0] == 'z' || rest[0] == 'Z':
			return out, true
		case matchPrefix(rest) > 0:
			out = append(out, 0x07)
			data = rest[matchPrefix(rest):]
		default:
			out = append(out, data[:prefix]...)
			data = rest
		}
	}
	return out, false
}

func matchPrefix(b []byte) int {
	switch {
	case bytes.HasPrefix(b, zoomPrefixLegacy):
		return len(zoomPrefixLegacy)
	case bytes.HasPrefix(b, zoomPrefixKitty):
		return len(zoomPrefixKitty)
	}
	return 0
}
