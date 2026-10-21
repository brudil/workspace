package cli_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/cli"
	"github.com/brudil/workspace/internal/testutil"
)

func requireRg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not installed")
	}
}

// rgWorkspace builds two repos with ground worktrees and one capsule in
// frontend, each containing a file with the search term.
func rgWorkspace(t *testing.T) *testutil.Workspace {
	t.Helper()
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos: []testutil.RepoOpts{
			{Name: "frontend", Aliases: []string{"fe"}},
			{Name: "backend", Aliases: []string{"api"}},
		},
	})

	write := func(rel, content string) {
		path := filepath.Join(w.Root, "repos", rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("frontend/.ground/src/app.ts", "needle in frontend ground\n")
	write("frontend/.ground/lib/util.ts", "needle in frontend lib\n")
	write("backend/.ground/src/main.go", "needle in backend ground\n")

	// Capsule in frontend, created the same way ws does.
	bare := filepath.Join(w.Root, "repos", "frontend", ".bare")
	capsule := filepath.Join(w.Root, "repos", "frontend", "login-fix")
	testutil.GitCmd(t, bare, "worktree", "add", "-b", "login-fix", capsule, "main")
	write("frontend/login-fix/src/app.ts", "needle in frontend capsule\n")

	// Silo and bare must never be searched.
	write("frontend/.silo/src/app.ts", "needle in silo\n")
	write("frontend/.bare/hooks/note.txt", "needle in bare\n")

	return w
}

func TestRg_RootsDefaultIsGround(t *testing.T) {
	w := rgWorkspace(t)
	t.Setenv(cli.ShellPWD, w.Root)

	res := testutil.RunCommand(t, w.Root, nil, "rg", "--roots")
	if res.Err != nil {
		t.Fatalf("rg --roots: %v\nstderr: %s", res.Err, res.Stderr)
	}
	want := filepath.Join(w.Root, "repos", "backend", ".ground") + "\n" +
		filepath.Join(w.Root, "repos", "frontend", ".ground") + "\n"
	if res.Stdout != want {
		t.Errorf("stdout = %q, want %q", res.Stdout, want)
	}
}

func TestRg_RootsWhereIStand(t *testing.T) {
	w := rgWorkspace(t)
	t.Setenv(cli.ShellPWD, filepath.Join(w.Root, "repos", "frontend", "login-fix", "src"))

	res := testutil.RunCommand(t, w.Root, nil, "rg", "--roots")
	if res.Err != nil {
		t.Fatalf("rg --roots: %v\nstderr: %s", res.Err, res.Stderr)
	}
	want := filepath.Join(w.Root, "repos", "backend", ".ground") + "\n" +
		filepath.Join(w.Root, "repos", "frontend", "login-fix") + "\n"
	if res.Stdout != want {
		t.Errorf("stdout = %q, want %q", res.Stdout, want)
	}
}

func TestRg_RootsRepoSelectors(t *testing.T) {
	w := rgWorkspace(t)
	t.Setenv(cli.ShellPWD, w.Root)

	res := testutil.RunCommand(t, w.Root, nil, "rg", "--repo", "fe/login-fix,api", "--roots")
	if res.Err != nil {
		t.Fatalf("rg --roots: %v\nstderr: %s", res.Err, res.Stderr)
	}
	want := filepath.Join(w.Root, "repos", "backend", ".ground") + "\n" +
		filepath.Join(w.Root, "repos", "frontend", "login-fix") + "\n"
	if res.Stdout != want {
		t.Errorf("stdout = %q, want %q", res.Stdout, want)
	}
}

func TestRg_RootsAll(t *testing.T) {
	w := rgWorkspace(t)
	t.Setenv(cli.ShellPWD, w.Root)

	res := testutil.RunCommand(t, w.Root, nil, "rg", "--repo", "fe", "--all", "--roots")
	if res.Err != nil {
		t.Fatalf("rg --roots: %v\nstderr: %s", res.Err, res.Stderr)
	}
	want := filepath.Join(w.Root, "repos", "frontend", ".ground") + "\n" +
		filepath.Join(w.Root, "repos", "frontend", "login-fix") + "\n"
	if res.Stdout != want {
		t.Errorf("stdout = %q, want %q", res.Stdout, want)
	}
}

func TestRg_UnknownRepoIsErrorNotPicker(t *testing.T) {
	w := rgWorkspace(t)
	t.Setenv(cli.ShellPWD, w.Root)

	res := testutil.RunCommand(t, w.Root, nil, "rg", "--repo", "nope", "--roots")
	if res.Err == nil {
		t.Fatal("expected error for unknown repo")
	}
	if !strings.Contains(res.Stderr, `unknown repo "nope"`) {
		t.Errorf("stderr = %q, want unknown repo message", res.Stderr)
	}
}

func TestRg_RejectsPathTraversalInWorktree(t *testing.T) {
	w := rgWorkspace(t)
	t.Setenv(cli.ShellPWD, w.Root)

	for _, sel := range []string{"fe/..", "fe/.", "fe/login-fix/src", `fe/..\x`} {
		res := testutil.RunCommand(t, w.Root, nil, "rg", "--repo", sel, "--roots")
		if res.Err == nil {
			t.Errorf("--repo %s: expected error, got roots:\n%s", sel, res.Stdout)
		}
	}
}

func TestRg_SearchesOneTreePerRepo(t *testing.T) {
	requireRg(t)
	w := rgWorkspace(t)
	t.Setenv(cli.ShellPWD, filepath.Join(w.Root, "repos", "frontend", "login-fix"))

	res := testutil.RunCommand(t, w.Root, nil, "rg", "--sort", "path", "needle")
	if res.Err != nil {
		t.Fatalf("rg: %v\nstderr: %s", res.Err, res.Stderr)
	}
	lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), res.Stdout)
	}
	if !strings.HasPrefix(lines[0], filepath.Join(w.Root, "repos", "backend", ".ground", "src", "main.go")+":") {
		t.Errorf("line 0 = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], filepath.Join(w.Root, "repos", "frontend", "login-fix", "src", "app.ts")+":") {
		t.Errorf("line 1 = %q", lines[1])
	}
	if !strings.Contains(lines[1], "frontend capsule") {
		t.Errorf("expected capsule content, got %q", lines[1])
	}
	if strings.Contains(res.Stdout, ".silo") || strings.Contains(res.Stdout, ".bare") {
		t.Errorf("silo or bare leaked into results:\n%s", res.Stdout)
	}
}

