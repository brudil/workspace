package cli

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- palette styles ---

const paletteSelBg = lipgloss.Color("236")

var (
	paletteBg         lipgloss.Style
	paletteDim        lipgloss.Style
	paletteMatch      lipgloss.Style
	paletteKey        lipgloss.Style
	paletteScopeStyle lipgloss.Style
	paletteLabelSel   lipgloss.Style
	paletteLabelNorm  lipgloss.Style
)

func init() {
	paletteBg = lipgloss.NewStyle().Background(paletteSelBg)
	paletteDim = lipgloss.NewStyle().Faint(true)
	paletteMatch = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	paletteKey = lipgloss.NewStyle().Faint(true).Width(3).Align(lipgloss.Right)
	paletteScopeStyle = lipgloss.NewStyle().Faint(true)
	paletteLabelSel = lipgloss.NewStyle().Bold(true)
	paletteLabelNorm = lipgloss.NewStyle()
}

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
	name  string // internal identifier (kept for test compat)
	label string // human-readable display name
	desc  string // short description
	key   string // direct keybinding hint (e.g. "⏎", "o")
	scope paletteScope
	run   func(m mcModel) (mcModel, tea.Cmd)
}

// --- registry ---

func paletteCommands() []paletteCommand {
	return []paletteCommand{
		{name: "go", label: "Go", desc: "cd into worktree", key: "⏎", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doGo() }},
		{name: "open", label: "Open in Editor", desc: "open in $EDITOR", key: "o", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doOpen() }},
		{name: "github", label: "View on GitHub", desc: "open PR in browser", key: "", scope: scopeHasPR, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doOpenPR() }},
		{name: "board", label: "Board", desc: "add to IDE workspace", key: "b", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doBoardToggle() }},
		{name: "unboard", label: "Unboard", desc: "remove from IDE workspace", key: "b", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doBoardToggle() }},
		{name: "undock", label: "Undock", desc: "remove worktree", key: "d", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doDelete() }},
		{name: "dock", label: "Dock", desc: "create worktree from PR", key: "d", scope: scopeGhostPR, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doCreateWorktree() }},
		{name: "copy-path", label: "Copy Path", desc: "copy worktree path", key: "", scope: scopeWorktree, run: paletteCmdCopyPath},
		{name: "open-repo", label: "View Repo on GitHub", desc: "open repo in browser", key: "", scope: scopeRepo, run: paletteCmdOpenRepo},
		{name: "fetch", label: "Fetch", desc: "fetch PR data for repo", key: "", scope: scopeRepo, run: paletteCmdFetch},
		{name: "filter-local", label: "Filter: Local", desc: "toggle local filter", key: "1", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) {
			m.activeFilters ^= filterLocal
			m.ensureCursorOnVisible()
			return m, nil
		}},
		{name: "filter-mine", label: "Filter: Mine", desc: "toggle my PRs filter", key: "2", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) {
			m.activeFilters ^= filterMine
			m.ensureCursorOnVisible()
			return m, nil
		}},
		{name: "filter-review", label: "Filter: Review Requested", desc: "toggle review filter", key: "3", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) {
			m.activeFilters ^= filterReviewReq
			m.ensureCursorOnVisible()
			return m, nil
		}},
		{name: "filter-dirty", label: "Filter: Dirty", desc: "toggle dirty filter", key: "4", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) {
			m.activeFilters ^= filterDirty
			m.ensureCursorOnVisible()
			return m, nil
		}},
		{name: "refresh", label: "Refresh", desc: "refresh all data", key: "r", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doRefresh() }},
		{name: "debrief", label: "Debrief", desc: "clean up merged branches", key: "", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doRefresh() }},
		{name: "fetch-all", label: "Fetch All", desc: "fetch all repos", key: "", scope: scopeAlways, run: paletteCmdFetchAll},
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
		if filter != "" && !workspace.FuzzyMatch(filter, cmd.label) {
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
	// Smart sort: context-relevant commands first, then global.
	sort.SliceStable(out, func(i, j int) bool {
		iContext := out[i].scope != scopeAlways
		jContext := out[j].scope != scopeAlways
		if iContext != jContext {
			return iContext
		}
		return false
	})
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
		m.paletteOffset = 0
		return m, nil

	case "enter":
		cmds := m.availableCommands()
		if m.paletteCursor >= 0 && m.paletteCursor < len(cmds) {
			m.paletteInput.SetValue("")
			m.paletteInput.Blur()
			m.paletteActive = false
			selected := cmds[m.paletteCursor]
			m.paletteCursor = 0
			m.paletteOffset = 0
			return selected.run(m)
		}
		return m, nil

	case "up":
		if m.paletteCursor > 0 {
			m.paletteCursor--
		}
		if m.paletteCursor < m.paletteOffset {
			m.paletteOffset = m.paletteCursor
		}
		return m, nil

	case "down":
		cmds := m.availableCommands()
		if m.paletteCursor < len(cmds)-1 {
			m.paletteCursor++
		}
		if m.paletteCursor >= m.paletteOffset+paletteMaxVisible {
			m.paletteOffset = m.paletteCursor - paletteMaxVisible + 1
		}
		return m, nil

	case "ctrl+c":
		return m, tea.Quit

	default:
		var cmd tea.Cmd
		m.paletteInput, cmd = m.paletteInput.Update(msg)
		m.paletteCursor = 0
		m.paletteOffset = 0
		return m, cmd
	}
}

