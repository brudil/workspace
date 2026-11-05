package cli

import (
	"fmt"
	"strconv"
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
	f := m.frame()
	v := tea.NewView(renderFrame(f))
	v.OnMouse = clickHandler(f)
	v.AltScreen = true
	v.MouseMode = m.mouseMode()
	// The tab says when you're needed, so a background tab can tell you.
	v.WindowTitle = "Deck"
	if n := m.groupCounts()[groupNeedsYou]; m.loaded && n > 0 {
		v.WindowTitle = fmt.Sprintf("Deck · %d need you", n)
	}
	v.ProgramStatus = m.programStatus()
	// Focus reports let notifications wait until you're elsewhere.
	v.ReportFocus = true
	// With a crew terminal focused, put the real cursor where the agent's is.
	if r, ok := m.focusedRun(); ok && !m.showHelp && m.mode == deckModeNormal && r.scroll == 0 {
		if x, y, visible := r.term.Cursor(); visible {
			v.Cursor = tea.NewCursor(x, y+2) // below header and rule
		}
	}
	return v
}

// programStatus is what the deck reports to terminals that show programs'
// status on their tabs (OSC 7501): blocked while a mission needs you, with
// the most pressing reason, working while crew work, launch or the
// orchestrator decides, and nothing when all is quiet. Terminals drop
// working and blocked reports when the program exits, so a deck that quits
// or detaches leaves nothing behind.
func (m deckModel) programStatus() *tea.ProgramStatus {
	if !m.loaded {
		return nil
	}
	needs, moving := 0, false
	var top deckRow
	// Rows are in the sidebar's order: the first needing you is the most
	// pressing.
	for _, r := range m.rows {
		if r.kind != deckRowMission {
			continue
		}
		switch {
		case r.status.group == groupNeedsYou:
			if needs == 0 {
				top = r
			}
			needs++
		case r.status.moving:
			moving = true
		}
	}
	ps := &tea.ProgramStatus{App: "ws", Title: m.ws.Title() + " deck"}
	switch {
	case needs > 0:
		dm := m.data.Missions[top.mission]
		ps.State, ps.Kind = tea.ProgramStateBlocked, top.status.waitsFor
		ps.Message = firstNonEmpty(dm.M.Title, dm.M.Slug)
		if len(top.status.why) > 0 {
			ps.Message += ": " + top.status.why[0]
		}
		if needs > 1 {
			ps.Message = fmt.Sprintf("%d need you · %s", needs, ps.Message)
		}
	case moving:
		ps.State, ps.Message = tea.ProgramStateWorking, m.counts()
	default:
		return nil
	}
	return ps
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

// render is the deck's screen as text.
func (m deckModel) render() string { return renderFrame(m.frame()) }

// frame lays the deck out as layers: the header, the main pane, the divider
// and sidebar, and the footer, each where it sits on the screen, with a
// child layer for every zone (see zone). The same layout draws the deck and
// answers clicks.
func (m deckModel) frame() *lipgloss.Compositor {
	if m.width == 0 {
		return lipgloss.NewCompositor(lipgloss.NewLayer("loading..."))
	}
	if m.showHelp {
		return lipgloss.NewCompositor(paneLayer(m.renderHelp(), 0, 0))
	}

	bodyH := m.bodyHeight()
	var main string
	if r, ok := m.shownRun(); ok {
		main = renderTerm(r.term, r.scroll, m.mainWidth(), bodyH)
	} else {
		m.syncMain()
		main = m.mainVP.View()
	}
	layers := []*lipgloss.Layer{
		paneLayer(m.renderHeader(), 0, 0),
		paneLayer(main, 0, 2), // below the header and its rule
		paneLayer(m.renderFooter(), 0, 2+bodyH),
	}
	if m.sidebarShown() {
		divider := deckDivider.Render(strings.TrimSuffix(strings.Repeat("│\n", bodyH), "\n"))
		layers = append(layers,
			lipgloss.NewLayer(divider).X(m.mainWidth()).Y(2),
			paneLayer(m.renderSidebar(deckSidebarWidth, bodyH), m.mainWidth()+1, 2))
	}
	return lipgloss.NewCompositor(layers...)
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
	k := m.keys()
	switch m.mode {
	case deckModeNewTitle:
		if m.newBriefing && m.onOverview() {
			return " " + hints(k.fieldFooter(), " · ", m.width-1) // the field is on the overview
		}
		if m.answering {
			return " " + ui.Bold.Render("Your answer:") + " " + m.input.View()
		}
		if m.newBriefing {
			return " " + ui.Bold.Render("What do you want to do?") + " " + m.input.View()
		}
		return " " + ui.Bold.Render("Mission title:") + " " + m.input.View()
	case deckModeConfirm:
		return " " + ui.Orange.Render(m.confirm.prompt) + ui.Dim.Render("  "+k.Yes.Help().Key)
	case deckModePick:
		return " " + hints(k.pickFooter(), "  ", m.width-1)
	}
	if m.focusTerm {
		if m.scrollMode {
			return " " + ui.Orange.Render("history") + " " + hints(k.scrollFooter(), " · ", m.width-9)
		}
		if m.prefix {
			lead := " " + ui.Orange.Render(k.Prefix.Help().Key) + ui.Dim.Render(" then: ")
			return lead + hints(k.prefixFooter(), " · ", m.width-lipgloss.Width(lead))
		}
		if m.flash != "" {
			return " " + m.flash
		}
		return ui.Dim.Render(ansi.Truncate(" keys go to the crew · wheel scrolls · shift-drag selects · "+deckZoomKey+" zoom · "+deckPrefixKey+" for deck commands (tab back to the sidebar)", m.width, "…"))
	}
	if m.flash != "" {
		return " " + m.flash
	}
	return " " + hints(k.footer(m.onOverview()), "  ", m.width-1)
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
		return fmt.Sprintf("m:%s:%d", dm.M.Slug, m.missionTab), m.renderMission(dm, indent, inner)
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
	width := max(m.mainWidth()-2*len(indent), 20)
	var b strings.Builder
	b.WriteString("\n" + wrapUnder(indent, "", ui.Bold.Render(m.pick.title), width) + "\n")
	for i, item := range m.pick.items {
		var lines string
		if i == m.pick.cursor {
			lines = wrapUnder(indent, ui.Orange.Render("▸ "), ui.Bold.Render(item), width)
		} else {
			lines = wrapUnder(indent, "  ", item, width)
		}
		b.WriteString(zoneLines(zonePick, strconv.Itoa(i), strings.TrimSuffix(lines, "\n")) + "\n")
	}
	return b.String()
}

// wrapUnder writes lead then text, word-wrapped to width, with every line
// after the first indented to start under the text: a question or answer
// is read whole, not cut off at the pane's edge.
func wrapUnder(indent, lead, text string, width int) string {
	leadW := ansi.StringWidth(lead)
	lines := strings.Split(ansi.Wordwrap(text, max(width-leadW, 20), ""), "\n")
	var b strings.Builder
	for i, l := range lines {
		if i == 0 {
			b.WriteString(indent + lead + l + "\n")
		} else {
			b.WriteString(indent + strings.Repeat(" ", leadW) + l + "\n")
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
		add(zone(zoneRow, r.key(m.data), line), i)
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
// what it was dispatched with.
func (m deckModel) crewPurpose(r *deckCrewRun) string {
	switch r.role {
	case mission.RoleBriefing:
		return "briefing"
	case mission.RoleAnswer:
		return "answering the orchestrator's question"
	}
	for _, dm := range m.data.Missions {
		if dm.M.Slug == r.slug {
			if c, ok := crewRecord(dm.M, r.crewID); ok && c.Dispatch != "" {
				return c.Dispatch
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

// --- help ---

func (m deckModel) renderHelp() string {
	var b strings.Builder
	b.WriteString(ui.Bold.Render("  Deck") + "\n\n")
	for _, k := range m.keys().helpScreen() {
		b.WriteString("  " + lipgloss.NewStyle().Bold(true).Width(10).Render(k.helpKey()) + " " + k.about + "\n")
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
// (found from its branch), then recorded artifacts grouped by kind, each
// with where it is now.
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
	// Recorded artifacts, grouped by kind in the order kinds first appear.
	avs := resolveArtifacts(m.ws, m.store, dm.missionView)
	var kinds []string
	byKind := map[string][]artifactView{}
	for _, av := range avs {
		if _, seen := byKind[av.Kind]; !seen {
			kinds = append(kinds, av.Kind)
		}
		byKind[av.Kind] = append(byKind[av.Kind], av)
	}
	for _, k := range kinds {
		for _, av := range byKind[k] {
			lines = append(lines, artifactText(av))
		}
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

// mouseMode decides whether the deck captures the mouse. It does, even
// while a crew has the keyboard: then the sidebar is clickable and the wheel
// scrolls the crew's history. Let go of, the terminal (on the alternate
// screen the deck draws on) turns the wheel into up/down keys, which went to
// the agent and paged through its prompt history. Selecting text takes
// Shift-drag (terminals let Shift bypass a program's mouse capture); M lets
// go everywhere, for plain selection and links.
func (m deckModel) mouseMode() tea.MouseMode {
	if m.mouseOff {
		return tea.MouseModeNone
	}
	return tea.MouseModeCellMotion
}
