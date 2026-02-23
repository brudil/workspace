package cli

import (
	"fmt"
	"testing"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

func baseStatusModel() statusModel {
	return statusModel{
		ws: &workspace.Workspace{
			Org:          "testorg",
			RepoNames:    []string{"repo1"},
			DisplayNames: map[string]string{},
			RepoColors:   map[string]string{},
		},
		repos: []repoView{
			{
				name: "repo1",
				worktrees: []worktreeView{
					{name: "main"},
					{name: "feat"},
				},
			},
		},
		total:   2,
		prTotal: 1,
	}
}

func TestStatusUpdate_WtStatusMsg(t *testing.T) {
	m := baseStatusModel()

	msg := wtStatusMsg{
		repo: "repo1",
		wt: workspace.WorktreeStatus{
			Name:   "main",
			Branch: "main",
			Dirty:  true,
			Ahead:  1,
			Behind: 2,
		},
	}

	result, _ := m.Update(msg)
	sm := result.(statusModel)

	wt := sm.repos[0].worktrees[0]
	if wt.branch != "main" {
		t.Errorf("branch = %q, want %q", wt.branch, "main")
	}
	if !wt.dirty {
		t.Error("dirty = false, want true")
	}
	if wt.ahead != 1 {
		t.Errorf("ahead = %d, want 1", wt.ahead)
	}
	if wt.behind != 2 {
		t.Errorf("behind = %d, want 2", wt.behind)
	}
	if !wt.loaded {
		t.Error("loaded = false, want true")
	}
	if sm.done != 1 {
		t.Errorf("done = %d, want 1", sm.done)
	}
}

func TestStatusUpdate_WtStatusMsg_AllDone(t *testing.T) {
	m := baseStatusModel()
	m.done = 1   // one already done
	m.prDone = 1 // PRs already done

	msg := wtStatusMsg{
		repo: "repo1",
		wt:   workspace.WorktreeStatus{Name: "feat", Branch: "feat"},
	}

	_, cmd := m.Update(msg)
	if !isQuitCmd(cmd) {
		t.Error("expected tea.Quit when all done")
	}
}

func TestStatusUpdate_WtStatusMsg_PRsPending(t *testing.T) {
	m := baseStatusModel()
	m.done = 1   // one already done
	m.prDone = 0 // PRs still pending

	msg := wtStatusMsg{
		repo: "repo1",
		wt:   workspace.WorktreeStatus{Name: "feat", Branch: "feat"},
	}

	_, cmd := m.Update(msg)
	if isQuitCmd(cmd) {
		t.Error("should not quit when PRs still pending")
	}
}

func TestStatusUpdate_RepoPRsMsg_Success(t *testing.T) {
	m := baseStatusModel()

	msg := repoPRsMsg{
		repo: "repo1",
		prs: []github.PR{
			{Number: 10, HeadRefName: "feat"},
		},
	}

	result, _ := m.Update(msg)
	sm := result.(statusModel)

	if sm.prDone != 1 {
		t.Errorf("prDone = %d, want 1", sm.prDone)
	}
	if !sm.repos[0].prsLoaded {
		t.Error("prsLoaded = false, want true")
	}
	if sm.repos[0].prs == nil {
		t.Fatal("prs map should not be nil")
	}
	pr, ok := sm.repos[0].prs["feat"]
	if !ok {
		t.Fatal("expected PR for 'feat' branch")
	}
	if pr.Number != 10 {
		t.Errorf("PR number = %d, want 10", pr.Number)
	}
}

func TestStatusUpdate_RepoPRsMsg_Error(t *testing.T) {
	m := baseStatusModel()

	msg := repoPRsMsg{
		repo: "repo1",
		err:  fmt.Errorf("gh failed"),
	}

	result, _ := m.Update(msg)
	sm := result.(statusModel)

	if sm.prDone != 1 {
		t.Errorf("prDone = %d, want 1", sm.prDone)
	}
	if sm.prErrors != 1 {
		t.Errorf("prErrors = %d, want 1", sm.prErrors)
	}
}

func TestStatusUpdate_KeyQ(t *testing.T) {
	m := baseStatusModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !isQuitCmd(cmd) {
		t.Error("expected tea.Quit on 'q'")
	}
}

func TestStatusUpdate_KeyCtrlC(t *testing.T) {
	m := baseStatusModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !isQuitCmd(cmd) {
		t.Error("expected tea.Quit on ctrl+c")
	}
}
