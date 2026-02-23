package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type CheckStatus int

const (
	CheckOK CheckStatus = iota
	CheckWarn
	CheckFail
)

type CheckResult struct {
	Name   string
	Status CheckStatus
	Detail string
}

type CheckCategory struct {
	Name   string
	Checks []CheckResult
}

// Doctor runs all workspace health checks and returns results grouped by category.
func (w *Workspace) Doctor() []CheckCategory {
	return []CheckCategory{
		w.checkRepos(),
		w.checkOrphanedWorktrees(),
		w.checkTools(),
	}
}

func (w *Workspace) checkRepos() CheckCategory {
	var checks []CheckResult
	for _, name := range w.RepoNames {
		bareDir := w.BareDir(name)
		result := CheckResult{Name: name, Status: CheckOK}
		if _, err := os.Stat(bareDir); err != nil {
			result.Status = CheckFail
			result.Detail = fmt.Sprintf("not cloned (expected %s)", bareDir)
		}
		checks = append(checks, result)
	}
	return CheckCategory{Name: "Repos", Checks: checks}
}

func (w *Workspace) checkOrphanedWorktrees() CheckCategory {
	var checks []CheckResult
	for _, name := range w.RepoNames {
		repoDir := w.RepoDir(name)
		worktrees, err := ListAllWorktrees(repoDir)
		if err != nil {
			checks = append(checks, CheckResult{
				Name:   name,
				Status: CheckWarn,
				Detail: fmt.Sprintf("could not list worktrees: %v", err),
			})
			continue
		}

		knownBranches := gitWorktreeListBranches(w.BareDir(name))
		if knownBranches == nil {
			continue
		}

		for _, wt := range worktrees {
			if !knownBranches[wt] {
				checks = append(checks, CheckResult{
					Name:   fmt.Sprintf("%s/%s", name, wt),
					Status: CheckWarn,
					Detail: "directory exists but not registered as a git worktree",
				})
			}
		}
	}

	if len(checks) == 0 {
		checks = append(checks, CheckResult{
			Name:   "No orphaned worktrees",
			Status: CheckOK,
		})
	}
	return CheckCategory{Name: "Worktrees", Checks: checks}
}

func (w *Workspace) checkTools() CheckCategory {
	result := CheckResult{Name: "gh", Status: CheckOK}
	if _, err := exec.LookPath("gh"); err != nil {
		result.Status = CheckFail
		result.Detail = "gh CLI not found on PATH"
	}
	return CheckCategory{Name: "Tools", Checks: []CheckResult{result}}
}

// gitWorktreeListBranches uses `git worktree list` to get registered worktree paths,
// then extracts the directory name (which corresponds to the worktree folder name).
func gitWorktreeListBranches(mainWT string) map[string]bool {
	out, err := runGitOutput(mainWT, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	result := make(map[string]bool)
	for line := range strings.SplitSeq(out, "\n") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			result[filepath.Base(path)] = true
		}
	}
	return result
}
