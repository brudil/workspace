package cli

import (
	"testing"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/lipgloss"
)

func TestFilteredRowCount(t *testing.T) {
	t.Run("no filters counts non-header rows", func(t *testing.T) {
		m := mcModel{
			rows: []mcRow{
				{kind: rowRepoHeader, repo: "repo1"},
				{kind: rowWorktree, wt: "main", branch: "main"},
				{kind: rowWorktree, wt: "feat", branch: "feat"},
				{kind: rowRepoHeader, repo: "repo2"},
				{kind: rowWorktree, wt: "dev", branch: "dev"},
			},
		}
		visible, total := m.filteredRowCount()
		if total != 3 {
			t.Errorf("total = %d, want 3", total)
		}
		if visible != 3 {
			t.Errorf("visible = %d, want 3", visible)
		}
	})

	t.Run("with filters only visible rows counted", func(t *testing.T) {
		m := mcModel{
			activeFilters: filterLocal,
			rows: []mcRow{
				{kind: rowRepoHeader, repo: "repo1"},
				{kind: rowWorktree, wt: "main", branch: "main"},
				{kind: rowGhostPR, branch: "ghost", pr: &github.PR{}},
			},
		}
		visible, total := m.filteredRowCount()
		if total != 2 {
			t.Errorf("total = %d, want 2", total)
		}
		if visible != 1 {
			t.Errorf("visible = %d, want 1", visible)
		}
	})

	t.Run("all hidden", func(t *testing.T) {
		m := mcModel{
			activeFilters: filterDirty,
			rows: []mcRow{
				{kind: rowRepoHeader, repo: "repo1"},
				{kind: rowWorktree, wt: "main", branch: "main", loaded: true},
			},
		}
		visible, _ := m.filteredRowCount()
		if visible != 0 {
			t.Errorf("visible = %d, want 0", visible)
		}
	})
}

func TestMoveCursor_Down(t *testing.T) {
	m := mcModel{
		cursor: 1,
		rows: []mcRow{
			{kind: rowRepoHeader, repo: "repo1"},
			{kind: rowWorktree, wt: "a", branch: "a"},
			{kind: rowRepoHeader, repo: "repo2"},
			{kind: rowWorktree, wt: "b", branch: "b"},
		},
	}

	// Move down from row 1 — should skip header at 2, land on 3
	m.moveCursor(1)
	if m.cursor != 3 {
		t.Errorf("cursor = %d, want 3 (should skip header)", m.cursor)
	}

	// Move down again — should stay at 3 (last row)
	m.moveCursor(1)
	if m.cursor != 3 {
		t.Errorf("cursor = %d, want 3 (should stop at last)", m.cursor)
	}
}

func TestMoveCursor_Up(t *testing.T) {
	m := mcModel{
		cursor: 3,
		rows: []mcRow{
			{kind: rowRepoHeader, repo: "repo1"},
			{kind: rowWorktree, wt: "a", branch: "a"},
			{kind: rowRepoHeader, repo: "repo2"},
			{kind: rowWorktree, wt: "b", branch: "b"},
		},
	}

	// Move up from row 3 — should skip header at 2, land on 1
	m.moveCursor(-1)
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (should skip header)", m.cursor)
	}

	// Move up again — should stay at 1 (header at 0 is not visible)
	m.moveCursor(-1)
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (should stop at first visible)", m.cursor)
	}
}

func TestMoveCursor_SkipsFilteredRows(t *testing.T) {
	m := mcModel{
		cursor:        1,
		activeFilters: filterLocal,
		rows: []mcRow{
			{kind: rowRepoHeader, repo: "repo1"},
			{kind: rowWorktree, wt: "a", branch: "a"},
			{kind: rowGhostPR, branch: "ghost", pr: &github.PR{}},
			{kind: rowWorktree, wt: "b", branch: "b"},
		},
	}

	// Move down — should skip ghost PR at 2, land on 3
	m.moveCursor(1)
	if m.cursor != 3 {
		t.Errorf("cursor = %d, want 3 (should skip ghost PR)", m.cursor)
	}
}

