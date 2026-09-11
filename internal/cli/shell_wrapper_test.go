package cli_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/testutil"
)

// The burn wrapper can only be exercised by a real shell running a real
// binary: the bug it fixes is the shell standing in a directory that burn
// deletes, which no in-process test can reproduce.

func requireZsh(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping shell integration test in short mode")
	}
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not available")
	}
	return zsh
}

// buildWorkspaceBinary builds the CLI so a shell can invoke it as `workspace`.
func buildWorkspaceBinary(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "workspace")
	build := exec.Command("go", "build", "-o", bin, "github.com/brudil/workspace/cmd/workspace")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building workspace binary: %v\n%s", err, out)
	}
	return bin
}

// writeWrapper writes the ws() wrapper to a file the shell can source. The
// completion half of shell-init needs compinit, which a bare shell lacks.
func writeWrapper(t *testing.T, bin string) string {
	t.Helper()
	out, err := exec.Command(bin, "shell-init", "zsh").Output()
	if err != nil {
		t.Fatalf("shell-init zsh: %v", err)
	}
	wrapper, _, found := strings.Cut(string(out), "#compdef")
	if !found {
		t.Fatalf("shell-init output missing completion section:\n%s", out)
	}
	path := filepath.Join(t.TempDir(), "wrapper.zsh")
	if err := os.WriteFile(path, []byte(wrapper), 0644); err != nil {
		t.Fatalf("writing wrapper: %v", err)
	}
	return path
}

type shellResult struct {
	status   string
	pwd      string // the shell's logical cwd once burn returned
	physical string // getcwd(), empty when the shell is sitting in a deleted dir
	during   string // the shell's cwd at the moment burn was invoked
	stderr   string
}

// installProbe puts a shim named `workspace` ahead of the real binary on PATH.
// It records the calling shell's cwd at the moment burn is invoked — the
// directory that must not be the one about to be deleted.
func installProbe(t *testing.T, bin string) (probeDir, logPath string) {
	t.Helper()
	probeDir = t.TempDir()
	logPath = filepath.Join(probeDir, "cwd.log")
	shim := fmt.Sprintf(`#!/bin/sh
if [ "$1" = burn ] || [ "$1" = rm ]; then
  /bin/pwd -P 2>/dev/null >> %s || echo "(deleted)" >> %s
fi
exec %s "$@"
`, quote(logPath), quote(logPath), quote(bin))
	if err := os.WriteFile(filepath.Join(probeDir, "workspace"), []byte(shim), 0755); err != nil {
		t.Fatalf("writing probe: %v", err)
	}
	return probeDir, logPath
}

// runBurnInZsh sources the wrapper, steps into startDir, and runs `ws burn`
// with the given args, reporting where the shell ended up.
func runBurnInZsh(t *testing.T, bin, startDir string, args ...string) shellResult {
	t.Helper()
	zsh := requireZsh(t)
	wrapper := writeWrapper(t, bin)

	script := fmt.Sprintf(`
source %s
cd %s
ws burn %s
print -r -- "status=$?"
print -r -- "pwd=$PWD"
print -r -- "physical=$(command pwd -P 2>/dev/null)"
`, quote(wrapper), quote(startDir), quoteAll(args))

	probeDir, logPath := installProbe(t, bin)

	cmd := exec.Command(zsh, "-f", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+strings.Join(
		[]string{probeDir, filepath.Dir(bin), os.Getenv("PATH")},
		string(os.PathListSeparator)))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("zsh script failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr.String())
	}

	res := shellResult{stderr: stderr.String()}
	if logged, err := os.ReadFile(logPath); err == nil {
		res.during = strings.TrimSpace(string(logged))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(stdout)), "\n") {
		key, value, _ := strings.Cut(line, "=")
		switch key {
		case "status":
			res.status = value
		case "pwd":
			res.pwd = value
		case "physical":
			res.physical = value
		}
	}
	return res
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func quoteAll(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = quote(a)
	}
	return strings.Join(quoted, " ")
}

