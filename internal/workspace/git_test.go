package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- Task 5: Filesystem functions ---

func TestRepoCloneURL(t *testing.T) {
	got := RepoCloneURL("my-org", "my-repo", "")
	want := "https://github.com/my-org/my-repo.git"
	if got != want {
		t.Errorf("RepoCloneURL() = %q, want %q", got, want)
	}
}

func TestListWorktrees(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "main"), 0755)
	os.MkdirAll(filepath.Join(dir, "feature-x"), 0755)
	os.MkdirAll(filepath.Join(dir, ".hidden"), 0755)

	wts, err := ListWorktrees(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(wts) != 2 {
		t.Fatalf("got %d worktrees, want 2: %v", len(wts), wts)
	}
	found := map[string]bool{}
	for _, wt := range wts {
		found[wt] = true
	}
	if !found["main"] || !found["feature-x"] {
		t.Errorf("worktrees = %v, want [main feature-x]", wts)
	}
}

func TestListWorktrees_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	wts, err := ListWorktrees(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wts) != 0 {
		t.Errorf("got %d worktrees, want 0", len(wts))
	}
}

func TestListWorktrees_NotExists(t *testing.T) {
	_, err := ListWorktrees("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestDisableWorkspaceGit_And_EnableWorkspaceGit(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	disabledDir := filepath.Join(dir, ".git-disabled")
	os.MkdirAll(gitDir, 0755)

	DisableWorkspaceGit(dir)

	if _, err := os.Stat(gitDir); !os.IsNotExist(err) {
		t.Error(".git should not exist after disable")
	}
	if _, err := os.Stat(disabledDir); err != nil {
		t.Error(".git-disabled should exist after disable")
	}

	EnableWorkspaceGit(dir)

	if _, err := os.Stat(disabledDir); !os.IsNotExist(err) {
		t.Error(".git-disabled should not exist after enable")
	}
	if _, err := os.Stat(gitDir); err != nil {
		t.Error(".git should exist after enable")
	}
}

func TestDisableWorkspaceGit_NoGitDir(t *testing.T) {
	dir := t.TempDir()
	DisableWorkspaceGit(dir)
}

func TestEnableWorkspaceGit_NoDisabledDir(t *testing.T) {
	dir := t.TempDir()
	EnableWorkspaceGit(dir)
}

// --- Task 2 (ground): ListAllWorktrees ---

func TestListWorktrees_SkipsDotPrefixed(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".ground"), 0755)
	os.MkdirAll(filepath.Join(dir, ".bare"), 0755)
	os.MkdirAll(filepath.Join(dir, "feature-x"), 0755)

	names, err := ListWorktrees(dir)
	if err != nil {
		t.Fatalf("ListWorktrees() error: %v", err)
	}
	if len(names) != 1 || names[0] != "feature-x" {
		t.Errorf("ListWorktrees() = %v, want [feature-x]", names)
	}
}

func TestListAllWorktrees_IncludesGround(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".ground"), 0755)
	os.MkdirAll(filepath.Join(dir, ".bare"), 0755)
	os.MkdirAll(filepath.Join(dir, "feature-x"), 0755)

	names, err := ListAllWorktrees(dir)
	if err != nil {
		t.Fatalf("ListAllWorktrees() error: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("ListAllWorktrees() = %v, want [.ground feature-x]", names)
	}
	if names[0] != ".ground" {
		t.Errorf("first entry = %q, want .ground", names[0])
	}
	if names[1] != "feature-x" {
		t.Errorf("second entry = %q, want feature-x", names[1])
	}
}

func TestListAllWorktrees_NoGround(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".bare"), 0755)
	os.MkdirAll(filepath.Join(dir, "feature-x"), 0755)

	names, err := ListAllWorktrees(dir)
	if err != nil {
		t.Fatalf("ListAllWorktrees() error: %v", err)
	}
	if len(names) != 1 || names[0] != "feature-x" {
		t.Errorf("ListAllWorktrees() = %v, want [feature-x]", names)
	}
}

// --- Task 6: Git functions with real repos ---

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init", "--initial-branch=main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	f := filepath.Join(dir, "README.md")
	os.WriteFile(f, []byte("hello"), 0644)

	cmds = [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	return dir
}

func TestGitCurrentBranch(t *testing.T) {
	dir := initTestRepo(t)
	got := GitCurrentBranch(dir)
	if got != "main" {
		t.Errorf("GitCurrentBranch() = %q, want %q", got, "main")
	}
}

