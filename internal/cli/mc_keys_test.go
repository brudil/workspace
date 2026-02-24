package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// keyMsg builds a tea.KeyMsg from a string like "q", "ctrl+c", "esc", etc.
func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

func keysMCModel() mcModel {
	ti := textinput.New()
	ti.Prompt = ""
	pi := textinput.New()
	pi.Prompt = ""

	return mcModel{
		ws: &workspace.Workspace{
			Org:           "testorg",
			DefaultBranch: "main",
			RepoNames:     []string{"repo1"},
			DisplayNames:  map[string]string{},
			RepoColors:    map[string]string{},
			Boarded:       map[string][]string{},
		},
		rows: []mcRow{
			{kind: rowRepoHeader, repo: "repo1"},
			{kind: rowWorktree, repo: "repo1", wt: "main", branch: "main", loaded: true},
			{kind: rowWorktree, repo: "repo1", wt: "feat", branch: "feat", loaded: true},
			{kind: rowGhostPR, repo: "repo1", branch: "ghost-pr", pr: &github.PR{Number: 5, HeadRefName: "ghost-pr"}},
		},
		cursor:        1,
		width:         100,
		height:        40,
		detailFor:     -1,
		confirmIdx:    -1,
		actionSpinner: -1,
		filterInput:   ti,
		paletteInput:  pi,
	}
}

func TestHandleKey_Quit(t *testing.T) {
	for _, key := range []string{"q", "ctrl+c"} {
		t.Run(key, func(t *testing.T) {
			m := keysMCModel()
			_, cmd := m.handleKey(keyMsg(key))
			if !isQuitCmd(cmd) {
				t.Errorf("expected tea.Quit on %q", key)
			}
		})
	}
}

func TestHandleKey_NavigateDown(t *testing.T) {
	m := keysMCModel()
	m.cursor = 1

	m, _ = m.handleKey(keyMsg("j"))
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2", m.cursor)
	}
}

func TestHandleKey_NavigateUp(t *testing.T) {
	m := keysMCModel()
	m.cursor = 2

	m, _ = m.handleKey(keyMsg("k"))
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}
}

func TestHandleKey_ActivateFilter(t *testing.T) {
	m := keysMCModel()

	m, _ = m.handleKey(keyMsg("/"))
	if !m.filterActive {
		t.Error("filterActive should be true after /")
	}
}

func TestHandleKey_ActivatePalette(t *testing.T) {
	m := keysMCModel()

	m, _ = m.handleKey(keyMsg(":"))
	if !m.paletteActive {
		t.Error("paletteActive should be true after :")
	}
}

func TestHandleKey_ToggleHelp(t *testing.T) {
	m := keysMCModel()

	m, _ = m.handleKey(keyMsg("?"))
	if !m.showHelp {
		t.Error("showHelp should be true after first ?")
	}

	m, _ = m.handleKey(keyMsg("?"))
	if m.showHelp {
		t.Error("showHelp should be false after second ?")
	}
}

func TestHandleKey_FilterToggles(t *testing.T) {
	tests := []struct {
		key  string
		flag filterFlag
	}{
		{"1", filterLocal},
		{"2", filterMine},
		{"3", filterReviewReq},
		{"4", filterDirty},
	}

	for _, tt := range tests {
		t.Run("key_"+tt.key, func(t *testing.T) {
			m := keysMCModel()

			// Toggle on
			m, _ = m.handleKey(keyMsg(tt.key))
			if m.activeFilters&tt.flag == 0 {
				t.Errorf("filter flag %d should be set after %q", tt.flag, tt.key)
			}

			// Toggle off
			m, _ = m.handleKey(keyMsg(tt.key))
			if m.activeFilters&tt.flag != 0 {
				t.Errorf("filter flag %d should be cleared after second %q", tt.flag, tt.key)
			}
		})
	}
}

func TestHandleKey_EscClearsFilters(t *testing.T) {
	m := keysMCModel()
	m.activeFilters = filterLocal | filterDirty
	m.filterInput.SetValue("something")

	m, _ = m.handleKey(keyMsg("esc"))

	if m.activeFilters != 0 {
		t.Errorf("activeFilters = %d, want 0", m.activeFilters)
	}
	if m.filterInput.Value() != "" {
		t.Errorf("filter text = %q, want empty", m.filterInput.Value())
	}
}

