package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/x/ansi"
)

const (
	deckSidebarWidth    = 34
	deckMinSidebarWidth = 72 // below this terminal width the sidebar hides itself
	deckChromeLines     = 3  // header, rule, footer
)

var (
	deckSelBg   = lipgloss.Color("236")
	deckDivider = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
)

func (m deckModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "Deck"
	// With a crew terminal focused, put the real cursor where the agent's is.
	if r, ok := m.focusedRun(); ok && !m.showHelp && m.mode == deckModeNormal && r.scroll == 0 {
		if x, y, visible := r.term.Cursor(); visible {
			v.Cursor = tea.NewCursor(x, y+2) // below header and rule
		}
	}
	return v
}

func (m deckModel) sidebarShown() bool {
	return !m.sidebarHidden && m.width >= deckMinSidebarWidth
}

func (m deckModel) mainWidth() int {
	if m.sidebarShown() {
		return m.width - deckSidebarWidth - 1
	}
	return m.width
}

func (m deckModel) bodyHeight() int { return max(m.height-deckChromeLines, 1) }

func (m deckModel) render() string {
	if m.width == 0 {
		return "loading..."
	}
	if m.showHelp {
		return m.renderHelp()
	}

	header := m.renderHeader()
	bodyH := m.bodyHeight()

	var main string
	if r, ok := m.shownRun(); ok {
		main = renderTerm(r.term, r.scroll, m.mainWidth(), bodyH)
	} else {
		m.syncMain()
		main = m.mainVP.View()
	}

	body := main
	if m.sidebarShown() {
		divider := deckDivider.Render(strings.TrimSuffix(strings.Repeat("│\n", bodyH), "\n"))
		side := m.renderSidebar(deckSidebarWidth, bodyH)
		body = lipgloss.JoinHorizontal(lipgloss.Top, main, divider, side)
	}
	return header + "\n" + body + "\n" + m.renderFooter()
}

func (m deckModel) renderHeader() string {
	title := " " + ui.Bold.Render(m.ws.Title()) + ui.Dim.Render(" deck")
	if r, ok := m.shownRun(); ok {
		label := fmt.Sprintf("  %s · crew %s · %s", r.slug, r.crewID, r.harness)
		if t := r.term.Title(); t != "" {
			label += " · " + t
		}
		if r.scroll > 0 {
			label += fmt.Sprintf(" · ↑%d lines", r.scroll)
		}
		if m.focusTerm {
			title += ui.Green.Render(label)
		} else {
			title += ui.Dim.Render(label)
		}
	}
	right := ""
	if m.loaded {
		right = ui.Dim.Render(m.counts()) + " "
	}
	pad := max(m.width-lipgloss.Width(title)-lipgloss.Width(right), 1)
	return title + strings.Repeat(" ", pad) + right + "\n" + ui.Dim.Render(strings.Repeat("─", m.width))
}

func (m deckModel) renderFooter() string {
	switch m.mode {
	case deckModeNewTitle:
		if m.newBriefing {
			return " " + ui.Bold.Render("What's the mission?") + " " + m.input.View()
		}
		return " " + ui.Bold.Render("Mission title:") + " " + m.input.View()
	case deckModeConfirm:
		return " " + ui.Orange.Render(m.confirm.prompt) + ui.Dim.Render("  y/n")
	case deckModePick:
		return ui.Dim.Render(" j/k move  ⏎ choose  esc cancel")
	}
	if m.focusTerm {
		if m.scrollMode {
			return " " + ui.Orange.Render("history") + ui.Dim.Render(ansi.Truncate(" k/j line · ctrl+u/d half page · g top · G/esc back to live", m.width-9, "…"))
		}
		if m.prefix {
			return " " + ui.Orange.Render(deckPrefixKey) + ui.Dim.Render(ansi.Truncate(" then: tab sidebar · [ history · z zoom · n/p next/prev crew · s sidebar · S stop · q quit · ? help · "+deckPrefixKey+" send it", m.width-len(deckPrefixKey)-2, "…"))
		}
		if m.flash != "" {
			return " " + m.flash
		}
		return ui.Dim.Render(ansi.Truncate(" keys go to the crew · "+deckPrefixKey+" for deck commands (tab back to the sidebar)", m.width, "…"))
	}
	if m.flash != "" {
		return " " + m.flash
	}
	hints := " j/k move  ⏎ crew  tab focus  n new  e brief  a attach  p park  R resume  S stop  ? help  q quit"
	return ui.Dim.Render(ansi.Truncate(hints, m.width, "…"))
}

// --- main pane ---

