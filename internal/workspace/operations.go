package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// SetupResult reports what happened for each repo during setup.
type SetupResult struct {
	Repo    string
	Cloned  bool
	Skipped bool
	Err     error
}

// EnsureReposDir creates the repos directory if it doesn't exist.
func (w *Workspace) EnsureReposDir() error {
	return os.MkdirAll(w.ReposDir(), 0755)
}

// Setup clones all repos that don't already exist.
func (w *Workspace) Setup() []SetupResult {
	w.EnsureReposDir()

	results := make([]SetupResult, len(w.RepoNames))
	for i, name := range w.RepoNames {
		results[i] = w.SetupRepo(name)
	}

	DisableWorkspaceGit(w.Root)
	return results
}

// SetupRepo clones a single repo if it doesn't already exist.
func (w *Workspace) SetupRepo(name string) SetupResult {
	repoDir := w.RepoDir(name)
	bareDir := w.BareDir(name)

	if _, err := os.Stat(bareDir); err == nil {
		return SetupResult{Repo: name, Skipped: true}
	}

	os.MkdirAll(repoDir, 0755)

	url := RepoCloneURL(w.Org, name, w.GitProtocol)
	if err := GitCloneBare(url, bareDir); err != nil {
		return SetupResult{Repo: name, Err: fmt.Errorf("cloning %s: %w", name, err)}
	}

	mainWT := w.MainWorktree(name)
	if err := GitWorktreeAddBranch(bareDir, mainWT, w.DefaultBranch); err != nil {
		return SetupResult{Repo: name, Err: fmt.Errorf("creating %s worktree for %s: %w", w.DefaultBranch, name, err)}
	}

	return SetupResult{Repo: name, Cloned: true}
}

// FetchResult reports what happened for each repo during fetch.
type FetchResult struct {
	Repo string
	Err  error
}

// FetchRepo fetches a single repo.
func (w *Workspace) FetchRepo(name string) FetchResult {
	err := GitFetch(w.BareDir(name))
	return FetchResult{Repo: name, Err: err}
}

// FetchAll fetches all repos in parallel.
func (w *Workspace) FetchAll() []FetchResult {
	results := make([]FetchResult, len(w.RepoNames))
	var wg sync.WaitGroup

	for i, name := range w.RepoNames {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			results[idx] = w.FetchRepo(name)
		}(i, name)
	}

	wg.Wait()
	return results
}

// LiftWorktree creates a new branch from base and sets up a worktree for it.
// Returns the capsule directory name used for the worktree.
func (w *Workspace) LiftWorktree(repo, branch, base string) (string, error) {
	bareDir := w.BareDir(repo)
	GitFetch(bareDir) // best-effort fetch

	capsule := UniqueCapsuleName(w.RepoDir(repo), branch)
	wtPath := filepath.Join(w.RepoDir(repo), capsule)
	if err := GitWorktreeAddNewBranch(bareDir, wtPath, branch, base); err != nil {
		return "", fmt.Errorf("creating worktree: %w", err)
	}
	return capsule, nil
}

// DockWorktree checks out an existing remote branch into a new worktree.
// Returns the capsule directory name used for the worktree.
func (w *Workspace) DockWorktree(repo, branch string) (string, error) {
	bareDir := w.BareDir(repo)
	GitFetch(bareDir) // best-effort fetch

	capsule := UniqueCapsuleName(w.RepoDir(repo), branch)
	wtPath := filepath.Join(w.RepoDir(repo), capsule)
	if err := GitWorktreeAddBranch(bareDir, wtPath, branch); err != nil {
		return "", fmt.Errorf("creating worktree: %w", err)
	}
	return capsule, nil
}

// RemovePrecheck holds info the CLI needs before confirming removal.
type RemovePrecheck struct {
	IsDirty bool
}

// CheckRemoveWorktree validates a removal and returns state for CLI prompting.
func (w *Workspace) CheckRemoveWorktree(repo, branch string) (*RemovePrecheck, error) {
	if branch == w.DefaultBranch || branch == GroundDir {
		return nil, fmt.Errorf("cannot remove the default branch worktree (%s)", branch)
	}

	wtPath := filepath.Join(w.RepoDir(repo), branch)

	return &RemovePrecheck{
		IsDirty: GitIsDirty(wtPath),
	}, nil
}

// RemoveWorktree removes a worktree.
func (w *Workspace) RemoveWorktree(repo, branch string) error {
	repoDir := w.RepoDir(repo)

	wtPath := filepath.Join(repoDir, branch)
	if err := GitWorktreeRemove(w.BareDir(repo), wtPath); err != nil {
		return fmt.Errorf("removing worktree: %w", err)
	}
	return nil
}

// Board adds a capsule to the boarded set for a repo.
// Returns an error if the capsule directory doesn't exist.
// No-op if already boarded.
func (w *Workspace) Board(repo, capsule string) error {
	wtPath := filepath.Join(w.RepoDir(repo), capsule)
	if _, err := os.Stat(wtPath); err != nil {
		return fmt.Errorf("capsule %s/%s does not exist", repo, capsule)
	}
	if w.IsBoarded(repo, capsule) {
		return nil
	}
	w.Boarded[repo] = append(w.Boarded[repo], capsule)
	return nil
}

// Unboard removes a capsule from the boarded set for a repo.
// Returns an error if the capsule is not currently boarded.
func (w *Workspace) Unboard(repo, capsule string) error {
	capsules := w.Boarded[repo]
	for i, c := range capsules {
		if c == capsule {
			w.Boarded[repo] = append(capsules[:i], capsules[i+1:]...)
			if len(w.Boarded[repo]) == 0 {
				delete(w.Boarded, repo)
			}
			return nil
		}
	}
	return fmt.Errorf("capsule %s/%s is not boarded", repo, capsule)
}