func TestGitCurrentBranch_InvalidDir(t *testing.T) {
	got := GitCurrentBranch("/nonexistent")
	if got != "" {
		t.Errorf("GitCurrentBranch() = %q, want empty", got)
	}
}

func TestGitDirtyCount(t *testing.T) {
	// Zero for clean dir (non-git dir returns 0)
	if got := GitDirtyCount(t.TempDir()); got != 0 {
		t.Errorf("GitDirtyCount(clean) = %d, want 0", got)
	}
}

func TestGitIsDirty_Clean(t *testing.T) {
	dir := initTestRepo(t)
	if GitIsDirty(dir) {
		t.Error("expected clean repo to not be dirty")
	}
}

func TestGitIsDirty_Dirty(t *testing.T) {
	dir := initTestRepo(t)
	os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("dirty"), 0644)
	if !GitIsDirty(dir) {
		t.Error("expected dirty repo to be dirty")
	}
}

func TestGitRecentCommits(t *testing.T) {
	dir := initTestRepo(t)
	// No baseBranch — shows all commits
	commits := GitRecentCommits(dir, 5, "")
	if len(commits) != 1 {
		t.Fatalf("got %d commits, want 1", len(commits))
	}
	if commits[0] == "" {
		t.Error("expected non-empty commit line")
	}
}

func TestGitRecentCommits_BranchOnly(t *testing.T) {
	dir := initTestRepo(t)
	// Create a feature branch with one extra commit
	runGitOutput(dir, "checkout", "-b", "feature")
	os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feat"), 0o644)
	runGitOutput(dir, "add", ".")
	runGitOutput(dir, "commit", "-m", "feature commit")

	// With baseBranch=main, only the feature commit should show
	commits := GitRecentCommits(dir, 10, "main")
	if len(commits) != 1 {
		t.Fatalf("got %d commits, want 1: %v", len(commits), commits)
	}
	if !strings.Contains(commits[0], "feature commit") {
		t.Errorf("expected feature commit, got %q", commits[0])
	}

	// On main itself, baseBranch filtering is skipped (same branch)
	runGitOutput(dir, "checkout", "main")
	commits = GitRecentCommits(dir, 10, "main")
	if len(commits) != 1 {
		t.Fatalf("got %d commits on main, want 1: %v", len(commits), commits)
	}
}

func TestGitRecentCommits_InvalidDir(t *testing.T) {
	commits := GitRecentCommits("/nonexistent", 5, "")
	if commits != nil {
		t.Errorf("expected nil, got %v", commits)
	}
}

func TestGitStashCount_NoStashes(t *testing.T) {
	dir := initTestRepo(t)
	if got := GitStashCount(dir); got != 0 {
		t.Errorf("GitStashCount() = %d, want 0", got)
	}
}

func TestGitLastCommitDate(t *testing.T) {
	dir := initTestRepo(t)
	d := GitLastCommitDate(dir)
	if d.IsZero() {
		t.Error("expected non-zero commit date")
	}
}

func TestGitLastCommitDate_InvalidDir(t *testing.T) {
	d := GitLastCommitDate("/nonexistent")
	if !d.IsZero() {
		t.Error("expected zero time for invalid dir")
	}
}

func TestGitDiffStat_Clean(t *testing.T) {
	dir := initTestRepo(t)
	if got := GitDiffStat(dir); got != "" {
		t.Errorf("GitDiffStat() = %q, want empty", got)
	}
}