// syncMain puts the current main-pane content into the viewport, keeping the
// scroll position unless the pane now shows something else.
func (m *deckModel) syncMain() {
	key, content := m.mainContent(m.mainWidth())
	m.mainVP.SetWidth(m.mainWidth())
	m.mainVP.SetHeight(m.bodyHeight())
	y := m.mainVP.YOffset()
	m.mainVP.SetContent(content)
	if key == m.mainKey {
		m.mainVP.SetYOffset(y)
	} else {
		m.mainVP.GotoTop()
		m.mainKey = key
	}
}

func (m deckModel) mainContent(width int) (string, string) {
	const indent = "  "
	inner := max(width-2*len(indent), 10)

	if m.mode == deckModePick && m.pick != nil {
		return "pick", m.renderPick(indent)
	}
	if !m.loaded {
		return "loading", "\n" + indent + ui.Dim.Render("Loading missions…")
	}
	if m.err != nil {
		return "error", "\n" + indent + ui.Red.Render("Couldn't load missions: "+m.err.Error())
	}

	r, ok := m.selected()
	if !ok {
		return "empty", m.renderWelcome(indent)
	}
	switch r.kind {
	case deckRowMission, deckRowCapsule:
		dm := m.data.Missions[r.mission]
		return "m:" + dm.M.Slug, m.renderMission(dm, indent, inner)
	case deckRowLoose:
		c := m.data.Loose[r.capsule]
		return "l:" + c.Repo + "/" + c.Name, m.renderLoose(c, indent)
	default:
		return "loose", "\n" + indent + fmt.Sprintf("%d capsules belong to no mission.", len(m.data.Loose)) +
			"\n" + indent + ui.Dim.Render("⏎ shows them; select one and press a to attach it to a mission.")
	}
}

func (m deckModel) renderWelcome(indent string) string {
	var b strings.Builder
	b.WriteString("\n" + indent + ui.Bold.Render("No missions yet") + "\n\n")
	b.WriteString(indent + "A mission is a unit of work: a brief, the capsules that carry it out,\n")
	b.WriteString(indent + "and a log of what happened.\n\n")
	b.WriteString(indent + "Press " + ui.Bold.Render("n") + " to create one.\n")
	return b.String()
}

func (m deckModel) renderMission(dm deckMission, indent string, inner int) string {
	var b strings.Builder
	b.WriteString("\n" + indent + ui.Bold.Render(dm.M.Title) + "\n")
	b.WriteString(indent + ui.TagDim.Render(dm.M.Slug) + "  " + stateStyle(dm.State).Render(string(dm.State)) + "\n")
	b.WriteString(indent + m.crewSummary(dm) + "\n")
	if dm.State == mission.Briefing {
		b.WriteString("\n" + indent + ui.Orange.Render("Being briefed") + ui.Dim.Render(" · an agent is turning the request into a brief and capsules; it finishes with ws mission ready") + "\n")
		if dm.M.Request != "" {
			b.WriteString(indent + ui.Dim.Render("Requested: ") + strings.ReplaceAll(dm.M.Request, "\n", " ") + "\n")
		}
	}

	b.WriteString("\n" + indent + ui.Bold.Render("Capsules") + "\n")
	if len(dm.Capsules) == 0 {
		b.WriteString(indent + "  " + ui.Dim.Render("none yet · a attaches a loose capsule · WS_MISSION="+dm.M.Slug+" ws lift <repo> <name>") + "\n")
	}
	nameW := 0
	for _, c := range dm.Capsules {
		nameW = max(nameW, lipgloss.Width(m.ws.FormatRepoName(c.Repo)))
	}
	for _, c := range dm.Capsules {
		repo := m.ws.FormatRepoName(c.Repo)
		line := indent + "  " + repo + strings.Repeat(" ", nameW-lipgloss.Width(repo)) + "  " + ui.TagDim.Render(c.Name)
		if badges := deckBadges(dm.Git[mission.Capsule{Repo: c.Repo, Name: c.Name}], c.Exists, c.PR, c.PRS); badges != "" {
			line += "  " + badges
		}
		b.WriteString(line + "\n")
		if c.Exists {
			b.WriteString(indent + "  " + ui.Dim.Render(c.Path) + "\n")
		}
	}

	b.WriteString("\n" + indent + ui.Bold.Render("Brief") + ui.Dim.Render("  e to edit") + "\n")
	if strings.TrimSpace(dm.Brief) == "" {
		b.WriteString(indent + "  " + ui.Dim.Render("(empty)") + "\n")
	} else {
		for line := range strings.SplitSeq(m.md.render(dm.Brief, inner), "\n") {
			b.WriteString(indent + line + "\n")
		}
	}

	if len(dm.Events) > 0 {
		b.WriteString("\n" + indent + ui.Bold.Render("Log") + "\n")
		for _, e := range dm.Events {
			b.WriteString(indent + "  " + formatEventLine(e, false) + "\n")
		}
	}
	return b.String()
}

