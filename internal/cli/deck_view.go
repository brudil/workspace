package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/brudil/workspace/internal/github"
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
	v.MouseMode = m.mouseMode()
	// The tab says when you're needed, so a background tab can tell you.
	v.WindowTitle = "Deck"
	if n := m.groupCounts()[groupNeedsYou]; m.loaded && n > 0 {
		v.WindowTitle = fmt.Sprintf("Deck · %d need you", n)
	}
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
		if working, since := r.activity(); working {
			label += " · working"
		} else {
			label += " · waiting " + shortDuration(since)
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
		if m.newBriefing && m.onOverview() {
			return ui.Dim.Render(" ⏎ start it · esc cancel") // the field is on the overview
		}
		if m.newBriefing {
			return " " + ui.Bold.Render("What do you want to do?") + " " + m.input.View()
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
			return " " + ui.Orange.Render(deckPrefixKey) + ui.Dim.Render(ansi.Truncate(" then: tab sidebar · [ history · z zoom · m mouse · n/p next/prev crew · s sidebar · S stop · q quit · ? help · "+deckPrefixKey+" send it", m.width-len(deckPrefixKey)-2, "…"))
		}
		if m.flash != "" {
			return " " + m.flash
		}
		return ui.Dim.Render(ansi.Truncate(" keys go to the crew · select text and click links freely · "+deckPrefixKey+" for deck commands (tab back to the sidebar)", m.width, "…"))
	}
	if m.flash != "" {
		return " " + m.flash
	}
	hints := " j/k move  ⏎ crew  tab focus  n new  e brief  a attach  p park  R resume  S stop  ? help  q quit"
	if m.onOverview() {
		hints = " ⏎ start a mission  j/k move  ? help  q quit"
	}
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
	case deckRowOverview:
		return "overview", m.renderOverview(indent, inner)
	case deckRowMission, deckRowCrew:
		dm := m.data.Missions[r.mission]
		return "m:" + dm.M.Slug, m.renderMission(dm, indent, inner)
	case deckRowLoose:
		c := m.data.Loose[r.capsule]
		return "l:" + c.Repo + "/" + c.Name, m.renderLoose(c, indent)
	case deckRowPastHeader:
		return "past", "\n" + indent + fmt.Sprintf("%d missions have landed or been archived.", m.groupCounts()[groupPast]) +
			"\n" + indent + ui.Dim.Render("⏎ lists them; selecting one shows it as it was. x on an archived mission reopens it.")
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
	// Where it stands for you, as the sidebar says; the slug is its id.
	b.WriteString(indent + ui.TagDim.Render(dm.M.Slug) + "  " + m.missionStatus(&dm).label + "\n")
	b.WriteString(indent + m.crewSummary(dm) + "\n")
	if dm.State == mission.Briefing {
		if len(m.reg.live(dm.M.Slug)) > 0 {
			b.WriteString("\n" + indent + ui.Orange.Render("Being briefed") + ui.Dim.Render(" · an agent is turning the request into a brief and capsules; it finishes with ws mission ready") + "\n")
		} else {
			b.WriteString("\n" + indent + ui.Orange.Render("Waiting to be briefed") + ui.Dim.Render(" · ⏎ starts a briefing agent") + "\n")
		}
		if dm.M.Request != "" {
			b.WriteString(indent + ui.Dim.Render("Requested: ") + strings.ReplaceAll(dm.M.Request, "\n", " ") + "\n")
		}
	}
	if dm.M.From != "" {
		b.WriteString(indent + ui.Dim.Render("Raised from: "+dm.M.From) + "\n")
	}
	if f := dm.M.Flag; f != nil {
		b.WriteString("\n" + indent + ui.Orange.Render("! Needs you: ") + f.Text + ui.Dim.Render(" ("+f.By+") · F dismisses") + "\n")
	}
	if d := dm.M.Dispatch; d != nil {
		where := ""
		if d.Capsule != "" {
			where = " in " + d.Capsule
		}
		b.WriteString("\n" + indent + ui.Orange.Render("→ Next"+where+": ") + d.Text + ui.Dim.Render(" ("+d.By+")") + "\n")
	}
	if c := dm.M.Conclude; c != nil && dm.State != mission.Archived {
		b.WriteString("\n" + indent + ui.Green.Render("✓ Suggested conclusion: ") + c.Text + ui.Dim.Render(" ("+c.By+") · x archives") + "\n")
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

	b.WriteString(m.renderArtifacts(dm, indent))
	if hint := m.conclusionHint(dm); hint != "" {
		b.WriteString("\n" + indent + hint + "\n")
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
	var lines []string
	var rowOf []int
	add := func(s string, row int) {
		lines = append(lines, s)
		rowOf = append(rowOf, row)
	}
	// right puts a label at the end of a row of this width.
	right := func(left, label string) string {
		avail := width - lipgloss.Width(label) - 2 // a space either side of the label
		left = ansi.Truncate(left, avail, "…")
		return left + strings.Repeat(" ", avail-lipgloss.Width(left)+1) + label + " "
	}
	marker := func(selected bool) string {
		if selected {
			return ui.Orange.Render("▸ ")
		}
		return "  "
	}

	group := deckGroup(-1)
	for i, r := range m.rows {
		selected := i == m.cursor && m.mode == deckModeNormal
		var line string
		switch r.kind {
		case deckRowOverview:
			line = " " + marker(selected) + ui.Bold.Render("Overview")
		case deckRowMission:
			if g := r.status.group; g != group && g != groupPast {
				group = g
				add("", -1)
				heading := ui.Bold.Render(" " + strings.ToUpper(deckGroupNames[g]))
				if g == groupNeedsYou {
					heading = ui.Orange.Bold(true).Render(" " + strings.ToUpper(deckGroupNames[g]))
				}
				add(heading, -1)
			}
			dm := m.data.Missions[r.mission]
			title := firstNonEmpty(dm.M.Title, dm.M.Slug)
			if r.status.group == groupPast {
				title = ui.Dim.Render(title)
			}
			line = right(" "+marker(selected)+title, r.status.label)
		case deckRowCrew:
			mk := "   "
			if selected && m.focusTerm {
				mk = ui.Green.Render(" ▸ ")
			}
			line = "  " + mk + m.crewBadge(r.run)
			if what := m.crewPurpose(r.run); what != "" {
				line += ui.Dim.Render(" · " + what)
			}
		case deckRowPastHeader:
			add("", -1)
			line = "  " + ui.Dim.Render(fmt.Sprintf("%s Past (%d)", arrow(m.pastOpen), m.groupCounts()[groupPast]))
		case deckRowLooseHeader:
			// One gap before the entries to the past, not one each.
			if i == 0 || m.rows[i-1].kind != deckRowPastHeader {
				add("", -1)
			}
			line = "  " + ui.Dim.Render(fmt.Sprintf("%s Loose capsules (%d)", arrow(m.looseOpen), len(m.data.Loose)))
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
		if r.kind == deckRowOverview && m.loaded && len(m.data.Missions) == 0 {
			add(ui.Dim.Render("   no missions yet · n starts one"), -1)
		}
	}
	return lines, rowOf
}

func arrow(open bool) string {
	if open {
		return "▾"
	}
	return "▸"
}

// crewPurpose is what a crew is for, in a few words: briefing, or the
// handoff it was dispatched with.
func (m deckModel) crewPurpose(r *deckCrewRun) string {
	if r.role == mission.RoleBriefing {
		return "briefing"
	}
	for _, dm := range m.data.Missions {
		if dm.M.Slug == r.slug {
			if c, ok := crewRecord(dm.M, r.crewID); ok && c.Handoff != "" {
				return c.Handoff
			}
		}
	}
	return ""
}

// onOverview reports whether the overview is selected.
func (m deckModel) onOverview() bool {
	r, ok := m.selected()
	return ok && r.kind == deckRowOverview
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
		{"j/k ↑/↓", "Move through the overview, missions and their crew"},
		{"⏎", "Launch crew (" + m.roles.ForCrew().Harness + ") on the mission, or focus its live crew"},
		{"L", "Launch crew with a chosen harness"},
		{"R", "Resume the mission's last crew conversation"},
		{"tab", "Focus the mission's live crew terminal"},
		{deckPrefixKey, "In a crew terminal: then tab returns here, s hides the sidebar, S stops crew"},
		{"S", "Stop the mission's live crew"},
		{"F", "Dismiss the flag, then any queued crew"},
		{"M", "Mouse: let the terminal select text and open links everywhere (again: the deck takes the mouse back)"},
		{"J/K", "Scroll the main pane (also ctrl+d/u, mouse wheel)"},
		{"n", "New mission: say what you want (⏎ on the overview too); a briefing agent (" + m.roles.ForBriefing().Harness + ") writes the brief and lifts capsules"},
		{"N", "New mission you name and brief yourself (in $EDITOR)"},
		{"e", "Edit the selected mission's brief in $EDITOR"},
		{"a", "Attach: a loose capsule to this mission, or this loose capsule to a mission"},
		{"d", "Detach one of the mission's capsules (it's kept)"},
		{"p", "Park / reopen the selected mission"},
		{"x", "Archive / reopen the selected mission"},
		{"A", "Show or hide past missions (landed and archived; also ⏎ on Past)"},
		{"⏎ (loose)", "Show or hide capsules that belong to no mission"},
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

// renderArtifacts lists what the mission has produced: each capsule's PR
// (found from its branch) and links crew recorded.
func (m deckModel) renderArtifacts(dm deckMission, indent string) string {
	var lines []string
	for _, c := range dm.Capsules {
		if c.PR == nil {
			continue
		}
		state := ui.Blue.Render("open")
		if c.PRS == mission.PRMerged {
			state = ui.Green.Render("merged")
		}
		line := fmt.Sprintf("%s  #%d %s", c.Repo+"/"+c.Name, c.PR.Number, state)
		if c.PR.Title != "" {
			line += "  " + c.PR.Title
		}
		if c.PR.URL != "" {
			line = ui.Hyperlink(c.PR.URL, line)
		}
		lines = append(lines, line)
	}
	for _, a := range dm.M.Artifacts {
		label := a.Label
		if label == "" {
			label = a.URL
		}
		line := ui.Hyperlink(a.URL, label)
		if a.Label != "" {
			line += ui.Dim.Render("  " + a.URL)
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n" + indent + ui.Bold.Render("Artifacts") + "\n")
	for _, l := range lines {
		b.WriteString(indent + "  " + l + "\n")
	}
	return b.String()
}

// conclusionHint says when a mission looks finished, and what's left.
func (m deckModel) conclusionHint(dm deckMission) string {
	live := m.reg.live(dm.M.Slug)
	allDone := len(live) > 0
	for _, r := range live {
		if status, _ := m.crewStatus(r); status != mission.CrewDone {
			allDone = false
		}
	}
	switch {
	case dm.State == mission.Landed && len(live) == 0:
		return ui.Green.Render("✓ Concluded") + ui.Dim.Render(" · every PR merged · x archives the mission")
	case dm.State == mission.Landed && allDone:
		return ui.Green.Render("✓ Concluded") + ui.Dim.Render(" · every PR merged and crew done · S stops crew, then x archives")
	case allDone && dm.State == mission.Review:
		return ui.Green.Render("✓ Crew done") + ui.Dim.Render(" · PRs in review · tab to the crew to handle review comments")
	case allDone:
		return ui.Green.Render("✓ Crew done") + ui.Dim.Render(" · check their work; open PRs if needed")
	}
	return ""
}

// mouseMode decides whether the deck captures the mouse. Capturing it makes
// the sidebar clickable and the wheel scroll history, but stops the
// terminal selecting text, copying and Cmd-clicking links. So while a crew
// terminal has the keyboard (you're reading and working with it) the deck
// lets go, and M lets go everywhere.
func (m deckModel) mouseMode() tea.MouseMode {
	if m.mouseOff {
		return tea.MouseModeNone
	}
	if _, ok := m.focusedRun(); ok {
		return tea.MouseModeNone
	}
	return tea.MouseModeCellMotion
}
