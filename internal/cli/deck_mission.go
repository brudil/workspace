package cli

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/ui"
	"github.com/charmbracelet/x/ansi"
)

// The mission view has tabs. Summary, the first, is what you open a mission
// for: what needs you and what to press, then a line or two each for its
// goal, crew, capsules, artifacts and latest log. The rest is a tab away:
// the brief, the work in full (capsules with paths, every artifact), and the
// whole log with every crew's history. 1–4 or a click on a tab switches;
// the tab stays as you move between missions.

type missionTab int

const (
	tabSummary missionTab = iota
	tabBrief
	tabWork
	tabLog
)

var missionTabNames = []string{"Summary", "Brief", "Work", "Log"}

const (
	summaryRecent    = 3  // log lines on Summary
	summaryArtifacts = 3  // artifacts on Summary
	summaryLabelW    = 11 // the left column on Summary: "Artifacts" and a gap
)

var (
	tabActive = lipgloss.NewStyle().Bold(true).Background(askBoxBg)
	tabIdle   = ui.Dim
)

func (m deckModel) renderMission(dm deckMission, indent string, inner int) string {
	head := m.missionHeader(dm, indent)
	switch m.missionTab {
	case tabBrief:
		return head + m.renderMissionBrief(dm, indent, inner)
	case tabWork:
		return head + m.renderMissionWork(dm, indent)
	case tabLog:
		return head + m.renderMissionLog(dm, indent, inner)
	}
	return head + m.renderMissionSummary(dm, indent, inner)
}

// missionHeader is the title, slug and status, and the tab bar (each tab
// clickable).
func (m deckModel) missionHeader(dm deckMission, indent string) string {
	var b strings.Builder
	b.WriteString("\n" + indent + ui.Bold.Render(dm.M.Title) + "\n")
	// Where it stands for you, as the sidebar says; the slug is its id.
	b.WriteString(indent + ui.TagDim.Render(dm.M.Slug) + "  " + m.missionStatus(&dm).label + "\n\n")
	var parts []string
	for i, name := range missionTabNames {
		style := tabIdle
		if missionTab(i) == m.missionTab {
			style = tabActive
		}
		parts = append(parts, zone(zoneTab, strconv.Itoa(i), style.Render(" "+name+" ")))
	}
	b.WriteString(indent + strings.Join(parts, " ") + "  " + ui.Dim.Render("1–4") + "\n")
	return b.String()
}

