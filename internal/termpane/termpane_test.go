package termpane

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

// waitFor polls the pane's text until cond holds or the deadline passes.
func waitFor(t *testing.T, p *Pane, what string, cond func(string) bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if cond(p.Text()) {
			return
		}
		select {
		case <-p.Updates():
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatalf("timed out waiting for %s; screen:\n%s", what, p.Text())
		}
	}
}

func start(t *testing.T, w, h int, name string, args ...string) *Pane {
	t.Helper()
	p, err := Start(exec.Command(name, args...), w, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close(time.Second) })
	return p
}

func TestPane_RendersOutputAndExitsOK(t *testing.T) {
	p := start(t, 40, 5, "sh", "-c", `printf 'hello\r\nworld'`)
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("program didn't exit")
	}
	if got := p.Text(); !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Errorf("screen = %q", got)
	}
	if p.ExitSummary() != "ok" {
		t.Errorf("exit = %s", p.ExitSummary())
	}
	if lines := strings.Split(p.Render(), "\n"); len(lines) != 5 {
		t.Errorf("render has %d lines, want 5", len(lines))
	}
}

func TestPane_ExitCode(t *testing.T) {
	p := start(t, 20, 3, "sh", "-c", "exit 3")
	<-p.Done()
	if got := p.ExitSummary(); got != "exit 3" {
		t.Errorf("exit = %q", got)
	}
}

func TestPane_KeysReachProgram(t *testing.T) {
	p := start(t, 40, 5, "sh", "-c", `read line; printf 'got:%s' "$line"`)
	for _, r := range "abc" {
		p.SendKey(uv.KeyPressEvent{Code: r, Text: string(r)})
	}
	p.SendKey(uv.KeyPressEvent{Code: uv.KeyEnter})
	waitFor(t, p, "echoed input", func(s string) bool { return strings.Contains(s, "got:abc") })
}

func TestPane_CtrlCInterrupts(t *testing.T) {
	p := start(t, 40, 5, "sh", "-c", `trap 'printf interrupted; exit 0' INT; printf ready; while :; do sleep 0.05; done`)
	waitFor(t, p, "ready", func(s string) bool { return strings.Contains(s, "ready") })
	p.SendKey(uv.KeyPressEvent{Code: 'c', Mod: uv.ModCtrl, IsRepeat: true}) // IsRepeat must not break matching
	waitFor(t, p, "trap", func(s string) bool { return strings.Contains(s, "interrupted") })
}

func TestPane_ResizeSignalsProgram(t *testing.T) {
	p := start(t, 40, 5, "sh", "-c", `trap 'printf "size:%s" "$(stty size)"' WINCH; printf ready; while :; do sleep 0.05; done`)
	waitFor(t, p, "ready", func(s string) bool { return strings.Contains(s, "ready") })
	p.Resize(60, 8)
	waitFor(t, p, "new size", func(s string) bool { return strings.Contains(s, "size:8 60") })
	if w, h := p.Size(); w != 60 || h != 8 {
		t.Errorf("Size = %d×%d", w, h)
	}
	if lines := strings.Split(p.Render(), "\n"); len(lines) != 8 {
		t.Errorf("render has %d lines after resize, want 8", len(lines))
	}
}

func TestPane_AnswersCursorPositionQuery(t *testing.T) {
	// Ask the terminal where the cursor is and print the raw reply. A pane that
	// didn't answer would hang here until the timeout.
	p := start(t, 40, 5, "sh", "-c", `stty raw -echo; printf 'ab\033[6n'; reply=$(dd bs=1 count=6 2>/dev/null); stty sane; printf '\r\nreply:%s' "$(printf %s "$reply" | tr -d '\033')"`)
	waitFor(t, p, "cursor reply", func(s string) bool { return strings.Contains(s, "reply:[1;3R") })
}