func TestRg_SlashGlobIsAnchoredPerRoot(t *testing.T) {
	requireRg(t)
	w := rgWorkspace(t)
	t.Setenv(cli.ShellPWD, w.Root)

	res := testutil.RunCommand(t, w.Root, nil, "rg", "--repo", "fe", "-l", "-g", "src/*.ts", "needle")
	if res.Err != nil {
		t.Fatalf("rg: %v\nstderr: %s", res.Err, res.Stderr)
	}
	want := filepath.Join(w.Root, "repos", "frontend", ".ground", "src", "app.ts") + "\n"
	if res.Stdout != want {
		t.Errorf("stdout = %q, want %q", res.Stdout, want)
	}
}

func TestRg_NoMatchPassesExitCodeOne(t *testing.T) {
	requireRg(t)
	w := rgWorkspace(t)
	t.Setenv(cli.ShellPWD, w.Root)

	res := testutil.RunCommand(t, w.Root, nil, "rg", "definitely-not-here-zzz")
	var exit *cli.ExitCodeError
	if !errors.As(res.Err, &exit) || exit.Code != 1 {
		t.Fatalf("err = %v, want ExitCodeError{1}", res.Err)
	}
	if res.Stdout != "" || res.Stderr != "" {
		t.Errorf("expected silence on no match, got stdout=%q stderr=%q", res.Stdout, res.Stderr)
	}
}

func TestRg_MissingPatternIsOurError(t *testing.T) {
	w := rgWorkspace(t)
	t.Setenv(cli.ShellPWD, w.Root)

	res := testutil.RunCommand(t, w.Root, nil, "rg")
	if res.Err == nil || !strings.Contains(res.Stderr, "missing ripgrep pattern") {
		t.Errorf("err = %v, stderr = %q", res.Err, res.Stderr)
	}
}
