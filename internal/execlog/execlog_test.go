package execlog

import (
	"os"
	"strings"
	"testing"
	"time"
)

func read(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(Path(root))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEntries(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(root+"/.ws", 0o755)
	Start(root, "orchestrator cache", []string{"cursor-agent", "-p", "--model", "claude-haiku-5-5-medium", "Read /ws/x.md first: it's yours."},
		"/ws", []string{"HOME=/h", "GH_TOKEN=secret", "WS_MISSION=cache", "WS_ROLE=orchestrator", "PATH=/ws/.ws/bin:/usr/bin", "TERM=xterm-256color"}, 4242)
	Note(root, "crew cache/c2", "typed in its prompt (11 chars):\nline one\nline two")
	End(root, "orchestrator cache", 4242, 6100*time.Millisecond, "signal: killed · 0 bytes of output")

	log := read(t, root)
	for _, want := range []string{
		"[orchestrator cache] start pid 4242 in /ws\n",
		"    $ cursor-agent -p --model claude-haiku-5-5-medium 'Read /ws/x.md first: it'\\''s yours.'\n",
		"    env WS_MISSION=cache WS_ROLE=orchestrator PATH=/ws/.ws/bin:… TERM=xterm-256color\n",
		"[crew cache/c2] typed in its prompt (11 chars):\n    line one\n    line two\n",
		"[orchestrator cache] end pid 4242 after 6.1s: signal: killed · 0 bytes of output\n",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "secret") || strings.Contains(log, "HOME=") {
		t.Errorf("logged environment beyond ws's own:\n%s", log)
	}
}

func TestLongArgumentsAreClipped(t *testing.T) {
	long := strings.Repeat("é", 1000) // 2000 bytes
	line := CommandLine([]string{"agent", long})
	if !strings.Contains(line, "…(+") || len(line) > argMax+40 {
		t.Errorf("line = %d bytes: %q…", len(line), line[:60])
	}
	if !strings.HasSuffix(line, "bytes)'") {
		t.Errorf("clipped argument not quoted whole: %q", line[len(line)-30:])
	}
}

func TestNoRootNoLog(t *testing.T) {
	Note("", "x", "nothing") // must not panic or write anywhere
}
