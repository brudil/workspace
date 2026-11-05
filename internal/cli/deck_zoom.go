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
// every byte straight through, until ctrl+] (or ctrl+g z). It is the escape hatch for
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

// ctrlKey is a ctrl+key as raw input can carry it: the legacy control
// byte, the kitty keyboard protocol's encoding and xterm's
// modifyOtherKeys, since the crew may have turned either on.
func ctrlKey(legacy byte, key rune) [][]byte {
	return [][]byte{
		{legacy},
		[]byte(fmt.Sprintf("\x1b[%d;5u", key)),
		[]byte(fmt.Sprintf("\x1b[27;5;%d~", key)),
	}
}

var (
	zoomPrefix = ctrlKey(0x07, 'g') // ctrl+g, then z leaves zoom
	zoomToggle = ctrlKey(0x1d, ']') // ctrl+] leaves zoom at once
)

// zoomFilter passes input through, watching for ctrl+] or ctrl+g z (leave
// zoom). A ctrl+g followed by anything else goes through as typed;
// ctrl+g ctrl+g sends one ctrl+g.
type zoomFilter struct {
	pending []byte // the start of a key sequence at the end of the last chunk
}

func (f *zoomFilter) feed(in []byte) (forward []byte, exit bool) {
	data := append(f.pending, in...)
	f.pending = nil
	var out []byte
	for len(data) > 0 {
		if matchKey(data, zoomToggle) > 0 {
			return out, true
		}
		prefix := matchKey(data, zoomPrefix)
		if prefix == 0 {
			// The start of a sequence at the end waits for the next chunk.
			// A lone ESC doesn't: it's the escape key, and agents act on it.
			if len(data) > 1 && (startsKey(data, zoomPrefix) || startsKey(data, zoomToggle)) {
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
		case matchKey(rest, zoomPrefix) > 0:
			out = append(out, 0x07)
			data = rest[matchKey(rest, zoomPrefix):]
		default:
			out = append(out, data[:prefix]...)
			data = rest
		}
	}
	return out, false
}

// matchKey is the length of the encoding of key that b starts with, or 0.
func matchKey(b []byte, key [][]byte) int {
	for _, enc := range key {
		if bytes.HasPrefix(b, enc) {
			return len(enc)
		}
	}
	return 0
}

// startsKey reports whether b is the start of one of key's encodings.
func startsKey(b []byte, key [][]byte) bool {
	for _, enc := range key {
		if len(b) < len(enc) && bytes.HasPrefix(enc, b) {
			return true
		}
	}
	return false
}