// --- helpers ---

func scopeLabel(s paletteScope) string {
	switch s {
	case scopeWorktree:
		return "worktree"
	case scopeGhostPR:
		return "remote"
	case scopeHasPR:
		return "pr"
	case scopeRepo:
		return "repo"
	default:
		return ""
	}
}

func renderFuzzyHighlight(label, filter string, baseStyle, matchStyle lipgloss.Style) string {
	if filter == "" {
		return baseStyle.Render(label)
	}
	positions := workspace.FuzzyMatchPositions(filter, label)
	if positions == nil {
		return baseStyle.Render(label)
	}

	posSet := make(map[int]struct{}, len(positions))
	for _, p := range positions {
		posSet[p] = struct{}{}
	}

	runes := []rune(label)
	var b strings.Builder
	var run strings.Builder
	inMatch := false

	flush := func() {
		s := run.String()
		if s == "" {
			return
		}
		if inMatch {
			b.WriteString(matchStyle.Render(s))
		} else {
			b.WriteString(baseStyle.Render(s))
		}
		run.Reset()
	}

	for i, r := range runes {
		_, isMatch := posSet[i]
		if i == 0 {
			inMatch = isMatch
		} else if isMatch != inMatch {
			flush()
			inMatch = isMatch
		}
		run.WriteRune(r)
	}
	flush()
	return b.String()
}

// --- rendering ---

const paletteMaxVisible = 8

func (m mcModel) paletteHeight() int {
	cmds := m.availableCommands()
	total := len(cmds)

	if total == 0 {
		return 3 // border + "no matches" + input line
	}

	visible := min(total-m.paletteOffset, paletteMaxVisible)
	h := visible + 2 // +1 border, +1 prompt

	// Scroll indicators
	if m.paletteOffset > 0 {
		h++ // ▲
	}
	if m.paletteOffset+visible < total {
		h++ // ▼
	}

	return h
}

func (m mcModel) renderPalette() string {
	cmds := m.availableCommands()
	total := len(cmds)

	var b strings.Builder

	border := ui.Dim.Render(strings.Repeat("─", m.width))
	b.WriteString(border + "\n")

	if total == 0 {
		b.WriteString(ui.Dim.Render("  no matches") + "\n")
		prompt := ui.Dim.Render(":") + " " + m.paletteInput.View()
		b.WriteString(prompt)
		return b.String()
	}

	visible := min(total-m.paletteOffset, paletteMaxVisible)
	end := m.paletteOffset + visible

	// Scroll indicator: up
	if m.paletteOffset > 0 {
		b.WriteString(ui.Dim.Render("  ▲") + "\n")
	}

	filter := m.paletteInput.Value()

	for i := m.paletteOffset; i < end; i++ {
		cmd := cmds[i]
		selected := i == m.paletteCursor

		// Derive styles with selection background when needed
		currentKey := paletteKey
		currentDim := paletteDim
		currentScope := paletteScopeStyle
		currentLabelBase := paletteLabelNorm
		currentMatch := paletteMatch
		if selected {
			bg := paletteSelBg
			currentKey = paletteKey.Background(bg)
			currentDim = paletteDim.Background(bg)
			currentScope = paletteScopeStyle.Background(bg)
			currentLabelBase = paletteLabelSel.Background(bg)
			currentMatch = paletteMatch.Background(bg)
		}

		keyCol := currentKey.Render(cmd.key)
		labelCol := renderFuzzyHighlight(cmd.label, filter, currentLabelBase, currentMatch)
		descCol := currentDim.Render(cmd.desc)

		scopeCol := ""
		if sl := scopeLabel(cmd.scope); sl != "" {
			scopeCol = currentScope.Render(sl)
		}

		// Compose the line
		line := "  " + keyCol + "  " + labelCol + "  " + descCol
		if scopeCol != "" {
			lineWidth := lipgloss.Width(line)
			scopeWidth := lipgloss.Width(scopeCol)
			pad := max(m.width-lineWidth-scopeWidth-2, 1)
			line += strings.Repeat(" ", pad) + scopeCol
		}

		if selected {
			// Pad line to full width for background highlight
			lineWidth := lipgloss.Width(line)
			if lineWidth < m.width {
				line += paletteBg.Render(strings.Repeat(" ", m.width-lineWidth))
			}
		}

		b.WriteString(line + "\n")
	}

	// Scroll indicator: down
	if end < total {
		b.WriteString(ui.Dim.Render("  ▼") + "\n")
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
