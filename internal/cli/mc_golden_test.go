package cli

import (
	"testing"
	"time"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/x/exp/golden"
)

// goldenTime is a fixed reference time so relative-time rendering is stable.
var goldenTime = time.Date(2025, 1, 20, 12, 0, 0, 0, time.UTC)

func pinClock(t *testing.T) {
	t.Helper()
	orig := nowFunc
	nowFunc = func() time.Time { return goldenTime }
	t.Cleanup(func() { nowFunc = orig })
}

// goldenMCModel builds a rich mcModel with two repos and several worktrees
// in various states. No filesystem or network access is needed.
func goldenMCModel() mcModel {
	ti := textinput.New()
	ti.Prompt = ""
	pi := textinput.New()
	pi.Prompt = ""

	return mcModel{
		ws: &workspace.Workspace{
			Org:           "acme",
			Name:          "Acme Corp",
			DefaultBranch: "main",
			RepoNames:     []string{"frontend", "backend"},
			DisplayNames:  map[string]string{},
			RepoColors:    map[string]string{},
			Boarded:       map[string][]string{"frontend": {"feat-auth"}},
		},
		repos: []mcRepoData{
			{name: "frontend", boarded: []string{"feat-auth"}, worktrees: []string{".ground", "feat-auth", "fix-styles", "redesign"}},
			{name: "backend", boarded: nil, worktrees: []string{".ground", "add-api", "refactor-db"}},
		},
		rows: []mcRow{
			// frontend
			{kind: rowRepoHeader, repo: "frontend"},
			{kind: rowWorktree, repo: "frontend", wt: ".ground", branch: "main", loaded: true},
			{kind: rowWorktree, repo: "frontend", wt: "feat-auth", branch: "feat-auth", loaded: true, dirty: true, ahead: 2, isBoarded: true},
			{kind: rowWorktree, repo: "frontend", wt: "fix-styles", branch: "fix-styles", loaded: true, ahead: 1},
			{kind: rowWorktree, repo: "frontend", wt: "redesign", branch: "redesign", loaded: true, behind: 1},
			// backend
			{kind: rowRepoHeader, repo: "backend"},
			{kind: rowWorktree, repo: "backend", wt: ".ground", branch: "main", loaded: true},
			{kind: rowWorktree, repo: "backend", wt: "add-api", branch: "add-api", loaded: true, behind: 3},
			{kind: rowWorktree, repo: "backend", wt: "refactor-db", branch: "refactor-db", loaded: true, dirty: true},
		},
		cursor:        2, // feat-auth
		width:         120,
		height:        40,
		detailFor:     -1,
		confirmIdx:    -1,
		actionSpinner: -1,
		filterInput:   ti,
		paletteInput:  pi,
	}
}

func TestGolden_MC_BasicTwoRepo(t *testing.T) {
	pinClock(t)
	m := goldenMCModel()
	golden.RequireEqual(t, m.View())
}

func TestGolden_MC_WithPR(t *testing.T) {
	pinClock(t)
	m := goldenMCModel()
	m.rows[2].pr = &github.PR{
		Number:         42,
		Title:          "Add authentication flow",
		HeadRefName:    "feat-auth",
		State:          "OPEN",
		ReviewDecision: "REVIEW_REQUIRED",
		StatusRollup:   "success",
		URL:            "https://github.com/acme/frontend/pull/42",
		Author:         "alice",
	}
	golden.RequireEqual(t, m.View())
}

func TestGolden_MC_GhostPRRow(t *testing.T) {
	pinClock(t)
	m := goldenMCModel()
	// Insert a ghost PR row after the last frontend worktree
	ghost := mcRow{
		kind:   rowGhostPR,
		repo:   "frontend",
		branch: "deps/upgrade-react",
		pr: &github.PR{
			Number:         99,
			Title:          "Upgrade React to v19",
			HeadRefName:    "deps/upgrade-react",
			State:          "OPEN",
			ReviewDecision: "APPROVED",
			StatusRollup:   "success",
			URL:            "https://github.com/acme/frontend/pull/99",
			Author:         "bob",
		},
	}
	m.rows = append(m.rows[:5], append([]mcRow{ghost}, m.rows[5:]...)...)
	golden.RequireEqual(t, m.View())
}