func TestGitDiffStat_WithChanges(t *testing.T) {
	dir := initTestRepo(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed"), 0644)
	if got := GitDiffStat(dir); got == "" {
		t.Error("expected non-empty diff stat")
	}
}

func TestGitMergedBranches(t *testing.T) {
	dir := initTestRepo(t)

	cmd := exec.Command("git", "checkout", "-b", "feature")
	cmd.Dir = dir
	cmd.CombinedOutput()

	cmd = exec.Command("git", "checkout", "main")
	cmd.Dir = dir
	cmd.CombinedOutput()

	branches := GitMergedBranches(dir, "main")
	found := false
	for _, b := range branches {
		if b == "feature" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'feature' in merged branches, got %v", branches)
	}
}

func TestGitMergedBranches_InvalidDir(t *testing.T) {
	branches := GitMergedBranches("/nonexistent", "main")
	if branches != nil {
		t.Errorf("expected nil, got %v", branches)
	}
}

// --- Task 15: GitAheadBehind ---

func TestGitAheadBehind_NoUpstream(t *testing.T) {
	dir := initTestRepo(t)
	ahead, behind := GitAheadBehind(dir)
	if ahead != 0 || behind != 0 {
		t.Errorf("GitAheadBehind() = (%d, %d), want (0, 0)", ahead, behind)
	}
}

// --- Task 1: GitWorktreeAddBranch / GitWorktreeAddNewBranch ---

func TestGitWorktreeAddBranch(t *testing.T) {
	src := initTestRepo(t)
	bare := filepath.Join(t.TempDir(), ".bare")
	if err := GitCloneBare(src, bare); err != nil {
		t.Fatalf("GitCloneBare() error: %v", err)
	}

	wtPath := filepath.Join(t.TempDir(), "main")
	if err := GitWorktreeAddBranch(bare, wtPath, "main"); err != nil {
		t.Fatalf("GitWorktreeAddBranch() error: %v", err)
	}

	if got := GitCurrentBranch(wtPath); got != "main" {
		t.Errorf("branch = %q, want %q", got, "main")
	}
}

func TestGitWorktreeAddBranch_NoSuchBranch(t *testing.T) {
	src := initTestRepo(t)
	bare := filepath.Join(t.TempDir(), ".bare")
	if err := GitCloneBare(src, bare); err != nil {
		t.Fatalf("GitCloneBare() error: %v", err)
	}

	wtPath := filepath.Join(t.TempDir(), "nonexistent")
	if err := GitWorktreeAddBranch(bare, wtPath, "nonexistent"); err == nil {
		t.Error("expected error for nonexistent branch")
	}
}

func TestGitWorktreeAddNewBranch(t *testing.T) {
	src := initTestRepo(t)
	bare := filepath.Join(t.TempDir(), ".bare")
	if err := GitCloneBare(src, bare); err != nil {
		t.Fatalf("GitCloneBare() error: %v", err)
	}

	// Need a worktree for HEAD to resolve — add main first
	mainPath := filepath.Join(t.TempDir(), "main")
	GitWorktreeAddBranch(bare, mainPath, "main")

	wtPath := filepath.Join(t.TempDir(), "my-feature")
	if err := GitWorktreeAddNewBranch(bare, wtPath, "my-feature", "main"); err != nil {
		t.Fatalf("GitWorktreeAddNewBranch() error: %v", err)
	}

	if got := GitCurrentBranch(wtPath); got != "my-feature" {
		t.Errorf("branch = %q, want %q", got, "my-feature")
	}
}

// --- Task 2: GitCloneBare ---

func TestGitCloneBare(t *testing.T) {
	// Create a source repo to clone from
	src := initTestRepo(t)

	dst := filepath.Join(t.TempDir(), ".bare")

	if err := GitCloneBare(src, dst); err != nil {
		t.Fatalf("GitCloneBare() error: %v", err)
	}

	// Verify it's a bare repo (has HEAD file directly, not in .git/)
	if _, err := os.Stat(filepath.Join(dst, "HEAD")); err != nil {
		t.Error("expected HEAD file in bare repo")
	}

	// Verify fetch refspec was configured
	out, err := runGitOutput(dst, "config", "remote.origin.fetch")
	if err != nil {
		t.Fatalf("git config error: %v", err)
	}
	want := "+refs/heads/*:refs/remotes/origin/*"
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("fetch refspec = %q, want %q", got, want)
	}

	// Verify push.autoSetupRemote was configured
	out, err = runGitOutput(dst, "config", "push.autoSetupRemote")
	if err != nil {
		t.Fatalf("git config push.autoSetupRemote error: %v", err)
	}
	if got := strings.TrimSpace(out); got != "true" {
		t.Errorf("push.autoSetupRemote = %q, want %q", got, "true")
	}
}

func TestRepoCloneURL_SSH(t *testing.T) {
	got := RepoCloneURL("my-org", "my-repo", "ssh")
	want := "git@github.com:my-org/my-repo.git"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRepoCloneURL_HTTPS(t *testing.T) {
	got := RepoCloneURL("my-org", "my-repo", "https")
	want := "https://github.com/my-org/my-repo.git"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
