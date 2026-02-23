package cli

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- message types ---

type mcFetchMsg struct {
	repo string
	err  error
}

type mcFetchAllMsg struct {
	err error
}

// --- scope ---

type paletteScope int

const (
	scopeAlways   paletteScope = iota
	scopeWorktree              // row is a docked worktree
	scopeGhostPR               // row is a ghost PR (no local worktree)
	scopeHasPR                 // row has a PR (worktree or ghost)
	scopeRepo                  // row belongs to a specific repo (worktree or ghost)
)

// --- command ---

type paletteCommand struct {
	name  string
	scope paletteScope
	run   func(m mcModel) (mcModel, tea.Cmd)
}

// --- registry ---

func paletteCommands() []paletteCommand {
	return []paletteCommand{
		{name: "go", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doGo() }},
		{name: "open", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doOpen() }},
		{name: "github", scope: scopeHasPR, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doOpenPR() }},
		{name: "board", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doBoard() }},
		{name: "unboard", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doUnboard() }},
		{name: "undock", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doDelete() }},
		{name: "dock", scope: scopeGhostPR, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doCreateWorktree() }},
		{name: "copy-path", scope: scopeWorktree, run: paletteCmdCopyPath},
		{name: "open-repo", scope: scopeRepo, run: paletteCmdOpenRepo},
		{name: "fetch", scope: scopeRepo, run: paletteCmdFetch},
		{name: "filter-local", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) {
			m.activeFilters ^= filterLocal
			m.ensureCursorOnVisible()
			return m, nil
		}},
		{name: "filter-mine", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) {
			m.activeFilters ^= filterMine
			m.ensureCursorOnVisible()
			return m, nil
		}},
		{name: "filter-review", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) {
			m.activeFilters ^= filterReviewReq
			m.ensureCursorOnVisible()
			return m, nil
		}},
		{name: "filter-dirty", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) {
			m.activeFilters ^= filterDirty
			m.ensureCursorOnVisible()
			return m, nil
		}},
		{name: "refresh", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doRefresh() }},
		{name: "debrief", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doRefresh() }},
		{name: "fetch-all", scope: scopeAlways, run: paletteCmdFetchAll},
	}
}

// --- filtering ---

func (m mcModel) availableCommands() []paletteCommand {
	all := paletteCommands()
	filter := m.paletteInput.Value()

	var row mcRow
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		row = m.rows[m.cursor]
	}

	var out []paletteCommand
	for _, cmd := range all {
		// text filter
		if filter != "" && !workspace.FuzzyMatch(filter, cmd.name) {
			continue
		}
		// scope filter
		switch cmd.scope {
		case scopeWorktree:
			if row.kind != rowWorktree {
				continue
			}
		case scopeGhostPR:
			if row.kind != rowGhostPR {
				continue
			}
		case scopeHasPR:
			if row.pr == nil {
				continue
			}
		case scopeRepo:
			if row.kind == rowRepoHeader {
				continue
			}
		}
		// board/unboard visibility
		if cmd.name == "board" && row.isBoarded {
			continue
		}
		if cmd.name == "unboard" && !row.isBoarded {
			continue
		}
		out = append(out, cmd)
	}
	return out
}

// --- key handling ---

func (m mcModel) handlePaletteKey(msg tea.KeyMsg) (mcModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.paletteInput.SetValue("")
		m.paletteInput.Blur()
		m.paletteActive = false
		m.paletteCursor = 0
		return m, nil

	case "enter":
		cmds := m.availableCommands()
		if m.paletteCursor >= 0 && m.paletteCursor < len(cmds) {
			m.paletteInput.SetValue("")
			m.paletteInput.Blur()
			m.paletteActive = false
			selected := cmds[m.paletteCursor]
			m.paletteCursor = 0
			return selected.run(m)
		}
		return m, nil

	case "up":
		if m.paletteCursor > 0 {
			m.paletteCursor--
		}
		return m, nil

	case "down":
		cmds := m.availableCommands()
		if m.paletteCursor < len(cmds)-1 {
			m.paletteCursor++
		}
		return m, nil

	case "ctrl+c":
		return m, tea.Quit

	default:
		var cmd tea.Cmd
		m.paletteInput, cmd = m.paletteInput.Update(msg)
		// Reset cursor when text changes
		m.paletteCursor = 0
		return m, cmd
	}
}

// --- rendering ---

const paletteMaxVisible = 8

func (m mcModel) paletteHeight() int {
	cmds := m.availableCommands()
	visible := min(len(cmds), paletteMaxVisible)
	return visible + 2 // commands + border + input line
}

func (m mcModel) renderPalette() string {
	cmds := m.availableCommands()
	visible := min(len(cmds), paletteMaxVisible)

	var b strings.Builder

	border := ui.Dim.Render(strings.Repeat("─", m.width))
	b.WriteString(border + "\n")

	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true)

	for i := 0; i < visible; i++ {
		cmd := cmds[i]
		if i == m.paletteCursor {
			b.WriteString("  " + selectedStyle.Render(cmd.name))
		} else {
			b.WriteString("  " + cmd.name)
		}
		b.WriteString("\n")
	}

	prompt := ui.Dim.Render(":") + " " + m.paletteInput.View()
	b.WriteString(prompt)

	return b.String()
}

// --- command implementations ---

func paletteCmdCopyPath(m mcModel) (mcModel, tea.Cmd) {
	path := m.selectedWorktreePath()
	if path == "" {
		return m, nil
	}
	c := exec.Command("pbcopy")
	c.Stdin = strings.NewReader(path)
	_ = c.Run()
	return m, nil
}

func paletteCmdOpenRepo(m mcModel) (mcModel, tea.Cmd) {
	row := m.rows[m.cursor]
	if row.repo == "" {
		return m, nil
	}
	url := fmt.Sprintf("https://github.com/%s/%s", m.ws.Org, row.repo)
	_ = exec.Command("open", url).Start()
	return m, nil
}

func paletteCmdFetch(m mcModel) (mcModel, tea.Cmd) {
	row := m.rows[m.cursor]
	repo := row.repo
	ws := m.ws
	return m, func() tea.Msg {
		result := ws.FetchRepo(repo)
		return mcFetchMsg{repo: repo, err: result.Err}
	}
}

func paletteCmdFetchAll(m mcModel) (mcModel, tea.Cmd) {
	ws := m.ws
	return m, func() tea.Msg {
		results := ws.FetchAll()
		for _, r := range results {
			if r.Err != nil {
				return mcFetchAllMsg{err: r.Err}
			}
		}
		return mcFetchAllMsg{}
	}
}
