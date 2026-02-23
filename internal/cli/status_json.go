package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/workspace"
)

// --- JSON output types ---

type statusJSON struct {
	Workspace string     `json:"workspace"`
	Repos     []repoJSON `json:"repos"`
}

type repoJSON struct {
	Name      string         `json:"name"`
	Boarded   []string       `json:"boarded"`
	Error     string         `json:"error,omitempty"`
	Worktrees []worktreeJSON `json:"worktrees"`
}

type worktreeJSON struct {
	Name   string  `json:"name"`
	Branch string  `json:"branch"`
	Dirty  bool    `json:"dirty"`
	Ahead  int     `json:"ahead"`
	Behind int     `json:"behind"`
	PR     *prJSON `json:"pr,omitempty"`
}

type prJSON struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	State          string `json:"state"`
	URL            string `json:"url"`
	ReviewDecision string `json:"review_decision"`
	CheckStatus    string `json:"check_status"`
}

func runStatusJSON(ws *workspace.Workspace, gh github.Client) error {
	outlines := ws.StatusOutline(false)

	result := statusJSON{
		Workspace: ws.Title(),
		Repos:     make([]repoJSON, len(outlines)),
	}

	// Collect git status and PRs in parallel.
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Per-repo PR results, indexed by repo name.
	prsByRepo := make(map[string]map[string]*github.PR)

	for i, o := range outlines {
		rj := repoJSON{
			Name:    o.Name,
			Boarded: o.Boarded,
		}
		if o.Err != nil {
			rj.Error = o.Err.Error()
			result.Repos[i] = rj
			continue
		}

		rj.Worktrees = make([]worktreeJSON, len(o.Worktrees))
		result.Repos[i] = rj

		// Query git status for each worktree.
		for j, wtName := range o.Worktrees {
			wg.Add(1)
			go func(repoIdx, wtIdx int, repoName, wtName string) {
				defer wg.Done()
				wtPath := filepath.Join(ws.RepoDir(repoName), wtName)
				st := workspace.QueryWorktreeStatus(wtPath)

				mu.Lock()
				result.Repos[repoIdx].Worktrees[wtIdx] = worktreeJSON{
					Name:   st.Name,
					Branch: st.Branch,
					Dirty:  st.Dirty,
					Ahead:  st.Ahead,
					Behind: st.Behind,
				}
				mu.Unlock()
			}(i, j, o.Name, wtName)
		}

		// Query PRs for this repo.
		wg.Add(1)
		go func(repoName string) {
			defer wg.Done()
			prs, err := gh.PRsForRepo(ws.Org, repoName)
			if err != nil {
				return // silent, same as TUI
			}
			m := make(map[string]*github.PR, len(prs))
			for k := range prs {
				m[prs[k].HeadRefName] = &prs[k]
			}
			mu.Lock()
			prsByRepo[repoName] = m
			mu.Unlock()
		}(o.Name)
	}

	wg.Wait()

	// Attach PRs to worktrees using branch-then-name lookup (same as TUI).
	for i := range result.Repos {
		prs := prsByRepo[result.Repos[i].Name]
		if prs == nil {
			continue
		}
		for j := range result.Repos[i].Worktrees {
			wt := &result.Repos[i].Worktrees[j]
			var pr *github.PR
			if wt.Branch != "" {
				pr = prs[wt.Branch]
			}
			if pr == nil {
				pr = prs[wt.Name]
			}
			if pr != nil {
				wt.PR = &prJSON{
					Number:         pr.Number,
					Title:          pr.Title,
					State:          pr.State,
					URL:            pr.URL,
					ReviewDecision: pr.ReviewDecision,
					CheckStatus:    pr.StatusRollup,
				}
			}
		}
	}

	return json.NewEncoder(os.Stdout).Encode(result)
}
