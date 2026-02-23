package cli

import (
	"testing"

	"github.com/brudil/workspace/internal/github"
)

func TestIsRowVisible_NoFilters(t *testing.T) {
	m := mcModel{
		rows: []mcRow{
			{kind: rowWorktree, wt: "main", branch: "main", loaded: true},
			{kind: rowGhostPR, branch: "ghost-branch", pr: &github.PR{Author: "alice"}},
		},
	}
	if !m.isRowVisible(0) {
		t.Error("worktree row should be visible with no filters")
	}
	if !m.isRowVisible(1) {
		t.Error("ghost row should be visible with no filters")
	}
}

func TestIsRowVisible_FilterLocal(t *testing.T) {
	m := mcModel{
		activeFilters: filterLocal,
		rows: []mcRow{
			{kind: rowWorktree, wt: "main", branch: "main", loaded: true},
			{kind: rowGhostPR, branch: "ghost-branch"},
		},
	}
	if !m.isRowVisible(0) {
		t.Error("worktree row should be visible with filterLocal")
	}
	if m.isRowVisible(1) {
		t.Error("ghost row should be hidden with filterLocal")
	}
}

func TestIsRowVisible_FilterMine(t *testing.T) {
	m := mcModel{
		activeFilters: filterMine,
		ghUser:        "alice",
		rows: []mcRow{
			{kind: rowWorktree, wt: "feat", branch: "feat", pr: &github.PR{Author: "alice"}},
			{kind: rowWorktree, wt: "other", branch: "other", pr: &github.PR{Author: "bob"}},
			{kind: rowWorktree, wt: "noPR", branch: "noPR"},
		},
	}
	if !m.isRowVisible(0) {
		t.Error("row with my PR should be visible")
	}
	if m.isRowVisible(1) {
		t.Error("row with other's PR should be hidden")
	}
	if m.isRowVisible(2) {
		t.Error("row with no PR should be hidden")
	}
}

func TestIsRowVisible_FilterMine_NoUser(t *testing.T) {
	m := mcModel{
		activeFilters: filterMine,
		ghUser:        "",
		rows: []mcRow{
			{kind: rowWorktree, wt: "feat", branch: "feat", pr: &github.PR{Author: "alice"}},
		},
	}
	// When ghUser is not loaded yet, filterMine is a no-op
	if !m.isRowVisible(0) {
		t.Error("filterMine should be no-op when ghUser is empty")
	}
}

func TestIsRowVisible_FilterReviewReq(t *testing.T) {
	m := mcModel{
		activeFilters: filterReviewReq,
		rows: []mcRow{
			{kind: rowWorktree, wt: "feat", branch: "feat", pr: &github.PR{ReviewDecision: "REVIEW_REQUIRED"}},
			{kind: rowWorktree, wt: "approved", branch: "approved", pr: &github.PR{ReviewDecision: "APPROVED"}},
			{kind: rowWorktree, wt: "noPR", branch: "noPR"},
		},
	}
	if !m.isRowVisible(0) {
		t.Error("row with REVIEW_REQUIRED should be visible")
	}
	if m.isRowVisible(1) {
		t.Error("row with APPROVED should be hidden")
	}
	if m.isRowVisible(2) {
		t.Error("row with no PR should be hidden")
	}
}

func TestIsRowVisible_FilterDirty(t *testing.T) {
	m := mcModel{
		activeFilters: filterDirty,
		rows: []mcRow{
			{kind: rowWorktree, wt: "d", branch: "d", dirty: true, loaded: true},
			{kind: rowWorktree, wt: "a", branch: "a", ahead: 2, loaded: true},
			{kind: rowWorktree, wt: "c", branch: "c", loaded: true},
		},
	}
	if !m.isRowVisible(0) {
		t.Error("dirty row should be visible")
	}
	if !m.isRowVisible(1) {
		t.Error("ahead row should be visible")
	}
	if m.isRowVisible(2) {
		t.Error("clean row should be hidden")
	}
}

func TestIsRowVisible_CombinedFilters(t *testing.T) {
	m := mcModel{
		activeFilters: filterLocal | filterDirty,
		rows: []mcRow{
			{kind: rowWorktree, wt: "d", branch: "d", dirty: true, loaded: true},
			{kind: rowWorktree, wt: "c", branch: "c", loaded: true},
			{kind: rowGhostPR, branch: "ghost", pr: &github.PR{}},
		},
	}
	if !m.isRowVisible(0) {
		t.Error("local+dirty row should be visible")
	}
	if m.isRowVisible(1) {
		t.Error("local+clean row should be hidden (fails dirty)")
	}
	if m.isRowVisible(2) {
		t.Error("ghost row should be hidden (fails local)")
	}
}

func TestIsRowVisible_RepoHeader(t *testing.T) {
	m := mcModel{
		rows: []mcRow{
			{kind: rowRepoHeader, repo: "myrepo"},
		},
	}
	if m.isRowVisible(0) {
		t.Error("repo header should never be directly visible")
	}
}