func (m deckModel) renderLoose(c deckCapsule, indent string) string {
	var b strings.Builder
	b.WriteString("\n" + indent + m.ws.FormatRepoName(c.Repo) + "  " + ui.TagDim.Render(c.Name) + "\n")
	var facts []string
	if c.Git.Branch != "" && c.Git.Branch != c.Name {
		facts = append(facts, "branch "+c.Git.Branch)
	}
	if badges := deckBadges(c.Git, true, c.PR, prStateOf(c.PR)); badges != "" {
		facts = append(facts, badges)
	}
	if len(facts) > 0 {
		b.WriteString(indent + strings.Join(facts, "  ") + "\n")
	}
	b.WriteString(indent + ui.Dim.Render(c.Path) + "\n\n")
	b.WriteString(indent + "Not part of a mission. Press " + ui.Bold.Render("a") + " to attach it to one.\n")
	return b.String()
}

func prStateOf(pr *github.PR) mission.PRState {
	if pr == nil {
		return mission.PRNone
	}
	return mission.PROpen
}

func (m deckModel) renderPick(indent string) string {
	var b strings.Builder
	b.WriteString("\n" + indent + ui.Bold.Render(m.pick.title) + "\n\n")
	for i, item := range m.pick.items {
		if i == m.pick.cursor {
			b.WriteString(indent + ui.Orange.Render("▸ ") + ui.Bold.Render(item) + "\n")
		} else {
			b.WriteString(indent + "  " + item + "\n")
		}
	}
	return b.String()
}

// deckBadges renders a capsule's git and PR state compactly.
func deckBadges(st workspace.WorktreeStatus, exists bool, pr *github.PR, prs mission.PRState) string {
	var parts []string
	if !exists {
		parts = append(parts, ui.Dim.Render("burned"))
	} else {
		if st.Dirty {
			parts = append(parts, ui.Orange.Render("●"))
		}
		if st.Ahead > 0 {
			parts = append(parts, ui.Blue.Render(fmt.Sprintf("↑%d", st.Ahead)))
		}
		if st.Behind > 0 {
			parts = append(parts, ui.Dim.Render(fmt.Sprintf("↓%d", st.Behind)))
		}
	}
	if pr != nil {
		label := fmt.Sprintf("#%d", pr.Number)
		if prs == mission.PRMerged {
			parts = append(parts, ui.Green.Render(label+" merged"))
		} else {
			parts = append(parts, ui.Blue.Render(label))
		}
	}
	return strings.Join(parts, " ")
}

// --- sidebar ---

// sidebarLines renders every sidebar line and, for each, the row index it
// shows (-1 for headings and spacing). It is shared by rendering and mouse
// hit-testing so the two can't disagree.
func (m deckModel) sidebarLines(width int) ([]string, []int) {
	lines := []string{ui.Bold.Render(" MISSIONS")}
	rowOf := []int{-1}
	add := func(s string, row int) {
		lines = append(lines, s)
		rowOf = append(rowOf, row)
	}

	if m.loaded && len(m.data.Missions) == 0 {
		add(ui.Dim.Render("   none yet · n to create"), -1)
	}

	firstMission := true
	for i, r := range m.rows {
		selected := i == m.cursor && m.mode == deckModeNormal
		var line string
		switch r.kind {
		case deckRowMission:
			if !firstMission {
				add("", -1)
			}
			firstMission = false
			dm := m.data.Missions[r.mission]
			marker := "  "
			if selected {
				marker = ui.Orange.Render("▸ ")
			}
			state := stateStyle(dm.State).Render(string(dm.State))
			if n := len(m.reg.live(dm.M.Slug)); n > 0 {
				state = ui.Green.Render(fmt.Sprintf("●%d", n)) + " " + state
			} else if m.launching[dm.M.Slug] {
				state = ui.Dim.Render("…") + " " + state
			}
			left := " " + marker + ui.Bold.Render(dm.M.Slug)
			avail := width - lipgloss.Width(state) - 2
			left = ansi.Truncate(left, avail, "…")
			line = left + strings.Repeat(" ", max(avail-lipgloss.Width(left), 1)) + state + " "
		case deckRowCapsule:
			dm := m.data.Missions[r.mission]
			c := dm.Capsules[r.capsule]
			line = "     " + c.Repo + "/" + c.Name
			if badges := deckBadges(dm.Git[mission.Capsule{Repo: c.Repo, Name: c.Name}], c.Exists, c.PR, c.PRS); badges != "" {
				line += " " + badges
			}
		case deckRowCrew:
			marker := "   "
			if selected && m.focusTerm {
				marker = ui.Green.Render(" ▸ ")
			}
			label := r.run.harness + ui.Dim.Render(" "+r.run.capsule)
			if r.run.role == mission.RoleBriefing {
				label = ui.Orange.Render("briefing") + ui.Dim.Render(" "+r.run.harness)
			}
			line = "  " + marker + ui.Green.Render("●"+r.run.crewID) + " " + label
		case deckRowLooseHeader:
			add("", -1)
			arrow := "▸"
			if m.looseOpen {
				arrow = "▾"
			}
			line = ui.Dim.Render(fmt.Sprintf(" %s loose capsules (%d)", arrow, len(m.data.Loose)))
		case deckRowLoose:
			c := m.data.Loose[r.capsule]
			line = "     " + c.Repo + "/" + c.Name
			if badges := deckBadges(c.Git, true, c.PR, prStateOf(c.PR)); badges != "" {
				line += " " + badges
			}
		}
		line = ansi.Truncate(line, width, "…")
		if selected {
			line = lipgloss.NewStyle().Background(deckSelBg).Width(width).Render(line)
		}
		add(line, i)
	}
	return lines, rowOf
}