// renderMissionSummary leads with what needs you, then a line or two of
// everything else.
func (m deckModel) renderMissionSummary(dm deckMission, indent string, inner int) string {
	var b strings.Builder
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
	if f := dm.M.Flag; f != nil {
		who := "The orchestrator asks"
		switch f.By {
		case "orchestrator", "":
		case "you":
			who = "You flagged"
		default:
			who = f.By + " asks"
		}
		b.WriteString("\n" + wrapUnder(indent, ui.Orange.Render("! "+who+": "), f.Text, inner))
		for i, o := range f.Options {
			b.WriteString(wrapUnder(indent, "    "+ui.Dim.Render(fmt.Sprintf("%d.", i+1))+" ", o, inner))
		}
		b.WriteString(indent + ui.Dim.Render("  ⏎ answer · F dismiss") + "\n")
	}
	if d := dm.M.Dispatch; d != nil {
		where := ""
		if d.Capsule != "" {
			where = " in " + d.Capsule
		}
		b.WriteString("\n" + wrapUnder(indent, ui.Orange.Render("→ Next"+where+": "), d.Text+ui.Dim.Render(" ("+d.By+")"), inner))
	}
	if c := dm.M.Conclude; c != nil && dm.State != mission.Archived {
		b.WriteString("\n" + wrapUnder(indent, ui.Green.Render("✓ Suggested conclusion: "), c.Text+ui.Dim.Render(" ("+c.By+") · x archives"), inner))
	}
	if hint := m.conclusionHint(dm); hint != "" {
		b.WriteString("\n" + indent + hint + "\n")
	}
	b.WriteString("\n")

	if goal := briefGoal(dm.Brief); goal != "" {
		b.WriteString(summarySection(indent, "Goal", inner, goal+ui.Dim.Render(" · 2 for the brief")))
	}
	if lines := m.stageSummary(dm); len(lines) > 0 {
		b.WriteString(summarySection(indent, "Stages", inner, lines...))
	}
	var crewLines []string
	for _, l := range strings.Split(m.crewSummary(dm), "\n") {
		crewLines = append(crewLines, strings.TrimLeft(l, " "))
	}
	b.WriteString(summarySection(indent, "Crew", inner, crewLines...))

	var caps []string
	for _, c := range dm.Capsules {
		line := c.Repo + "/" + c.Name
		if badges := deckBadges(dm.Git[mission.Capsule{Repo: c.Repo, Name: c.Name}], c.Exists, c.PR, c.PRS); badges != "" {
			line += "  " + badges
		}
		caps = append(caps, line)
	}
	if len(caps) == 0 {
		caps = []string{ui.Dim.Render("none yet · a attaches a loose capsule · WS_MISSION=" + dm.M.Slug + " ws lift <repo> <name>")}
	}
	b.WriteString(summarySection(indent, "Capsules", inner, caps...))

	if avs := resolveArtifacts(m.ws, m.store, dm.missionView); len(avs) > 0 {
		var arts []string
		for i, av := range avs {
			if i == summaryArtifacts {
				arts = append(arts, ui.Dim.Render(fmt.Sprintf("+%d more · 3 for the work", len(avs)-summaryArtifacts)))
				break
			}
			arts = append(arts, artifactText(av))
		}
		b.WriteString(summarySection(indent, "Artifacts", inner, arts...))
	}

	// The latest few events, as the overview tells them, oldest first.
	var recent []string
	hidden := 0
	for i := len(dm.Events) - 1; i >= 0; i-- {
		text, ok := eventSentence(dm.Events[i])
		if !ok {
			continue
		}
		if len(recent) == summaryRecent {
			hidden++
			continue
		}
		line := ui.Dim.Render(eventClock(dm.Events[i].Time)) + "  " + text
		recent = append([]string{ansi.Truncate(line, max(inner-summaryLabelW, 20), "…")}, recent...)
	}
	if hidden > 0 {
		recent = append(recent, ui.Dim.Render("4 for the whole log"))
	}
	if len(recent) > 0 {
		b.WriteString(summarySection(indent, "Recently", inner, recent...))
	}
	if dm.M.From != "" {
		b.WriteString(summarySection(indent, "Raised from", inner, ui.Dim.Render(dm.M.From)))
	}
	return b.String()
}

// summarySection is a labelled block on Summary: the label in the left
// column, its lines beside it.
func summarySection(indent, label string, inner int, lines ...string) string {
	var b strings.Builder
	lead := ui.Dim.Render(label) + strings.Repeat(" ", max(summaryLabelW-ansi.StringWidth(label), 1))
	pad := strings.Repeat(" ", summaryLabelW)
	for i, l := range lines {
		if i == 0 {
			b.WriteString(wrapUnder(indent, lead, l, inner))
		} else {
			b.WriteString(wrapUnder(indent, pad, l, inner))
		}
	}
	return b.String()
}

// briefGoal is the brief's goal in a line: the first paragraph under a Goal
// heading, wherever it is, else the first paragraph that isn't a heading.
func briefGoal(brief string) string {
	lines := strings.Split(brief, "\n")
	firstParagraph := func(from int) string {
		var para []string
		for _, line := range lines[from:] {
			t := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(t, "#") && len(para) > 0, t == "" && len(para) > 0:
				return strings.Join(para, " ")
			case t == "", strings.HasPrefix(t, "#"):
				continue
			}
			para = append(para, t)
		}
		return strings.Join(para, " ")
	}
	for i, line := range lines {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "#") && strings.EqualFold(strings.TrimSpace(strings.TrimLeft(t, "#")), "goal") {
			if goal := firstParagraph(i + 1); goal != "" {
				return goal
			}
		}
	}
	return firstParagraph(0)
}

