package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func GitClone(url, target string) error {
	return runGit("", "clone", url, target)
}

func GitCloneBare(url, bareDir string) error {
	if err := runGit("", "clone", "--bare", url, bareDir); err != nil {
		return err
	}
	if err := runGit(bareDir, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return err
	}
	return runGit(bareDir, "config", "push.autoSetupRemote", "true")
}

func GitFetch(repoDir string) error {
	return runGit(repoDir, "fetch", "--all", "--prune")
}

func GitPull(dir string) error {
	return runGit(dir, "pull")
}

// GitWorktreeAddBranch checks out an existing branch into a new worktree.
func GitWorktreeAddBranch(gitDir, path, branch string) error {
	return runGit(gitDir, "worktree", "add", path, branch)
}

// GitWorktreeAddNewBranch creates a new branch from base and checks it out into a new worktree.
func GitWorktreeAddNewBranch(gitDir, path, branch, base string) error {
	return runGit(gitDir, "worktree", "add", path, "-b", branch, base)
}

func GitWorktreeRemove(gitDir, path string) error {
	return runGit(gitDir, "worktree", "remove", path)
}

func GitIsDirty(dir string) bool {
	out, err := runGitOutput(dir, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != ""
}

// GitDirtyCount returns the number of uncommitted files (modified, staged, untracked).
func GitDirtyCount(dir string) int {
	out, err := runGitOutput(dir, "status", "--porcelain")
	if err != nil {
		return 0
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

func GitCurrentBranch(dir string) string {
	out, err := runGitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func GitAheadBehind(dir string) (int, int) {
	out, err := runGitOutput(dir, "rev-list", "--left-right", "--count", "HEAD...@{u}")
	if err != nil {
		return 0, 0
	}
	var ahead, behind int
	fmt.Sscanf(strings.TrimSpace(out), "%d\t%d", &ahead, &behind)
	return ahead, behind
}

func RepoCloneURL(org, name, gitProtocol string) string {
	if gitProtocol == "ssh" {
		return fmt.Sprintf("git@github.com:%s/%s.git", org, name)
	}
	return fmt.Sprintf("https://github.com/%s/%s.git", org, name)
}

func ListWorktrees(repoDir string) ([]string, error) {
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// ListAllWorktrees returns all worktree directories including .ground.
// .ground is always first when present.
func ListAllWorktrees(repoDir string) ([]string, error) {
	capsules, err := ListWorktrees(repoDir)
	if err != nil {
		return nil, err
	}

	groundDir := filepath.Join(repoDir, GroundDir)
	if info, err := os.Stat(groundDir); err == nil && info.IsDir() {
		return append([]string{GroundDir}, capsules...), nil
	}

	return capsules, nil
}

func DisableWorkspaceGit(root string) {
	gitDir := filepath.Join(root, ".git")
	disabled := filepath.Join(root, ".git-disabled")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		os.Rename(gitDir, disabled)
	}
}

func EnableWorkspaceGit(root string) {
	gitDir := filepath.Join(root, ".git")
	disabled := filepath.Join(root, ".git-disabled")
	if info, err := os.Stat(disabled); err == nil && info.IsDir() {
		os.Rename(disabled, gitDir)
	}
}

// GitRecentCommits returns the last n commits as one-line summaries.
// If baseBranch is non-empty and differs from the current branch, only
// commits not in baseBranch are shown (i.e. the capsule's own work).
func GitRecentCommits(dir string, n int, baseBranch string) []string {
	rangeSpec := ""
	if baseBranch != "" {
		cur := GitCurrentBranch(dir)
		if cur != "" && cur != baseBranch {
			rangeSpec = baseBranch + "..HEAD"
		}
	}

	args := []string{"log", "--oneline", "-n", fmt.Sprintf("%d", n)}
	if rangeSpec != "" {
		args = append(args, rangeSpec)
	}

	out, err := runGitOutput(dir, args...)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// GitDiffStat returns the --stat output for uncommitted changes.
func GitDiffStat(dir string) string {
	out, err := runGitOutput(dir, "diff", "--stat")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// GitStashCount returns the number of stash entries.
func GitStashCount(dir string) int {
	out, err := runGitOutput(dir, "stash", "list")
	if err != nil || strings.TrimSpace(out) == "" {
		return 0
	}
	return len(strings.Split(strings.TrimSpace(out), "\n"))
}

// GitLastCommitDate returns the author date of the most recent commit.
func GitLastCommitDate(dir string) time.Time {
	out, err := runGitOutput(dir, "log", "-1", "--format=%aI")
	if err != nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, strings.TrimSpace(out))
	return t
}

// GitMergedBranches returns branch names that are fully merged into base.
func GitMergedBranches(dir, base string) []string {
	out, err := runGitOutput(dir, "branch", "--merged", base)
	if err != nil {
		return nil
	}
	var branches []string
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		// git branch prefixes: "* " for current, "+ " for worktree-linked
		name := strings.TrimSpace(line)
		name = strings.TrimPrefix(name, "* ")
		name = strings.TrimPrefix(name, "+ ")
		if name != "" {
			branches = append(branches, name)
		}
	}
	return branches
}

func runGit(dir string, args ...string) error {
	_, err := runGitOutput(dir, args...)
	return err
}

func runGitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
