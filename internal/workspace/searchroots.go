package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// ScopeMode decides which worktrees of a repo are searched when the caller
// did not name one explicitly.
type ScopeMode int

const (
	// ScopeHere uses the worktree the caller is standing in for that repo and
	// .ground for every other repo. This is the default: it returns each
	// match once, from the tree the caller is most likely working against.
	ScopeHere ScopeMode = iota
	// ScopeGround uses .ground for every repo, wherever the caller stands.
	ScopeGround
	// ScopeAll uses .ground and every capsule. Expect duplicates.
	ScopeAll
)

// RepoSelector names a repo and optionally a specific worktree within it.
// An empty Worktree means "let the ScopeMode decide".
type RepoSelector struct {
	Repo     string
	Worktree string
}

// SearchRoot is one directory to search, with the repo and worktree it belongs to.
type SearchRoot struct {
	Repo     string
	Worktree string
	Path     string // absolute path to the worktree directory
}

// RelPath returns the root's path relative to the workspace root, using
// forward slashes: "repos/<repo>/<worktree>". This is the form ripgrep
// matches slash-containing globs against when run from the workspace root.
func (r SearchRoot) RelPath() string {
	return "repos/" + r.Repo + "/" + r.Worktree
}

// SearchRoots resolves the directories to search. With no selectors every
// repo in the workspace is selected. Roots come back ordered by repo name,
// .ground before capsules within a repo, so output is stable.
//
// .bare and .silo are never chosen by a ScopeMode; they only appear when a
// selector names them explicitly. A repo selected implicitly whose .ground is
// missing is skipped; an explicitly selected worktree that does not exist is
// an error.
func (w *Workspace) SearchRoots(cwd string, selectors []RepoSelector, mode ScopeMode) ([]SearchRoot, error) {
	explicit := len(selectors) > 0
	if !explicit {
		selectors = make([]RepoSelector, 0, len(w.RepoNames))
		for _, name := range w.RepoNames {
			selectors = append(selectors, RepoSelector{Repo: name})
		}
	}

	hereRepo, hereWT, _ := DetectRepo(w.Root, cwd)
	if hereWT == ".bare" || hereWT == SiloDir {
		hereRepo, hereWT = "", ""
	}

	perRepo := make(map[string][]string)
	for _, sel := range selectors {
		if _, ok := w.ResolveAlias(sel.Repo); !ok {
			return nil, fmt.Errorf("unknown repo %q", sel.Repo)
		}
		repoDir := w.RepoDir(sel.Repo)

		var wts []string
		switch {
		case sel.Worktree != "":
			if !isDir(filepath.Join(repoDir, sel.Worktree)) {
				return nil, fmt.Errorf("worktree %s/%s does not exist", sel.Repo, sel.Worktree)
			}
			wts = []string{sel.Worktree}
		case mode == ScopeAll:
			all, err := ListAllWorktrees(repoDir)
			if err != nil {
				if os.IsNotExist(err) && !explicit {
					continue
				}
				return nil, fmt.Errorf("listing worktrees for %s: %w", sel.Repo, err)
			}
			wts = all
		case mode == ScopeHere && sel.Repo == hereRepo && isDir(filepath.Join(repoDir, hereWT)):
			wts = []string{hereWT}
		default:
			if !isDir(filepath.Join(repoDir, GroundDir)) {
				if explicit {
					return nil, fmt.Errorf("worktree %s/%s does not exist", sel.Repo, GroundDir)
				}
				continue
			}
			wts = []string{GroundDir}
		}

		for _, wt := range wts {
			if !slices.Contains(perRepo[sel.Repo], wt) {
				perRepo[sel.Repo] = append(perRepo[sel.Repo], wt)
			}
		}
	}

	var roots []SearchRoot
	for _, repo := range w.RepoNames {
		wts, ok := perRepo[repo]
		if !ok {
			continue
		}
		// .ground first, then the rest in name order.
		slices.SortFunc(wts, func(a, b string) int {
			switch {
			case a == b:
				return 0
			case a == GroundDir:
				return -1
			case b == GroundDir:
				return 1
			default:
				if a < b {
					return -1
				}
				return 1
			}
		})
		for _, wt := range wts {
			roots = append(roots, SearchRoot{
				Repo:     repo,
				Worktree: wt,
				Path:     filepath.Join(w.RepoDir(repo), wt),
			})
		}
	}
	return roots, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