func TestPane_CloseStopsStubbornProgram(t *testing.T) {
	p := start(t, 20, 3, "sh", "-c", `trap '' HUP; while :; do sleep 0.05; done`)
	time.Sleep(100 * time.Millisecond)
	began := time.Now()
	p.Close(300 * time.Millisecond)
	if !p.Exited() {
		t.Fatal("still running after Close")
	}
	if time.Since(began) > 3*time.Second {
		t.Errorf("Close took %s", time.Since(began))
	}
}

func TestPane_CloseSendsHangup(t *testing.T) {
	p := start(t, 20, 3, "sh", "-c", `while :; do sleep 0.05; done`)
	time.Sleep(100 * time.Millisecond)
	p.Close(5 * time.Second)
	if got := p.ExitSummary(); got != "hangup" {
		t.Errorf("exit = %q, want hangup", got)
	}
}

func TestPane_TitleFromOSC(t *testing.T) {
	p := start(t, 20, 3, "sh", "-c", `printf '\033]2;my agent\007'; sleep 1`)
	deadline := time.Now().Add(5 * time.Second)
	for p.Title() != "my agent" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if p.Title() != "my agent" {
		t.Errorf("title = %q", p.Title())
	}
}

func TestPane_ScrollbackRender(t *testing.T) {
	p := start(t, 20, 5, "sh", "-c", `i=1; while [ $i -le 30 ]; do printf 'line%02d\r\n' $i; i=$((i+1)); done; sleep 5`)
	waitFor(t, p, "all lines", func(s string) bool { return strings.Contains(s, "line30") })

	if n := p.ScrollbackLen(); n < 25 {
		t.Fatalf("scrollback = %d lines, want ≥ 25", n)
	}
	live := p.RenderScrolled(0)
	if !strings.Contains(live, "line30") || strings.Contains(live, "line20") {
		t.Errorf("live view = %q", live)
	}
	back := p.RenderScrolled(10)
	if lines := strings.Split(back, "\n"); len(lines) != 5 {
		t.Errorf("scrolled view has %d lines", len(lines))
	}
	if !strings.Contains(back, "line21") || strings.Contains(back, "line30") {
		t.Errorf("scrolled 10 = %q", back)
	}
	// Past the top clamps to the oldest lines.
	if top := p.RenderScrolled(1000); !strings.Contains(top, "line01") {
		t.Errorf("scrolled to top = %q", top)
	}
}

func TestPane_AltScreenHasNoScrollback(t *testing.T) {
	p := start(t, 20, 5, "sh", "-c", `i=1; while [ $i -le 20 ]; do printf 'x%d\r\n' $i; i=$((i+1)); done; printf '\033[?1049hALT'; sleep 5`)
	waitFor(t, p, "alt screen", func(s string) bool { return strings.Contains(s, "ALT") })
	if !p.AltScreen() {
		t.Error("AltScreen = false")
	}
	if p.ScrollbackLen() != 0 {
		t.Errorf("scrollback on alt screen = %d", p.ScrollbackLen())
	}
	if got := p.RenderScrolled(5); !strings.Contains(got, "ALT") {
		t.Errorf("scrolling the alt screen should show it unchanged: %q", got)
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestPane_MirrorAndRawInput(t *testing.T) {
	p := start(t, 30, 5, "sh", "-c", `stty raw -echo; printf ready; c=$(dd bs=1 count=3 2>/dev/null); stty sane; printf '\r\nraw:%s\033[1mB' "$c"; sleep 5`)
	waitFor(t, p, "ready", func(s string) bool { return strings.Contains(s, "ready") })
	var mirror syncBuf
	p.SetMirror(&mirror)
	p.WriteRaw([]byte("xyz"))
	waitFor(t, p, "raw input", func(s string) bool { return strings.Contains(s, "raw:xyz") })
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(mirror.String(), "\x1b[1mB") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := mirror.String(); !strings.Contains(got, "raw:xyz\x1b[1mB") {
		t.Errorf("mirror got %q, want raw bytes including escapes", got)
	}
	p.SetMirror(nil)
}