func TestHandleKey_FilterMode_Esc(t *testing.T) {
	m := keysMCModel()
	m.filterActive = true
	m.filterInput.SetValue("search")

	m, _ = m.handleKey(keyMsg("esc"))

	if m.filterActive {
		t.Error("filterActive should be false after esc")
	}
	if m.filterInput.Value() != "" {
		t.Errorf("filter text = %q, want empty (esc clears)", m.filterInput.Value())
	}
}

func TestHandleKey_FilterMode_Enter(t *testing.T) {
	m := keysMCModel()
	m.filterActive = true
	m.filterInput.SetValue("search")

	m, _ = m.handleKey(keyMsg("enter"))

	if m.filterActive {
		t.Error("filterActive should be false after enter")
	}
	if m.filterInput.Value() != "search" {
		t.Errorf("filter text = %q, want %q (enter preserves)", m.filterInput.Value(), "search")
	}
}

func TestHandleKey_ConfirmMode_Y(t *testing.T) {
	m := keysMCModel()
	m.confirmIdx = 2 // confirming delete of "feat"

	m, cmd := m.handleKey(keyMsg("y"))

	if m.confirmIdx != -1 {
		t.Errorf("confirmIdx = %d, want -1", m.confirmIdx)
	}
	if m.actionSpinner != 2 {
		t.Errorf("actionSpinner = %d, want 2", m.actionSpinner)
	}
	if cmd == nil {
		t.Error("expected a cmd for delete action")
	}
}

func TestHandleKey_ConfirmMode_N(t *testing.T) {
	m := keysMCModel()
	m.confirmIdx = 2

	m, _ = m.handleKey(keyMsg("n"))

	if m.confirmIdx != -1 {
		t.Errorf("confirmIdx = %d, want -1", m.confirmIdx)
	}
	if m.actionSpinner != -1 {
		t.Errorf("actionSpinner = %d, want -1 (no action)", m.actionSpinner)
	}
}

func TestHandleKey_D_OnWorktree(t *testing.T) {
	m := keysMCModel()
	m.cursor = 2 // "feat" worktree (not default branch)

	m, _ = m.handleKey(keyMsg("d"))

	if m.confirmIdx != 2 {
		t.Errorf("confirmIdx = %d, want 2 (delete confirmation)", m.confirmIdx)
	}
}

func TestHandleKey_Board_TogglesIsBoarded(t *testing.T) {
	m := keysMCModel()
	// Create temp dir so Board() can stat the capsule path
	repoDir := t.TempDir()
	m.ws.Root = filepath.Dir(filepath.Dir(repoDir))
	// Remap so RepoDir("repo1") points to our temp dir
	m.ws.Root = t.TempDir()
	reposDir := filepath.Join(m.ws.Root, "repos")
	os.MkdirAll(filepath.Join(reposDir, "repo1", "feat"), 0755)

	m.cursor = 2 // "feat" worktree row

	// Board
	m, _ = m.handleKey(keyMsg("b"))
	if !m.rows[2].isBoarded {
		t.Error("expected feat to be boarded after pressing b")
	}

	// Unboard
	m, _ = m.handleKey(keyMsg("b"))
	if m.rows[2].isBoarded {
		t.Error("expected feat to be unboarded after pressing b again")
	}
}

func TestHandleKey_Board_SkipsDefaultBranch(t *testing.T) {
	m := keysMCModel()
	m.ws.Root = t.TempDir()
	os.MkdirAll(filepath.Join(m.ws.Root, "repos", "repo1", "main"), 0755)

	m.cursor = 1 // "main" worktree row (DefaultBranch)

	m, _ = m.handleKey(keyMsg("b"))
	if m.rows[1].isBoarded {
		t.Error("should not be able to board the default branch")
	}
}

func TestDoGo_NonTmux_SetsJumpPath(t *testing.T) {
	m := keysMCModel()
	m.ws.Root = t.TempDir()
	os.MkdirAll(filepath.Join(m.ws.Root, "repos", "repo1", "feat"), 0755)
	m.cursor = 2 // "feat" worktree

	t.Setenv("TMUX", "")

	m, cmd := m.doGo()

	if m.jumpPath == "" {
		t.Error("jumpPath should be set when not in tmux")
	}
	if !isQuitCmd(cmd) {
		t.Error("should return tea.Quit when not in tmux")
	}
}

func TestHandleKey_D_OnGhostPR(t *testing.T) {
	m := keysMCModel()
	m.cursor = 3 // ghost PR row

	m, cmd := m.handleKey(keyMsg("d"))

	if m.actionSpinner != 3 {
		t.Errorf("actionSpinner = %d, want 3 (dock action)", m.actionSpinner)
	}
	if cmd == nil {
		t.Error("expected a cmd for dock action")
	}
}
