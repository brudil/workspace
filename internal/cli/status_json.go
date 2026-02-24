package cli

import (
	"encoding/json"
	"os"

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

	for i, o := range outlines {
		rj := repoJSON{
			Name:    o.Name,
			Boarded: o.Boarded,
		}
		if o.Err != nil {
			rj.Error = o.Err.Error()
		} else {
			rj.Worktrees = make([]worktreeJSON, len(o.Worktrees))
		}
		result.Repos[i] = rj
	}

	data := collectStatusData(ws, gh, outlines)

	for i, o := range outlines {
		if o.Err != nil {
			continue
		}
		for j := range o.Worktrees {
			st := data.statuses[i][j]
			result.Repos[i].Worktrees[j] = worktreeJSON{
				Name:   st.Name,
				Branch: st.Branch,
				Dirty:  st.Dirty,
				Ahead:  st.Ahead,
				Behind: st.Behind,
			}
		}
	}

	for i := range result.Repos {
		prs := data.prsByRepo[result.Repos[i].Name]
		if prs == nil {
			continue
		}
		for j := range result.Repos[i].Worktrees {
			wt := &result.Repos[i].Worktrees[j]
			if pr := lookupPR(prs, wt.Branch, wt.Name); pr != nil {
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
