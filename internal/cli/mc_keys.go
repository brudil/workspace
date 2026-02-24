package cli

import (
	"os"
	"os/exec"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/ide"
	tmuxpkg "github.com/brudil/workspace/internal/tmux"
	"github.com/brudil/workspace/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

func (m mcModel) handleKey(msg tea.KeyMsg) (mcModel, tea.Cmd) {
	// Filter mode: route keys to textinput
	if m.filterActive {
		switch msg.String() {
		case "esc":
			m.filterInput.SetValue("")
			m.filterInput.Blur()
			m.filterActive = false
			m.activeFilters = 0
			m.ensureCursorOnVisible()
			return m, nil
		case "enter":
			m.filterInput.Blur()
			m.filterActive = false
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		default:
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			m.ensureCursorOnVisible()
			return m, cmd
		}
	}

	if m.paletteActive {
		return m.handlePaletteKey(msg)
	}

	if m.confirmIdx >= 0 {
		switch msg.String() {
		case "y":
			row := m.rows[m.confirmIdx]
			idx := m.confirmIdx
			m.confirmIdx = -1
			m.actionSpinner = idx
			repo := row.repo
			branch := row.wt
			ws := m.ws
			windowName := tmuxpkg.WindowName(m.ws.DisplayNameFor(repo), branch)
			return m, func() tea.Msg {
				if tmuxpkg.InTmux() {
					windows := tmuxpkg.ListWindows()
					if id, ok := windows[windowName]; ok {
						_ = tmuxpkg.KillWindow(id)
					}
				}
				err := ws.RemoveWorktree(repo, branch)
				return mcWorktreeDeletedMsg{rowIdx: idx, repo: repo, branch: branch, err: err}
			}
		case "n", "esc":
			m.confirmIdx = -1
			return m, nil
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "/":
		m.filterActive = true
		return m, m.filterInput.Focus()

	case ":":
		m.paletteActive = true
		m.paletteCursor = 0
		return m, m.paletteInput.Focus()

	case "1":
		m.activeFilters ^= filterLocal
		m.ensureCursorOnVisible()
	case "2":
		m.activeFilters ^= filterMine
		m.ensureCursorOnVisible()
	case "3":
		m.activeFilters ^= filterReviewReq
		m.ensureCursorOnVisible()
	case "4":
		m.activeFilters ^= filterDirty
		m.ensureCursorOnVisible()

	case "esc":
		m.activeFilters = 0
		m.filterInput.SetValue("")
		m.filterInput.Blur()
		m.ensureCursorOnVisible()

	case "j", "down":
		m.moveCursor(1)
		m.ensureCursorVisible()

	case "k", "up":
		m.moveCursor(-1)
		m.ensureCursorVisible()

	case "J", "shift+down":
		m.syncDetailContent()
		m.detailVP.LineDown(3)
	case "K", "shift+up":
		m.syncDetailContent()
		m.detailVP.LineUp(3)

	case "left", "h":
		if m.isOnGround() {
			m.ensureCursorOnVisible()
			m.ensureCursorVisible()
		}

	case "right", "l":
		return m.doSelectGround()

	case "enter":
		return m.doGo()
	case "o":
		return m.doOpen()
	case "b":
		return m.doBoardToggle()
	case "d":
		row := m.rows[m.cursor]
		if row.kind == rowGhostPR {
			return m.doCreateWorktree()
		}
		return m.doDelete()
	case "r":
		return m.doRefresh()
	case "?":
		m.showHelp = !m.showHelp
	}

	return m, nil
}

// --- shared action helpers ---

func (m mcModel) doSelectGround() (mcModel, tea.Cmd) {
	row := m.rows[m.cursor]
	repo := row.repo
	if repo == "" {
		return m, nil
	}
	for i, r := range m.rows {
		if r.kind == rowWorktree && r.repo == repo && r.wt == workspace.GroundDir {
			m.cursor = i
			return m, nil
		}
	}
	return m, nil
}

func (m mcModel) doGo() (mcModel, tea.Cmd) {
	path := m.selectedWorktreePath()
	if path == "" {
		return m, nil
	}
	if tmuxpkg.InTmux() {
		row := m.rows[m.cursor]
		name := tmuxpkg.WindowName(m.ws.DisplayNameFor(row.repo), row.wt)
		windows := tmuxpkg.ListWindows()
		if id, ok := windows[name]; ok {
			panes := tmuxpkg.ListPanes(id)
			if paneID, idle := tmuxpkg.FindIdlePane(panes); idle {
				_ = tmuxpkg.SelectWindow(id)
				_ = tmuxpkg.SelectPane(paneID)
			} else {
				_ = tmuxpkg.SelectWindow(id)
				_ = tmuxpkg.SplitWindow(id, path)
			}
		} else {
			_ = tmuxpkg.NewWindow(name, path)
		}
		return m, queryTmuxWindows()
	}
	m.jumpPath = path
	return m, tea.Quit
}

func (m mcModel) doOpen() (mcModel, tea.Cmd) {
	path := m.selectedWorktreePath()
	if path == "" {
		return m, nil
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}
	if tmuxpkg.InTmux() {
		row := m.rows[m.cursor]
		name := tmuxpkg.WindowName(m.ws.DisplayNameFor(row.repo), row.wt)
		windows := tmuxpkg.ListWindows()
		if id, ok := windows[name]; ok {
			_ = tmuxpkg.SelectWindow(id)
			_ = exec.Command("tmux", "split-window", "-t", id, "-c", path, editor, path).Start()
		} else {
			_ = exec.Command("tmux", "new-window", "-n", name, "-c", path, editor, path).Start()
		}
		return m, queryTmuxWindows()
	}
	c := exec.Command(editor, path)
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		return tea.ClearScreen()
	})
}

