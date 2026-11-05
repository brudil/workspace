package testgit

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestNoSigning_TurnsSigningOffAndKeepsTheRest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	os.Unsetenv("GIT_CONFIG_GLOBAL")
	os.WriteFile(home+"/.gitconfig", []byte("[user]\n\temail = me@example.com\n[commit]\n\tgpgsign = true\n"), 0o644)

	cleanup := NoSigning()
	defer cleanup()
	// Looked up as git operations do (includes followed), outside any repo.
	outside := t.TempDir()
	get := func(key string) string {
		cmd := exec.Command("git", "config", key)
		cmd.Dir = outside
		out, _ := cmd.Output()
		return strings.TrimSpace(string(out))
	}
	if got := get("commit.gpgsign"); got != "false" {
		t.Errorf("commit.gpgsign = %q", got)
	}
	if got := get("user.email"); got != "me@example.com" {
		t.Errorf("user.email = %q: the real config should still apply", got)
	}
	cleanup()
	if _, set := os.LookupEnv("GIT_CONFIG_GLOBAL"); set {
		t.Error("cleanup left GIT_CONFIG_GLOBAL set")
	}
}
