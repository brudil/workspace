package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorktreeHeadMtime_RegularGitDir(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	os.MkdirAll(gitDir, 0755)
	headPath := filepath.Join(gitDir, "HEAD")
	os.WriteFile(headPath, []byte("ref: refs/heads/main\n"), 0644)

	got := worktreeHeadMtime(dir)
	if got.IsZero() {
		t.Error("expected non-zero mtime")
	}
	if time.Since(got) > 2*time.Second {
		t.Errorf("mtime too old: %v", got)
	}
}

func TestWorktreeHeadMtime_LinkedWorktree(t *testing.T) {
	dir := t.TempDir()
	gitdir := filepath.Join(dir, "fake-gitdir")
	os.MkdirAll(gitdir, 0755)
	os.WriteFile(filepath.Join(gitdir, "HEAD"), []byte("ref: refs/heads/feature\n"), 0644)

	os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+gitdir+"\n"), 0644)

	got := worktreeHeadMtime(dir)
	if got.IsZero() {
		t.Error("expected non-zero mtime for linked worktree")
	}
}

func TestWorktreeHeadMtime_NoGit(t *testing.T) {
	dir := t.TempDir()
	got := worktreeHeadMtime(dir)
	if !got.IsZero() {
		t.Error("expected zero time for dir with no .git")
	}
}

func TestStatusOutline(t *testing.T) {
	root := t.TempDir()
	reposDir := filepath.Join(root, "repos")

	repoA := filepath.Join(reposDir, "repo-a")
	groundWT := filepath.Join(repoA, ".ground")
	featureWT := filepath.Join(repoA, "feature-x")
	os.MkdirAll(filepath.Join(groundWT, ".git"), 0755)
	os.WriteFile(filepath.Join(groundWT, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0644)
	os.MkdirAll(filepath.Join(featureWT, ".git"), 0755)
	os.WriteFile(filepath.Join(featureWT, ".git", "HEAD"), []byte("ref: refs/heads/feature-x\n"), 0644)
	ws := &Workspace{
		Root:          root,
		DefaultBranch: "main",
		RepoNames:     []string{"repo-a"},
		Boarded:       map[string][]string{"repo-a": {".ground"}},
	}

	outlines := ws.StatusOutline(false)
	if len(outlines) != 1 {
		t.Fatalf("got %d outlines, want 1", len(outlines))
	}

	o := outlines[0]
	if o.Name != "repo-a" {
		t.Errorf("name = %q, want %q", o.Name, "repo-a")
	}
	if len(o.Boarded) != 1 || o.Boarded[0] != ".ground" {
		t.Errorf("boarded = %v, want [.ground]", o.Boarded)
	}
	if len(o.Worktrees) != 2 {
		t.Errorf("got %d worktrees, want 2", len(o.Worktrees))
	}
	if o.Worktrees[0] != ".ground" {
		t.Errorf("first worktree = %q, want .ground", o.Worktrees[0])
	}
}

func TestStatusOutline_MissingRepoDir(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "repos"), 0755)

	ws := &Workspace{
		Root:          root,
		DefaultBranch: "main",
		RepoNames:     []string{"nonexistent"},
	}

	outlines := ws.StatusOutline(false)
	if len(outlines) != 1 {
		t.Fatalf("got %d outlines, want 1", len(outlines))
	}
	if outlines[0].Err == nil {
		t.Error("expected error for missing repo dir")
	}
}