func (m deckModel) renderMissionBrief(dm deckMission, indent string, inner int) string {
	var b strings.Builder
	b.WriteString("\n" + indent + ui.Dim.Render("e to edit") + "\n")
	if dm.M.Request != "" {
		b.WriteString(indent + ui.Dim.Render("Requested: ") + strings.ReplaceAll(dm.M.Request, "\n", " ") + "\n")
	}
	if strings.TrimSpace(dm.Brief) == "" {
		b.WriteString("\n" + indent + ui.Dim.Render("(empty)") + "\n")
		return b.String()
	}
	for line := range strings.SplitSeq(m.md.render(dm.Brief, inner), "\n") {
		b.WriteString(indent + line + "\n")
	}
	return b.String()
}

// renderMissionWork is the work in full: every capsule with its path and
// state, and every artifact.
func (m deckModel) renderMissionWork(dm deckMission, indent string) string {
	var b strings.Builder
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
	return b.String()
}

// renderMissionLog is the whole log, then every crew's history.
func (m deckModel) renderMissionLog(dm deckMission, indent string, inner int) string {
	var b strings.Builder
	b.WriteString("\n" + indent + ui.Bold.Render("Log") + "\n")
	if len(dm.Events) == 0 {
		b.WriteString(indent + "  " + ui.Dim.Render("nothing yet") + "\n")
	}
	for _, e := range dm.Events {
		// An event's text can run over several lines (an orchestrator's
		// report): keep it to one entry, wrapped under its indent.
		line := strings.Join(strings.FieldsFunc(formatEventLine(e, false), func(r rune) bool { return r == '\n' }), " ")
		for i, l := range strings.Split(ansi.Wordwrap(line, max(inner-4, 20), ""), "\n") {
			if i == 0 {
				b.WriteString(indent + "  " + l + "\n")
			} else {
				b.WriteString(indent + "    " + l + "\n")
			}
		}
	}
	if len(dm.M.Crew) > 0 {
		b.WriteString("\n" + indent + ui.Bold.Render("Crew") + "\n")
	}
	for _, c := range dm.M.Crew {
		role := "crew"
		if c.Role == mission.RoleBriefing {
			role = "briefing"
		}
		when := "started " + eventClock(c.Started)
		if c.Ended != nil {
			when += ", ended " + eventClock(*c.Ended) + " (" + c.Exit + ")"
		} else {
			when += ", running"
		}
		line := ui.Bold.Render(c.ID) + "  " + c.Harness + " · " + role + " · " + when
		if c.Capsule != "" {
			line += ui.Dim.Render(" · in " + c.Capsule)
		}
		if c.Status != "" {
			line += " · " + c.Status + ": " + c.Note
		}
		b.WriteString(wrapUnder(indent+"  ", "", line, inner-2))
		if c.Dispatch != "" {
			b.WriteString(wrapUnder(indent+"    ", ui.Dim.Render("dispatched for: "), c.Dispatch, inner-4))
		}
		if c.HandedOff != nil {
			path := m.store.HandoffPath(dm.M.Slug, c.ID)
			b.WriteString(indent + "    " + ui.Dim.Render("handoff: ") + ui.Hyperlink("file://"+path, path) + "\n")
		}
	}
	return b.String()
}

// stageSummary is the brief's stages with their progress, a line each:
// accepted, current (with its crew), still to come. None when the brief
// lists no stages.
func (m deckModel) stageSummary(dm deckMission) []string {
	states, _ := dm.M.StageProgress(dm.Brief)
	if len(states) == 1 && states[0].Implicit {
		return nil
	}
	var lines []string
	for _, st := range states {
		title := fmt.Sprintf("%d %s", st.N, st.Short())
		var facts []string
		if st.Record != nil && len(st.Record.Crew) > 0 {
			facts = append(facts, strings.Join(st.Record.Crew, ", "))
		}
		if st.CheckIn && !st.Accepted() {
			facts = append(facts, "check in after")
		}
		extra := ""
		if len(facts) > 0 {
			extra = ui.Dim.Render(" · " + strings.Join(facts, " · "))
		}
		switch {
		case st.Accepted():
			lines = append(lines, ui.Green.Render("✓ ")+title+extra)
		case st.Current:
			lines = append(lines, ui.Orange.Render("▸ ")+ui.Bold.Render(title)+extra)
		default:
			lines = append(lines, ui.Dim.Render("· "+title)+extra)
		}
	}
	return lines
}
