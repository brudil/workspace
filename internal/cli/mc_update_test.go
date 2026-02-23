package cli

import (
	"fmt"
	"testing"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func baseMCModel() mcModel {
	ti := textinput.New()
	ti.Prompt = ""
	pi := textinput.New()
	pi.Prompt = ""

	return mcModel{
		ws: &workspace.Workspace{
			Org:          "testorg",
			RepoNames:    []string{"repo1", "repo2"},
			DisplayNames: map[string]string{},
			RepoColors:   map[string]string{},
			Boarded:      map[string][]string{},
		},
		rows: []mcRow{
			{kind: rowRepoHeader, repo: "repo1"},
			{kind: rowWorktree, repo: "repo1", wt: "main", branch: "main"},
			{kind: rowWorktree, repo: "repo1", wt: "feat", branch: "feat"},
			{kind: rowRepoHeader, repo: "repo2"},
			{kind: rowWorktree, repo: "repo2", wt: "main", branch: "main"},
		},
		cursor: 1,
		repos: []mcRepoData{
			{name: "repo1", worktrees: []string{"main", "feat"}},
			{name: "repo2", worktrees: []string{"main"}},
		},
		wtTotal:       3,
		prTotal:       2,
		detailFor:     -1,
		confirmIdx:    -1,
		actionSpinner: -1,
		filterInput:   ti,
		paletteInput:  pi,
	}
}

func TestMCUpdate_WtStatusMsg(t *testing.T) {
	m := baseMCModel()

	msg := mcWtStatusMsg{
		repo: "repo1",
		wt: workspace.WorktreeStatus{
			Name:   "feat",
			Branch: "feature/xyz",
			Dirty:  true,
			Ahead:  2,
			Behind: 1,
		},
	}

	result, _ := m.Update(msg)
	rm := result.(mcModel)

	row := rm.rows[2] // feat is at index 2
	if row.branch != "feature/xyz" {
		t.Errorf("branch = %q, want %q", row.branch, "feature/xyz")
	}
	if !row.dirty {
		t.Error("dirty = false, want true")
	}
	if row.ahead != 2 {
		t.Errorf("ahead = %d, want 2", row.ahead)
	}
	if row.behind != 1 {
		t.Errorf("behind = %d, want 1", row.behind)
	}
	if !row.loaded {
		t.Error("loaded = false, want true")
	}
	if rm.wtDone != 1 {
		t.Errorf("wtDone = %d, want 1", rm.wtDone)
	}
}

func TestMCUpdate_PRsMsg_Success(t *testing.T) {
	m := baseMCModel()
	m.rows[1].branch = "main"
	m.rows[1].loaded = true
	m.rows[2].branch = "feat"
	m.rows[2].loaded = true

	msg := mcPRsMsg{
		repo: "repo1",
		prs: []github.PR{
			{Number: 42, HeadRefName: "feat", Title: "My PR"},
			{Number: 99, HeadRefName: "unmatched-branch", Title: "Ghost PR"},
		},
	}

	result, _ := m.Update(msg)
	rm := result.(mcModel)

	if rm.prDone != 1 {
		t.Errorf("prDone = %d, want 1", rm.prDone)
	}

	// feat row should have PR attached
	if rm.rows[2].pr == nil {
		t.Fatal("feat row should have PR")
	}
	if rm.rows[2].pr.Number != 42 {
		t.Errorf("PR number = %d, want 42", rm.rows[2].pr.Number)
	}

	// Ghost row should be inserted for unmatched PR
	hasGhost := false
	for _, row := range rm.rows {
		if row.kind == rowGhostPR && row.branch == "unmatched-branch" {
			hasGhost = true
			break
		}
	}
	if !hasGhost {
		t.Error("expected ghost row for unmatched PR branch 'unmatched-branch'")
	}
}

func TestMCUpdate_PRsMsg_Error(t *testing.T) {
	m := baseMCModel()

	msg := mcPRsMsg{
		repo: "repo1",
		err:  fmt.Errorf("gh failed"),
	}

	result, _ := m.Update(msg)
	rm := result.(mcModel)

	if rm.prDone != 1 {
		t.Errorf("prDone = %d, want 1", rm.prDone)
	}
	if rm.prErrors != 1 {
		t.Errorf("prErrors = %d, want 1", rm.prErrors)
	}
	// Rows should be unchanged
	if len(rm.rows) != len(m.rows) {
		t.Errorf("row count changed: %d -> %d", len(m.rows), len(rm.rows))
	}
}

func TestMCUpdate_MergedMsg(t *testing.T) {
	m := baseMCModel()
	m.rows[1].branch = "main"
	m.rows[2].branch = "feat"

	msg := mcMergedMsg{
		repo:     "repo1",
		branches: map[string]bool{"feat": true},
	}

	result, _ := m.Update(msg)
	rm := result.(mcModel)

	if rm.rows[2].merged != true {
		t.Error("feat row should be marked merged")
	}
	if rm.rows[1].merged != false {
		t.Error("main row should NOT be marked merged")
	}
}

func TestMCUpdate_GhUserMsg(t *testing.T) {
	m := baseMCModel()

	msg := mcGhUserMsg{login: "alice"}

	result, _ := m.Update(msg)
	rm := result.(mcModel)

	if rm.ghUser != "alice" {
		t.Errorf("ghUser = %q, want %q", rm.ghUser, "alice")
	}
}

func TestMCUpdate_WorktreeCreatedMsg_Success(t *testing.T) {
	m := baseMCModel()
	// Insert a ghost row
	m.rows = append(m.rows[:3], append([]mcRow{
		{kind: rowGhostPR, repo: "repo1", branch: "new-feat", pr: &github.PR{Number: 5}},
	}, m.rows[3:]...)...)
	m.actionSpinner = 3

	msg := mcWorktreeCreatedMsg{
		rowIdx: 3,
		repo:   "repo1",
		branch: "new-feat",
	}

	result, cmd := m.Update(msg)
	rm := result.(mcModel)

	if rm.actionSpinner != -1 {
		t.Errorf("actionSpinner = %d, want -1", rm.actionSpinner)
	}
	if rm.rows[3].kind != rowWorktree {
		t.Errorf("row kind = %d, want rowWorktree (%d)", rm.rows[3].kind, rowWorktree)
	}
	if cmd == nil {
		t.Error("expected a cmd to query the new worktree")
	}
}

func TestMCUpdate_WorktreeCreatedMsg_Error(t *testing.T) {
	m := baseMCModel()
	m.rows = append(m.rows[:3], append([]mcRow{
		{kind: rowGhostPR, repo: "repo1", branch: "new-feat"},
	}, m.rows[3:]...)...)
	m.actionSpinner = 3

	msg := mcWorktreeCreatedMsg{
		rowIdx: 3,
		repo:   "repo1",
		branch: "new-feat",
		err:    fmt.Errorf("failed to create"),
	}

	result, _ := m.Update(msg)
	rm := result.(mcModel)

	if rm.actionSpinner != -1 {
		t.Errorf("actionSpinner = %d, want -1", rm.actionSpinner)
	}
	// Row should still be ghost
	if rm.rows[3].kind != rowGhostPR {
		t.Errorf("row kind = %d, want rowGhostPR (%d)", rm.rows[3].kind, rowGhostPR)
	}
}

func TestMCUpdate_WorktreeDeletedMsg(t *testing.T) {
	m := baseMCModel()
	m.cursor = 2 // on "feat"
	m.actionSpinner = 2
	origRowCount := len(m.rows)

	msg := mcWorktreeDeletedMsg{
		rowIdx: 2,
		repo:   "repo1",
		branch: "feat",
	}

	result, _ := m.Update(msg)
	rm := result.(mcModel)

	if rm.actionSpinner != -1 {
		t.Errorf("actionSpinner = %d, want -1", rm.actionSpinner)
	}
	if len(rm.rows) != origRowCount-1 {
		t.Errorf("row count = %d, want %d", len(rm.rows), origRowCount-1)
	}
}

func TestMCUpdate_WindowSizeMsg(t *testing.T) {
	m := baseMCModel()

	msg := tea.WindowSizeMsg{Width: 120, Height: 40}

	result, _ := m.Update(msg)
	rm := result.(mcModel)

	if rm.width != 120 {
		t.Errorf("width = %d, want 120", rm.width)
	}
	if rm.height != 40 {
		t.Errorf("height = %d, want 40", rm.height)
	}
}

func TestMCUpdate_DetailTickMsg_Matching(t *testing.T) {
	m := baseMCModel()
	m.detailSeq = 5
	m.width = 100 // needs width for fetchDetail
	m.height = 40

	msg := mcDetailTickMsg{seq: 5}

	_, cmd := m.Update(msg)
	if cmd == nil {
		t.Error("matching seq should return a cmd")
	}
}

func TestMCUpdate_DetailTickMsg_Stale(t *testing.T) {
	m := baseMCModel()
	m.detailSeq = 5

	msg := mcDetailTickMsg{seq: 3} // stale

	_, cmd := m.Update(msg)
	if cmd != nil {
		t.Error("stale seq should return nil cmd")
	}
}

func TestMCUpdate_DetailDataMsg(t *testing.T) {
	m := baseMCModel()
	m.cursor = 2

	data := detailData{
		commits: []string{"fix: something"},
		loaded:  true,
	}
	msg := mcDetailDataMsg{rowIdx: 2, data: data}

	result, _ := m.Update(msg)
	rm := result.(mcModel)

	if rm.detailFor != 2 {
		t.Errorf("detailFor = %d, want 2", rm.detailFor)
	}
	if !rm.detail.loaded {
		t.Error("detail should be loaded")
	}
	if len(rm.detail.commits) != 1 {
		t.Errorf("detail commits = %d, want 1", len(rm.detail.commits))
	}
}

func TestMCUpdate_DetailDataMsg_WrongRow(t *testing.T) {
	m := baseMCModel()
	m.cursor = 2

	msg := mcDetailDataMsg{rowIdx: 4, data: detailData{loaded: true}}

	result, _ := m.Update(msg)
	rm := result.(mcModel)

	if rm.detail.loaded {
		t.Error("detail should not be loaded for wrong row")
	}
}
