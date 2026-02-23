package workspace

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RepoStatus holds the full status of a single repo.
type RepoStatus struct {
	Name      string
	Boarded   []string // which worktrees are boarded
	Worktrees []WorktreeStatus
	Err       error // non-nil if we couldn't list worktrees
}

// WorktreeStatus holds git state for a single worktree.
type WorktreeStatus struct {
	Name   string
	Branch string
	Dirty  bool
	Ahead  int
	Behind int
}

// Status gathers full status for all repos synchronously.
func (w *Workspace) Status() []RepoStatus {
	statuses := make([]RepoStatus, len(w.RepoNames))
	for i, name := range w.RepoNames {
		statuses[i] = w.repoStatus(name)
	}
	return statuses
}

func (w *Workspace) repoStatus(name string) RepoStatus {
	repoDir := w.RepoDir(name)

	worktrees, err := ListAllWorktrees(repoDir)
	if err != nil {
		return RepoStatus{Name: name, Err: err}
	}

	wts := make([]WorktreeStatus, len(worktrees))
	for i, wt := range worktrees {
		wts[i] = QueryWorktreeStatus(filepath.Join(repoDir, wt))
	}

	return RepoStatus{
		Name:      name,
		Boarded:   w.Boarded[name],
		Worktrees: wts,
	}
}

// RepoOutline is the fast, filesystem-only structure of a repo.
type RepoOutline struct {
	Name         string
	Boarded      []string
	Worktrees    []string
	LastActivity time.Time
	Err          error
}

// StatusOutline returns the repo structure without any git queries.
// This is instant — just filesystem reads. When recentFirst is true,
// repos and worktrees are sorted most-recent-first; otherwise most-recent-last.
func (w *Workspace) StatusOutline(recentFirst bool) []RepoOutline {
	outlines := make([]RepoOutline, len(w.RepoNames))
	for i, name := range w.RepoNames {
		repoDir := w.RepoDir(name)
		worktrees, err := ListAllWorktrees(repoDir)

		// Compute per-worktree mtime for sorting
		mtimes := make(map[string]time.Time, len(worktrees))
		var lastActivity time.Time
		for _, wt := range worktrees {
			t := worktreeHeadMtime(filepath.Join(repoDir, wt))
			mtimes[wt] = t
			if t.After(lastActivity) {
				lastActivity = t
			}
		}

		// Sort worktrees by activity (keep .ground always first)
		sort.SliceStable(worktrees, func(a, b int) bool {
			if worktrees[a] == GroundDir {
				return true
			}
			if worktrees[b] == GroundDir {
				return false
			}
			if recentFirst {
				return mtimes[worktrees[a]].After(mtimes[worktrees[b]])
			}
			return mtimes[worktrees[a]].Before(mtimes[worktrees[b]])
		})

		outlines[i] = RepoOutline{
			Name:         name,
			Boarded:      w.Boarded[name],
			Worktrees:    worktrees,
			LastActivity: lastActivity,
			Err:          err,
		}
	}

	sort.Slice(outlines, func(i, j int) bool {
		if recentFirst {
			return outlines[i].LastActivity.After(outlines[j].LastActivity)
		}
		return outlines[i].LastActivity.Before(outlines[j].LastActivity)
	})

	return outlines
}

// worktreeHeadMtime returns the mtime of a worktree's HEAD file.
// HEAD gets touched on checkout, commit, pull — a good proxy for "last used".
func worktreeHeadMtime(wtPath string) time.Time {
	gitPath := filepath.Join(wtPath, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return time.Time{}
	}

	if info.IsDir() {
		// Regular git repo — HEAD is at .git/HEAD
		if hi, err := os.Stat(filepath.Join(gitPath, "HEAD")); err == nil {
			return hi.ModTime()
		}
		return time.Time{}
	}

	// Linked worktree — .git is a file containing "gitdir: <path>"
	content, err := os.ReadFile(gitPath)
	if err != nil {
		return time.Time{}
	}
	line := strings.TrimSpace(string(content))
	if !strings.HasPrefix(line, "gitdir: ") {
		return time.Time{}
	}
	gitdir := strings.TrimPrefix(line, "gitdir: ")
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(wtPath, gitdir)
	}

	if hi, err := os.Stat(filepath.Join(gitdir, "HEAD")); err == nil {
		return hi.ModTime()
	}
	return time.Time{}
}

// QueryWorktreeStatus runs git queries for a single worktree. This is the slow part.
func QueryWorktreeStatus(wtPath string) WorktreeStatus {
	name := filepath.Base(wtPath)
	ahead, behind := GitAheadBehind(wtPath)
	return WorktreeStatus{
		Name:   name,
		Branch: GitCurrentBranch(wtPath),
		Dirty:  GitIsDirty(wtPath),
		Ahead:  ahead,
		Behind: behind,
	}
}
