package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// WorkspaceOpts configures a test workspace.
type WorkspaceOpts struct {
	Org           string
	DefaultBranch string
	Repos         []RepoOpts
}

// RepoOpts configures a single repo in the test workspace.
type RepoOpts struct {
	Name        string
	Branches    []string // extra remote branches (each gets a commit)
	Aliases     []string
	DisplayName string
	AfterCreate string
}

// Workspace holds paths to the created test workspace.
type Workspace struct {
	Root    string            // workspace root containing ws.toml
	Sources map[string]string // repo name -> source repo path (the "remote")
}

// SetupWorkspace creates a complete workspace with real git repos in t.TempDir().
func SetupWorkspace(t *testing.T, opts WorkspaceOpts) *Workspace {
	t.Helper()

	root := t.TempDir()
	sources := make(map[string]string)

	// Write ws.toml
	writeWSToml(t, root, opts)

	// Create repos dir
	reposDir := filepath.Join(root, "repos")
	os.MkdirAll(reposDir, 0755)

	for _, repo := range opts.Repos {
		// Create source repo (acts as the "remote")
		srcDir := t.TempDir()
		initRepo(t, srcDir, opts.DefaultBranch)
		sources[repo.Name] = srcDir

		// Create extra branches in source
		for _, branch := range repo.Branches {
			GitCmd(t, srcDir, "checkout", "-b", branch)
			os.WriteFile(filepath.Join(srcDir, branch+".txt"), []byte(branch), 0644)
			GitCmd(t, srcDir, "add", ".")
			GitCmd(t, srcDir, "commit", "-m", "add "+branch)
			GitCmd(t, srcDir, "checkout", opts.DefaultBranch)
		}

		// Create repo directory structure
		repoDir := filepath.Join(reposDir, repo.Name)
		os.MkdirAll(repoDir, 0755)

		// Bare clone from source
		bareDir := filepath.Join(repoDir, ".bare")
		GitCmd(t, "", "clone", "--bare", srcDir, bareDir)
		// Configure fetch refspec (same as ws does)
		GitCmd(t, bareDir, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
		// Fetch to populate remote refs
		GitCmd(t, bareDir, "fetch", "--all")

		// Add .ground worktree
		groundDir := filepath.Join(repoDir, ".ground")
		GitCmd(t, bareDir, "worktree", "add", groundDir, opts.DefaultBranch)
	}

	return &Workspace{Root: root, Sources: sources}
}

func writeWSToml(t *testing.T, root string, opts WorkspaceOpts) {
	t.Helper()

	var b strings.Builder
	fmt.Fprintf(&b, "[workspace]\norg = %q\ndefault_branch = %q\n\n", opts.Org, opts.DefaultBranch)

	for _, repo := range opts.Repos {
		fmt.Fprintf(&b, "[repos.%s]\n", repo.Name)
		if repo.DisplayName != "" {
			fmt.Fprintf(&b, "display_name = %q\n", repo.DisplayName)
		}
		if len(repo.Aliases) > 0 {
			fmt.Fprintf(&b, "aliases = [%s]\n", quotedList(repo.Aliases))
		}
		if repo.AfterCreate != "" {
			fmt.Fprintf(&b, "after_create = %q\n", repo.AfterCreate)
		}
		b.WriteString("\n")
	}

	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(b.String()), 0644)
}

func quotedList(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = fmt.Sprintf("%q", item)
	}
	return strings.Join(quoted, ", ")
}

func initRepo(t *testing.T, dir, defaultBranch string) {
	t.Helper()
	GitCmd(t, dir, "init", "--initial-branch="+defaultBranch)
	GitCmd(t, dir, "config", "user.email", "test@test.com")
	GitCmd(t, dir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0644)
	GitCmd(t, dir, "add", ".")
	GitCmd(t, dir, "commit", "-m", "initial")
}

// GitCmd runs a git command. Exported so integration tests can use it.
func GitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}
