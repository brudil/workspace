package cli

import (
	"fmt"
	"testing"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/x/exp/golden"
)

// goldenStatusModel builds a statusModel directly without filesystem access.
func goldenStatusModel() statusModel {
	return statusModel{
		ws: &workspace.Workspace{
			Org:          "acme",
			Name:         "Acme Corp",
			RepoNames:    []string{"frontend"},
			DisplayNames: map[string]string{},
			RepoColors:   map[string]string{},
			Boarded:      map[string][]string{"frontend": {"feat-auth"}},
		},
		repos: []repoView{
			{
				name:    "frontend",
				boarded: []string{"feat-auth"},
				worktrees: []worktreeView{
					{name: ".ground", branch: "main", loaded: true},
					{name: "feat-auth", branch: "feat-auth", dirty: true, ahead: 2, loaded: true},
					{name: "fix-styles", branch: "fix-styles", loaded: true},
				},
			},
		},
		total: 3,
		done:  3,
	}
}

func TestGolden_Status_BasicView(t *testing.T) {
	m := goldenStatusModel()
	golden.RequireEqual(t, m.View())
}

func TestGolden_Status_WithPRs(t *testing.T) {
	m := goldenStatusModel()
	m.repos[0].prs = map[string]*github.PR{
		"feat-auth": {
			Number:         42,
			Title:          "Add authentication flow",
			HeadRefName:    "feat-auth",
			State:          "OPEN",
			ReviewDecision: "APPROVED",
			StatusRollup:   "success",
			URL:            "https://github.com/acme/frontend/pull/42",
		},
	}
	m.repos[0].prsLoaded = true
	golden.RequireEqual(t, m.View())
}

func TestGolden_Status_RepoError(t *testing.T) {
	m := goldenStatusModel()
	m.ws.RepoNames = []string{"frontend", "backend"}
	m.repos = append(m.repos, repoView{
		name: "backend",
		err:  fmt.Errorf("permission denied: repos/backend"),
	})
	golden.RequireEqual(t, m.View())
}

func TestGolden_Status_MultipleRepos(t *testing.T) {
	m := goldenStatusModel()
	m.ws.RepoNames = []string{"frontend", "backend"}
	m.repos = append(m.repos, repoView{
		name: "backend",
		worktrees: []worktreeView{
			{name: ".ground", branch: "main", loaded: true},
			{name: "add-api", branch: "add-api", behind: 3, loaded: true},
			{name: "refactor-db", branch: "refactor-db", dirty: true, loaded: true},
		},
	})
	m.total = 6
	m.done = 6
	golden.RequireEqual(t, m.View())
}

func TestGolden_Status_Loading(t *testing.T) {
	m := goldenStatusModel()
	// Reset loaded state to simulate still-loading
	m.done = 0
	for i := range m.repos[0].worktrees {
		m.repos[0].worktrees[i].loaded = false
	}
	golden.RequireEqual(t, m.View())
}
