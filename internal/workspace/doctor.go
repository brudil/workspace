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
	Name    string
	Status  CheckStatus
	Detail  string
	Fix     func() error // nil if not auto-fixable
	FixHint string       // shown when Fix is nil but manual action exists
}

type CheckCategory struct {
	Name   string
	Checks []CheckResult
}

// Doctor runs all workspace health checks and returns results grouped by category.
func (w *Workspace) Doctor() []CheckCategory {
	cats := []CheckCategory{
		w.checkRepos(),
		w.checkOrphanedWorktrees(),
		w.checkTools(),
	}
	if siloChecks := w.checkSilos(); len(siloChecks.Checks) > 0 {
		cats = append(cats, siloChecks)
	}
	return cats
}

func (w *Workspace) checkRepos() CheckCategory {
	var checks []CheckResult
	for _, name := range w.RepoNames {
		bareDir := w.BareDir(name)
		result := CheckResult{Name: name, Status: CheckOK}
		if _, err := os.Stat(bareDir); err != nil {
			result.Status = CheckFail
			result.Detail = fmt.Sprintf("not cloned (expected %s)", bareDir)
			repoName := name // capture for closure
			result.Fix = func() error {
				return w.SetupRepo(repoName).Err
			}
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
				wtPath := filepath.Join(w.RepoDir(name), wt)
				checks = append(checks, CheckResult{
					Name:    fmt.Sprintf("%s/%s", name, wt),
					Status:  CheckWarn,
					Detail:  "directory exists but not registered as a git worktree",
					FixHint: fmt.Sprintf("rm -rf %s", wtPath),
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

// CheckGitHubAuthFunc is the function used to check GitHub auth status.
// Tests can replace this to avoid shelling out to gh.
var CheckGitHubAuthFunc = checkGitHubAuth

func (w *Workspace) checkTools() CheckCategory {
	ghResult := CheckResult{Name: "gh", Status: CheckOK}
	if _, err := exec.LookPath("gh"); err != nil {
		ghResult.Status = CheckFail
		ghResult.Detail = "gh CLI not found on PATH"
		ghResult.FixHint = "install with: brew install gh"
	}

	authResult := CheckGitHubAuthFunc()

	return CheckCategory{Name: "Tools", Checks: []CheckResult{ghResult, authResult}}
}

func checkGitHubAuth() CheckResult {
	result := CheckResult{Name: "gh auth", Status: CheckOK}
	out, err := exec.Command("gh", "auth", "status", "--hostname", "github.com").CombinedOutput()
	if err != nil {
		result.Status = CheckFail
		result.Detail = "not authenticated"
		result.FixHint = "run: gh auth login"
		return result
	}
	// Extract username from "Logged in to github.com account <user> ..."
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if _, after, ok := strings.Cut(line, "account "); ok {
			if user, _, ok := strings.Cut(after, " "); ok {
				result.Detail = user
			}
			break
		}
	}
	return result
}

func (w *Workspace) checkSilos() CheckCategory {
	var checks []CheckResult

	for repo, target := range w.Silo {
		// Check target capsule exists
		targetDir := filepath.Join(w.RepoDir(repo), target)
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			checks = append(checks, CheckResult{
				Name:    fmt.Sprintf("%s/%s", repo, target),
				Status:  CheckWarn,
				Detail:  "silo target does not exist",
				FixHint: fmt.Sprintf("ws silo point %s .ground", repo),
			})
		}

		// Check .silo/ directory exists
		siloDir := filepath.Join(w.RepoDir(repo), SiloDir)
		if _, err := os.Stat(siloDir); os.IsNotExist(err) {
			checks = append(checks, CheckResult{
				Name:    fmt.Sprintf("%s/.silo", repo),
				Status:  CheckWarn,
				Detail:  "silo configured but .silo/ directory missing",
				FixHint: fmt.Sprintf("ws silo point %s %s", repo, target),
			})
		}
	}

	// Check for orphaned .silo/ directories
	for _, name := range w.RepoNames {
		siloDir := filepath.Join(w.RepoDir(name), SiloDir)
		if _, err := os.Stat(siloDir); err == nil {
			if _, ok := w.Silo[name]; !ok {
				checks = append(checks, CheckResult{
					Name:    fmt.Sprintf("%s/.silo", name),
					Status:  CheckWarn,
					Detail:  ".silo/ directory exists but no silo configured",
					FixHint: fmt.Sprintf("ws silo stop %s", name),
				})
			}
		}
	}

	// Check for stale lock file
	lockPath := filepath.Join(w.Root, ".silo.lock")
	if _, err := os.Stat(lockPath); err == nil {
		if !IsLockHeld(lockPath) {
			lp := lockPath
			checks = append(checks, CheckResult{
				Name:   ".silo.lock",
				Status: CheckWarn,
				Detail: "stale lock file (no running watcher)",
				Fix: func() error {
					return os.Remove(lp)
				},
			})
		}
	}

	return CheckCategory{Name: "Silos", Checks: checks}
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