// sidebarOffset scrolls the sidebar so the cursor's line stays visible.
func sidebarOffset(rowOf []int, cursor, height int) int {
	cursorLine := 0
	for i, r := range rowOf {
		if r == cursor {
			cursorLine = i
			break
		}
	}
	return max(cursorLine-height+1, 0)
}

func (m deckModel) renderSidebar(width, height int) string {
	lines, rowOf := m.sidebarLines(width)
	off := sidebarOffset(rowOf, m.cursor, height)
	lines = lines[off:]
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

// sidebarRowAt maps a mouse position to a sidebar row.
func (m deckModel) sidebarRowAt(x, y int) (int, bool) {
	if !m.sidebarShown() || x <= m.mainWidth() {
		return 0, false
	}
	_, rowOf := m.sidebarLines(deckSidebarWidth)
	line := y - 2 + sidebarOffset(rowOf, m.cursor, m.bodyHeight()) // header + rule
	if line < 0 || line >= len(rowOf) || rowOf[line] < 0 {
		return 0, false
	}
	return rowOf[line], true
}

// --- help ---

func (m deckModel) renderHelp() string {
	help := []struct{ key, desc string }{
		{"j/k ↑/↓", "Move through missions and capsules"},
		{"⏎", "Launch crew (" + harness.Default() + ") on the mission, or focus its live crew"},
		{"L", "Launch crew with a chosen harness"},
		{"R", "Resume the mission's last crew conversation"},
		{"tab", "Focus the mission's live crew terminal"},
		{deckPrefixKey, "In a crew terminal: then tab returns here, s hides the sidebar, S stops crew"},
		{"S", "Stop the mission's live crew"},
		{"J/K", "Scroll the main pane (also ctrl+d/u, mouse wheel)"},
		{"n", "New mission: say what it is; a briefing agent writes the brief and lifts capsules"},
		{"N", "New mission you name and brief yourself (in $EDITOR)"},
		{"e", "Edit the selected mission's brief in $EDITOR"},
		{"a", "Attach: a loose capsule to this mission, or this loose capsule to a mission"},
		{"d", "Detach the selected capsule from its mission"},
		{"p", "Park / reopen the selected mission"},
		{"x", "Archive / reopen the selected mission"},
		{"A", "Show or hide archived missions"},
		{"⏎ (loose)", "Expand loose capsules"},
		{"s", "Toggle the sidebar"},
		{"r", "Refresh (including PRs)"},
		{"?", "Toggle this help"},
		{"q", "Quit"},
	}
	var b strings.Builder
	b.WriteString(ui.Bold.Render("  Deck") + "\n\n")
	for _, h := range help {
		b.WriteString("  " + lipgloss.NewStyle().Bold(true).Width(10).Render(h.key) + " " + h.desc + "\n")
	}
	b.WriteString("\n" + ui.Dim.Render("  Press ? to close"))
	return b.String()
}

// --- markdown ---

// markdownCache memoises glamour output; rendering on every frame is slow.
type markdownCache struct {
	entries map[string]string
}

func newMarkdownCache() *markdownCache { return &markdownCache{entries: map[string]string{}} }

const markdownCacheMax = 64

func (c *markdownCache) render(md string, width int) string {
	key := fmt.Sprintf("%d\x00%s", width, md)
	if out, ok := c.entries[key]; ok {
		return out
	}
	style := styles.DarkStyleConfig
	style.Document.Margin = uintPtr(0)
	out := strings.TrimSpace(md)
	if r, err := glamour.NewTermRenderer(glamour.WithStyles(style), glamour.WithWordWrap(width)); err == nil {
		if rendered, err := r.Render(md); err == nil {
			out = strings.Trim(rendered, "\n")
		}
	}
	if len(c.entries) >= markdownCacheMax {
		c.entries = map[string]string{}
	}
	c.entries[key] = out
	return out
}