func TestEnsureCursorOnVisible(t *testing.T) {
	t.Run("cursor on visible row - no change", func(t *testing.T) {
		m := mcModel{
			cursor: 1,
			rows: []mcRow{
				{kind: rowRepoHeader, repo: "repo1"},
				{kind: rowWorktree, wt: "main", branch: "main"},
			},
		}
		m.ensureCursorOnVisible()
		if m.cursor != 1 {
			t.Errorf("cursor = %d, want 1", m.cursor)
		}
	})

	t.Run("cursor on header snaps to first visible", func(t *testing.T) {
		m := mcModel{
			cursor: 0,
			rows: []mcRow{
				{kind: rowRepoHeader, repo: "repo1"},
				{kind: rowWorktree, wt: "main", branch: "main"},
			},
		}
		m.ensureCursorOnVisible()
		if m.cursor != 1 {
			t.Errorf("cursor = %d, want 1", m.cursor)
		}
	})

	t.Run("all hidden - no panic", func(t *testing.T) {
		m := mcModel{
			cursor:        0,
			activeFilters: filterDirty,
			rows: []mcRow{
				{kind: rowRepoHeader, repo: "repo1"},
				{kind: rowWorktree, wt: "main", branch: "main", loaded: true},
			},
		}
		// Should not panic
		m.ensureCursorOnVisible()
	})
}

func TestRepoColorFor(t *testing.T) {
	t.Run("custom color from RepoColors", func(t *testing.T) {
		m := mcModel{
			ws: &workspace.Workspace{
				RepoColors:   map[string]string{"myrepo": "196"},
				DisplayNames: map[string]string{},
			},
			rows: []mcRow{
				{kind: rowRepoHeader, repo: "myrepo"},
				{kind: rowWorktree, wt: "main", branch: "main"},
			},
		}
		got := m.repoColorFor("myrepo")
		if got != lipgloss.Color("196") {
			t.Errorf("got %v, want 196", got)
		}
	})

	t.Run("sequential palette assignment", func(t *testing.T) {
		m := mcModel{
			ws: &workspace.Workspace{
				RepoColors:   map[string]string{},
				DisplayNames: map[string]string{},
			},
			rows: []mcRow{
				{kind: rowRepoHeader, repo: "repo1"},
				{kind: rowWorktree, wt: "main", repo: "repo1"},
				{kind: rowRepoHeader, repo: "repo2"},
				{kind: rowWorktree, wt: "main", repo: "repo2"},
			},
		}

		color1 := m.repoColorFor("repo1")
		color2 := m.repoColorFor("repo2")

		if color1 != repoPalette[0] {
			t.Errorf("repo1 color = %v, want palette[0] = %v", color1, repoPalette[0])
		}
		if color2 != repoPalette[1] {
			t.Errorf("repo2 color = %v, want palette[1] = %v", color2, repoPalette[1])
		}
	})

	t.Run("custom-color repos don't consume palette slots", func(t *testing.T) {
		m := mcModel{
			ws: &workspace.Workspace{
				RepoColors:   map[string]string{"customrepo": "196"},
				DisplayNames: map[string]string{},
			},
			rows: []mcRow{
				{kind: rowRepoHeader, repo: "customrepo"},
				{kind: rowWorktree, wt: "main", repo: "customrepo"},
				{kind: rowRepoHeader, repo: "normalrepo"},
				{kind: rowWorktree, wt: "main", repo: "normalrepo"},
			},
		}

		// normalrepo should get palette[0] since customrepo doesn't consume a slot
		got := m.repoColorFor("normalrepo")
		if got != repoPalette[0] {
			t.Errorf("normalrepo color = %v, want palette[0] = %v", got, repoPalette[0])
		}
	})
}
