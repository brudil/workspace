package crew

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnv_PutsThisBinaryFirstOnPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", "/usr/bin:/bin")
	env := Env(root, "m", "c1")

	shim := filepath.Join(root, ".ws", "bin")
	var path string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			if path != "" {
				t.Fatal("PATH set twice")
			}
			path = v
		}
	}
	if path != shim+":/usr/bin:/bin" {
		t.Errorf("PATH = %q", path)
	}
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	for _, name := range []string{"ws", "workspace"} {
		if got, _ := os.Readlink(filepath.Join(shim, name)); got != exe {
			t.Errorf("%s -> %q, want %q", name, got, exe)
		}
	}

	// A stale link (an earlier build) is replaced; repeating is harmless.
	link := filepath.Join(shim, "ws")
	os.Remove(link)
	os.Symlink("/old/workspace", link)
	t.Setenv("PATH", shim+":/usr/bin")
	env = Env(root, "m", "c2")
	if got, _ := os.Readlink(link); got != exe {
		t.Errorf("stale link not replaced: %q", got)
	}
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok && v != shim+":/usr/bin" {
			t.Errorf("shim dir duplicated on PATH: %q", v)
		}
	}
}

// A deck started from inside a Claude Code session mustn't hand that
// session's identity to crew, but Claude Code settings pass through.
func TestEnv_DropsAgentSessionIdentity(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "parent-session")
	t.Setenv("CLAUDE_CODE_MESSAGING_SOCKET", "/tmp/parent.sock")
	t.Setenv("CLAUDE_CONFIG_DIR", "/home/me/.claude-work")
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")
	env := strings.Join(Env(t.TempDir(), "m", "c1"), "\n") + "\n"
	for _, gone := range []string{"CLAUDECODE=", "CLAUDE_CODE_SESSION_ID=", "CLAUDE_CODE_MESSAGING_SOCKET="} {
		if strings.Contains(env, "\n"+gone) || strings.HasPrefix(env, gone) {
			t.Errorf("%s passed to crew", gone)
		}
	}
	for _, kept := range []string{"CLAUDE_CONFIG_DIR=/home/me/.claude-work\n", "CLAUDE_CODE_USE_BEDROCK=1\n"} {
		if !strings.Contains(env, kept) {
			t.Errorf("%q dropped", kept)
		}
	}
}
