package cli

import (
	"slices"
	"testing"

	"github.com/brudil/workspace/internal/github"
	tea "github.com/charmbracelet/bubbletea"
)

func TestAvailableCommands_ScopeWorktree(t *testing.T) {
	m := mcModel{
		cursor: 0,
		rows: []mcRow{
			{kind: rowWorktree, repo: "r", wt: "feat", branch: "feat", loaded: true},
		},
	}

	cmds := m.availableCommands()
	names := cmdNames(cmds)

	if !contains(names, "go") {
		t.Error("expected 'go' on worktree row")
	}
	if !contains(names, "open") {
		t.Error("expected 'open' on worktree row")
	}
	if contains(names, "dock") {
		t.Error("'dock' should not appear on worktree row")
	}
}

func TestAvailableCommands_ScopeGhostPR(t *testing.T) {
	m := mcModel{
		cursor: 0,
		rows: []mcRow{
			{kind: rowGhostPR, repo: "r", branch: "feat", pr: &github.PR{URL: "http://example.com"}},
		},
	}

	cmds := m.availableCommands()
	names := cmdNames(cmds)

	if !contains(names, "dock") {
		t.Error("expected 'dock' on ghost PR row")
	}
	if contains(names, "go") {
		t.Error("'go' should not appear on ghost PR row")
	}
	if contains(names, "open") {
		t.Error("'open' should not appear on ghost PR row")
	}
	if contains(names, "undock") {
		t.Error("'undock' should not appear on ghost PR row")
	}
}

func TestAvailableCommands_BoardUnboard(t *testing.T) {
	// Not boarded: should see board, not unboard
	m := mcModel{
		cursor: 0,
		rows: []mcRow{
			{kind: rowWorktree, repo: "r", wt: "feat", isBoarded: false},
		},
	}

	cmds := m.availableCommands()
	names := cmdNames(cmds)
	if !contains(names, "board") {
		t.Error("expected 'board' when not boarded")
	}
	if contains(names, "unboard") {
		t.Error("'unboard' should not appear when not boarded")
	}

	// Boarded: should see unboard, not board
	m.rows[0].isBoarded = true
	cmds = m.availableCommands()
	names = cmdNames(cmds)
	if !contains(names, "unboard") {
		t.Error("expected 'unboard' when boarded")
	}
	if contains(names, "board") {
		t.Error("'board' should not appear when boarded")
	}
}

func TestAvailableCommands_TextFilter(t *testing.T) {
	m := mcModel{
		cursor: 0,
		rows: []mcRow{
			{kind: rowWorktree, repo: "r", wt: "feat"},
		},
	}
	m.paletteInput.SetValue("filt")

	cmds := m.availableCommands()
	names := cmdNames(cmds)

	// Should match filter-* commands
	if !contains(names, "filter-local") {
		t.Error("expected 'filter-local' to match 'filt'")
	}
	// Should not match non-matching commands
	if contains(names, "go") {
		t.Error("'go' should not match 'filt'")
	}
	if contains(names, "refresh") {
		t.Error("'refresh' should not match 'filt'")
	}
}

func TestHandlePaletteKey_Esc(t *testing.T) {
	m := mcModel{
		paletteActive: true,
		paletteCursor: 2,
	}
	m.paletteInput.SetValue("test")

	m, _ = m.handlePaletteKey(tea.KeyMsg{Type: tea.KeyEsc})

	if m.paletteActive {
		t.Error("palette should be deactivated on esc")
	}
	if m.paletteInput.Value() != "" {
		t.Error("palette input should be cleared on esc")
	}
	if m.paletteCursor != 0 {
		t.Error("palette cursor should be reset on esc")
	}
}

func TestHandlePaletteKey_Navigation(t *testing.T) {
	m := mcModel{
		paletteActive: true,
		paletteCursor: 0,
		cursor:        0,
		rows: []mcRow{
			{kind: rowWorktree, repo: "r", wt: "feat"},
		},
	}

	// Down moves cursor
	m, _ = m.handlePaletteKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.paletteCursor != 1 {
		t.Errorf("expected paletteCursor=1 after down, got %d", m.paletteCursor)
	}

	// Up moves cursor back
	m, _ = m.handlePaletteKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.paletteCursor != 0 {
		t.Errorf("expected paletteCursor=0 after up, got %d", m.paletteCursor)
	}

	// Up at 0 stays at 0
	m, _ = m.handlePaletteKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.paletteCursor != 0 {
		t.Errorf("expected paletteCursor=0 at top boundary, got %d", m.paletteCursor)
	}
}

// --- helpers ---

func cmdNames(cmds []paletteCommand) []string {
	names := make([]string, len(cmds))
	for i, c := range cmds {
		names[i] = c.name
	}
	return names
}

func contains(ss []string, s string) bool {
	return slices.Contains(ss, s)
}