func (m mcModel) doOpenPR() (mcModel, tea.Cmd) {
	row := m.rows[m.cursor]
	if row.pr != nil && row.pr.URL != "" {
		_ = exec.Command("open", row.pr.URL).Start()
	}
	return m, nil
}

func (m mcModel) doBoardToggle() (mcModel, tea.Cmd) {
	row := m.rows[m.cursor]
	if row.kind != rowWorktree {
		return m, nil
	}
	if row.wt == m.ws.DefaultBranch || row.wt == workspace.GroundDir {
		return m, nil
	}

	var err error
	if row.isBoarded {
		err = m.ws.Unboard(row.repo, row.wt)
	} else {
		err = m.ws.Board(row.repo, row.wt)
	}
	if err != nil {
		return m, nil
	}

	config.SaveBoarded(m.ws.Root, m.ws.Boarded)
	ide.Regenerate(m.ws.Root, m.ws.Boarded, m.ws.DisplayNames, m.ws.Org)
	for i := range m.rows {
		if m.rows[i].kind == rowWorktree && m.rows[i].repo == row.repo {
			m.rows[i].isBoarded = m.ws.IsBoarded(m.rows[i].repo, m.rows[i].wt)
		}
	}
	return m, nil
}

func (m mcModel) doDelete() (mcModel, tea.Cmd) {
	row := m.rows[m.cursor]
	if row.kind != rowWorktree || row.wt == m.ws.DefaultBranch || row.wt == workspace.GroundDir {
		return m, nil
	}
	m.confirmIdx = m.cursor
	return m, nil
}

func (m mcModel) doCreateWorktree() (mcModel, tea.Cmd) {
	row := m.rows[m.cursor]
	if row.kind != rowGhostPR || m.actionSpinner >= 0 {
		return m, nil
	}
	m.actionSpinner = m.cursor
	idx := m.cursor
	repo := row.repo
	branch := row.branch
	ws := m.ws
	return m, func() tea.Msg {
		capsule, err := ws.DockWorktree(repo, branch)
		return mcWorktreeCreatedMsg{rowIdx: idx, repo: repo, branch: branch, capsule: capsule, err: err}
	}
}

func (m mcModel) doRefresh() (mcModel, tea.Cmd) {
	capsules := m.ws.FindAllCapsules(14, "")

	if tmuxpkg.InTmux() {
		windows := tmuxpkg.ListWindows()
		for _, c := range capsules {
			if (c.Merged || c.Inactive) && !c.Dirty {
				name := tmuxpkg.WindowName(m.ws.DisplayNameFor(c.Repo), c.Name)
				if id, ok := windows[name]; ok {
					_ = tmuxpkg.KillWindow(id)
				}
			}
		}
	}

	boardChanged := false
	for _, c := range capsules {
		if (c.Merged || c.Inactive) && !c.Dirty {
			if m.ws.IsBoarded(c.Repo, c.Name) {
				_ = m.ws.Unboard(c.Repo, c.Name)
				boardChanged = true
			}
			_ = m.ws.RemoveWorktree(c.Repo, c.Name)
		}
	}
	if boardChanged {
		config.SaveBoarded(m.ws.Root, m.ws.Boarded)
		ide.Regenerate(m.ws.Root, m.ws.Boarded, m.ws.DisplayNames, m.ws.Org)
	}
	return m.rebuildModel()
}

func (m mcModel) rebuildModel() (mcModel, tea.Cmd) {
	m2 := newMCModel(m.ws, m.gh, m.cwd)
	m2.width = m.width
	m2.height = m.height
	m2.listVP = m.listVP
	m2.detailVP = m.detailVP
	m2.filterInput = m.filterInput
	m2.filterActive = m.filterActive
	m2.activeFilters = m.activeFilters
	m2.ghUser = m.ghUser
	return m2, m2.Init()
}
