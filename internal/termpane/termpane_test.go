package termpane

import (
	"os/exec"
	"strings"
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
