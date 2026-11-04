package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/brudil/workspace/internal/harness"
)

func TestRunOrchestrator_TimeoutKillsWhatItStarted(t *testing.T) {
	oldT, oldW := orchestrateTimeout, orchestrateWaitDelay
	orchestrateTimeout, orchestrateWaitDelay = 300*time.Millisecond, 2*time.Second
	t.Cleanup(func() { orchestrateTimeout, orchestrateWaitDelay = oldT, oldW })

	// The agent starts a long-lived child that holds its output open, as a
	// shell running `gh pr checks --watch` would.
	pidFile := filepath.Join(t.TempDir(), "child")
	l := harness.Launch{Dir: t.TempDir(), Args: []string{"/bin/sh", "-c", `sleep 30 & echo $! > ` + pidFile + `; echo started; wait`}}
	start := time.Now()
	out, err := runOrchestrator(l, "m")
	took := time.Since(start)
	t.Logf("returned after %s", took)
	if took > 5*time.Second {
		t.Fatalf("took %s: Run waited on the child", took)
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") || !strings.Contains(out, "started") {
		t.Errorf("err = %v, out = %q", err, out)
	}
	data, _ := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(2 * time.Second)
	for pid != 0 && running(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 || running(pid) {
		t.Errorf("child %d outlived the timeout", pid)
	}
}

// running reports whether pid is a live process: not gone, and not a zombie
// waiting for init to reap it (which kill -0 can't tell apart).
func running(pid int) bool {
	out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	stat := strings.TrimSpace(string(out))
	return stat != "" && !strings.HasPrefix(stat, "Z")
}

func TestClip_KeepsWholeCharactersAndLines(t *testing.T) {
	s := strings.Repeat("“quoted” ─ line…\n", 200)
	for limit := 10; limit < 300; limit += 7 {
		h, tl := clipHead(s, limit, "…"), clipTail(s, limit, "…")
		if !utf8.ValidString(h) || !utf8.ValidString(tl) {
			t.Fatalf("limit %d split a character: %q / %q", limit, h, tl)
		}
		if len(h) > limit || len(tl) > limit {
			t.Fatalf("limit %d exceeded: %d / %d", limit, len(h), len(tl))
		}
	}
	if got := clipTail("short", 100, "…"); got != "short" {
		t.Errorf("clipped what fit: %q", got)
	}
	tail := clipTail(strings.Repeat("a\n", 100)+"last line\n", 40, "…")
	if !strings.HasPrefix(tail, "…") || !strings.HasSuffix(tail, "last line\n") || strings.HasPrefix(tail, "…\n") {
		t.Errorf("tail = %q", tail)
	}
}