func TestWrapperBurn_FromInsideCapsule_LandsAtRoot(t *testing.T) {
	bin := buildWorkspaceBinary(t)
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}},
	})
	testutil.RunCommand(t, w.Root, nil, "lift", "repo-a", "standing-here")

	wtDir := filepath.Join(w.Root, "repos", "repo-a", "standing-here")
	nested := filepath.Join(wtDir, "nested")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	res := runBurnInZsh(t, bin, nested, "repo-a", "standing-here")

	if res.status != "0" {
		t.Fatalf("burn exited %s\nstderr: %s", res.status, res.stderr)
	}
	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Errorf("capsule not removed, stat err = %v", err)
	}
	// The invariant: the shell must already be out of the capsule when burn
	// runs, not repositioned afterwards. Anything else leaves it standing in a
	// deleted directory for the length of the removal, which is what gets the
	// shell killed.
	if res.during != w.Root {
		t.Errorf("shell cwd during burn = %q, want workspace root %q", res.during, w.Root)
	}
	if res.pwd != w.Root {
		t.Errorf("shell pwd = %q, want workspace root %q", res.pwd, w.Root)
	}
	if res.physical != w.Root {
		t.Errorf("shell is not in a live directory: getcwd = %q, want %q", res.physical, w.Root)
	}
}

func TestWrapperBurn_OtherCapsule_StaysPut(t *testing.T) {
	bin := buildWorkspaceBinary(t)
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}},
	})
	testutil.RunCommand(t, w.Root, nil, "lift", "repo-a", "staying")
	testutil.RunCommand(t, w.Root, nil, "lift", "repo-a", "going")

	stay := filepath.Join(w.Root, "repos", "repo-a", "staying")

	res := runBurnInZsh(t, bin, stay, "repo-a", "going")

	if res.status != "0" {
		t.Fatalf("burn exited %s\nstderr: %s", res.status, res.stderr)
	}
	if res.during != w.Root {
		t.Errorf("shell cwd during burn = %q, want workspace root %q", res.during, w.Root)
	}
	if res.pwd != stay {
		t.Errorf("shell pwd = %q, want %q — burning another capsule should not move the shell", res.pwd, stay)
	}
	if res.physical != stay {
		t.Errorf("getcwd = %q, want %q", res.physical, stay)
	}
}

// The wrapper runs burn from the workspace root, so the repo for the one-arg
// form has to be inferred from where the shell actually was.
func TestWrapperBurn_InfersRepoFromShellDir(t *testing.T) {
	bin := buildWorkspaceBinary(t)
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}},
	})
	testutil.RunCommand(t, w.Root, nil, "lift", "repo-a", "inferred")

	wtDir := filepath.Join(w.Root, "repos", "repo-a", "inferred")

	res := runBurnInZsh(t, bin, wtDir, "inferred")

	if res.status != "0" {
		t.Fatalf("burn exited %s\nstderr: %s", res.status, res.stderr)
	}
	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Errorf("capsule not removed, stat err = %v", err)
	}
	if res.physical != w.Root {
		t.Errorf("getcwd = %q, want %q", res.physical, w.Root)
	}
}

func TestWrapperBurn_FailedBurn_StaysPut(t *testing.T) {
	bin := buildWorkspaceBinary(t)
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}},
	})
	testutil.RunCommand(t, w.Root, nil, "lift", "repo-a", "survivor")

	wtDir := filepath.Join(w.Root, "repos", "repo-a", "survivor")

	res := runBurnInZsh(t, bin, wtDir, "repo-a", "no-such-capsule")

	if res.status == "0" {
		t.Errorf("expected non-zero exit for unknown capsule, got %s", res.status)
	}
	if res.pwd != wtDir {
		t.Errorf("shell pwd = %q, want %q — a failed burn should leave the shell where it was", res.pwd, wtDir)
	}
}
