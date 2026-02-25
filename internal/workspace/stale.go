package workspace

import (
	"fmt"
	"path/filepath"
	"time"
)

func DaysSince(t time.Time) int {
	return int(time.Since(t).Hours() / 24)
}

// FormatAge returns a human-readable relative time, or empty string for zero time.
func FormatAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	days := DaysSince(t)
	switch {
	case days < 1:
		return "today"
	case days == 1:
		return "1 day ago"
	case days < 30:
		return fmt.Sprintf("%d days ago", days)
	case days < 60:
		return "1 month ago"
	default:
		return fmt.Sprintf("%d months ago", days/30)
	}
}

// CapsuleInfo describes a capsule for debrief reporting.
type CapsuleInfo struct {
	Repo       string
	Name       string
	Branch     string
	Dirty      bool
	DirtyCount int
	IsBoarded  bool
	LastCommit time.Time
	Merged     bool
	Inactive   bool // true if past the inactivity threshold
	Ahead      int
	Behind     int
}

// FindAllCapsules scans repos and returns info about every non-default capsule.
// If repoFilter is non-empty, only that repo is scanned.
func (w *Workspace) FindAllCapsules(maxDays int, repoFilter string) []CapsuleInfo {
	repos := w.RepoNames
	if repoFilter != "" {
		repos = []string{repoFilter}
	}

	var capsules []CapsuleInfo
	cutoff := time.Now().AddDate(0, 0, -maxDays)

	for _, repo := range repos {
		repoDir := w.RepoDir(repo)
		bareDir := w.BareDir(repo)

		worktrees, err := ListWorktrees(repoDir)
		if err != nil {
			continue
		}

		mergedSet := make(map[string]bool)
		for _, b := range GitMergedBranches(bareDir, w.DefaultBranch) {
			mergedSet[b] = true
		}

		defaultTip := GitRevParse(bareDir, w.DefaultBranch)

		for _, wt := range worktrees {
			if wt == w.DefaultBranch {
				continue
			}

			wtPath := filepath.Join(repoDir, wt)
			branch := GitCurrentBranch(wtPath)
			lastCommit := GitLastCommitDate(wtPath)
			// A branch at the same commit as the default branch was just
			// created from it — not actually landed via a merge.
			branchTip := GitRevParse(wtPath, "HEAD")
			merged := mergedSet[branch] && branchTip != defaultTip
			inactive := !lastCommit.IsZero() && lastCommit.Before(cutoff)
			dirtyCount := GitDirtyCount(wtPath)
			dirty := dirtyCount > 0

			ahead, behind := GitAheadBehind(wtPath)

			capsules = append(capsules, CapsuleInfo{
				Repo:       repo,
				Name:       wt,
				Branch:     branch,
				Dirty:      dirty,
				DirtyCount: dirtyCount,
				IsBoarded:  w.IsBoarded(repo, wt),
				LastCommit: lastCommit,
				Merged:     merged,
				Inactive:   inactive,
				Ahead:      ahead,
				Behind:     behind,
			})
		}
	}

	return capsules
}