func TestGolden_MC_FilterActive(t *testing.T) {
	pinClock(t)
	m := goldenMCModel()
	m.activeFilters = filterLocal | filterDirty
	m.ensureCursorOnVisible()
	golden.RequireEqual(t, m.View())
}

func TestGolden_MC_ConfirmDialog(t *testing.T) {
	pinClock(t)
	m := goldenMCModel()
	m.confirmIdx = 2 // feat-auth row
	golden.RequireEqual(t, m.View())
}

func TestGolden_MC_HelpOverlay(t *testing.T) {
	pinClock(t)
	m := goldenMCModel()
	m.showHelp = true
	golden.RequireEqual(t, m.View())
}

func TestGolden_MC_DetailLoaded(t *testing.T) {
	pinClock(t)
	m := goldenMCModel()
	m.detailFor = 2 // matches cursor
	m.detail = detailData{
		loaded: true,
		commits: []string{
			"feat: add login form validation",
			"feat: wire up auth API client",
			"chore: scaffold auth module",
		},
		diffStat:   " 5 files changed, 142 insertions(+), 23 deletions(-)",
		stashCount: 1,
	}
	golden.RequireEqual(t, m.View())
}

func TestGolden_MC_DetailWithPRAndChecks(t *testing.T) {
	pinClock(t)
	m := goldenMCModel()
	m.rows[2].pr = &github.PR{
		Number:         42,
		Title:          "Add authentication flow",
		HeadRefName:    "feat-auth",
		State:          "OPEN",
		ReviewDecision: "CHANGES_REQUESTED",
		StatusRollup:   "failure",
		URL:            "https://github.com/acme/frontend/pull/42",
		Author:         "alice",
	}
	m.detailFor = 2
	m.detail = detailData{
		loaded:  true,
		prTitle: "Add authentication flow with OAuth2 support",
		prBody:  "## Summary\n\nAdds OAuth2 login and session management.\n\n- Google provider\n- GitHub provider\n- Token refresh logic",
		commits: []string{
			"feat: add OAuth2 providers",
			"feat: implement token refresh",
		},
		diffStat: " 8 files changed, 312 insertions(+), 45 deletions(-)",
		checks: []github.CheckRun{
			{Name: "build", Conclusion: "SUCCESS"},
			{Name: "lint", Conclusion: "SUCCESS"},
			{Name: "test-unit", Conclusion: "FAILURE"},
			{Name: "test-e2e", Conclusion: "SUCCESS"},
		},
	}
	golden.RequireEqual(t, m.View())
}

func TestGolden_MC_GroundDetail(t *testing.T) {
	pinClock(t)
	m := goldenMCModel()
	m.cursor = 1 // .ground row for frontend
	m.detailFor = 1
	m.detail = detailData{
		loaded: true,
		landings: []github.PR{
			{Number: 88, Title: "Fix header alignment", Author: "carol", MergedAt: "2020-01-15T10:30:00Z"},
			{Number: 85, Title: "Add dark mode toggle", Author: "alice", MergedAt: "2020-01-10T14:00:00Z"},
			{Number: 80, Title: "Refactor navigation component", Author: "bob", MergedAt: "2020-01-05T09:00:00Z"},
		},
		actions: []github.WorkflowRun{
			{Name: "CI", Status: "completed", Conclusion: "success", CreatedAt: "2020-01-15T10:35:00Z"},
			{Name: "Deploy", Status: "completed", Conclusion: "failure", CreatedAt: "2020-01-15T10:36:00Z"},
			{Name: "Nightly", Status: "in_progress", Conclusion: "", CreatedAt: "2020-01-15T11:00:00Z"},
		},
	}
	golden.RequireEqual(t, m.View())
}

func TestGolden_MC_MergedRow(t *testing.T) {
	pinClock(t)
	m := goldenMCModel()
	m.rows[3].merged = true // fix-styles is merged
	m.cursor = 3
	golden.RequireEqual(t, m.View())
}

func TestGolden_MC_BoardedRow(t *testing.T) {
	pinClock(t)
	m := goldenMCModel()
	// feat-auth is already boarded; move cursor there to see detail
	m.cursor = 2
	golden.RequireEqual(t, m.View())
}

func TestGolden_MC_PaletteOpen(t *testing.T) {
	pinClock(t)
	m := goldenMCModel()
	m.paletteActive = true
	m.paletteInput.Focus()
	golden.RequireEqual(t, m.View())
}
