package cli

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/ui"
	"github.com/charmbracelet/x/ansi"
)

// overviewRecent is how many log entries the overview's activity shows.
const overviewRecent = 12

// askBoxBg fills the overview's "What do you want to do?" box (the tags'
// dark grey).
var askBoxBg = lipgloss.Color("236")

// askBoxMaxWidth keeps the box readable on a wide pane.
const askBoxMaxWidth = 72

// renderAskBox is the overview's "What do you want to do?": a filled box,
// centred in the pane (width columns), holding the hint, or once ⏎ is
// pressed, the field you type into. Everything inside carries the box's
// background, so no styled piece leaves a hole in it.
func (m deckModel) renderAskBox(width int) string {
	boxW := min(askBoxMaxWidth, width-4)
	textW := max(boxW-6, 10) // inside the box's padding
	on := lipgloss.NewStyle().Background(askBoxBg)
	title := on.Bold(true).Width(textW).Align(lipgloss.Center).Render("What do you want to do?")
	var body string
	if m.mode == deckModeNewTitle && m.newBriefing {
		in := m.input
		st := in.Styles()
		for _, s := range []*textinput.StyleState{&st.Focused, &st.Blurred} {
			s.Text = s.Text.Background(askBoxBg)
			s.Placeholder = s.Placeholder.Background(askBoxBg)
			s.Suggestion = s.Suggestion.Background(askBoxBg)
			s.Prompt = s.Prompt.Background(askBoxBg)
		}
		in.SetStyles(st)
		in.SetWidth(max(textW-lipgloss.Width(in.Prompt)-1, 5))
		// A second line, so the box keeps its height from the hint.
		body = in.View() + "\n" + on.Faint(true).Width(textW).Render(hintText(m.keys().fieldFooter(), " · "))
	} else {
		body = on.Faint(true).Width(textW).Render("⏎ to say: an idea, a bug, or an issue like 'fe issue 45'. An agent turns it into a mission.")
	}
	box := on.Padding(1, 3).Width(boxW).Render(title + "\n\n" + body)
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, zoneLines(zoneAsk, "", box))
}

// renderOverview is the deck's front page: what you want to do next, what
// needs you, and what's been happening across every mission. Clicking a
// line under Needs you or Recently opens its mission.
func (m deckModel) renderOverview(indent string, inner int) string {
	var b strings.Builder
	b.WriteString("\n" + m.renderAskBox(inner+2*len(indent)) + "\n")

	const titleCol = 26
	line := func(slug, icon, title, text string) string {
		title = ansi.Truncate(title, titleCol-2, "…")
		title += strings.Repeat(" ", titleCol-ansi.StringWidth(title))
		return indent + zone(zoneMission, slug, icon+" "+title+ansi.Truncate(text, max(inner-titleCol-2, 10), "…")) + "\n"
	}

	// What needs you, in the sidebar's order: each reason on its own line.
	b.WriteString("\n" + indent + ui.Orange.Bold(true).Render("Needs you") + "\n")
	needs := 0
	for _, r := range m.rows {
		if r.kind != deckRowMission || r.status.group != groupNeedsYou {
			continue
		}
		dm := m.data.Missions[r.mission]
		for _, why := range r.status.why {
			b.WriteString(line(dm.M.Slug, ui.Orange.Render("•"), firstNonEmpty(dm.M.Title, dm.M.Slug), why))
			needs++
		}
	}
	if needs == 0 {
		b.WriteString(indent + ui.Dim.Render("Nothing right now.") + "\n")
	}

	n := m.groupCounts()
	var counts []string
	for _, g := range []deckGroup{groupInProgress, groupUpNext, groupPast} {
		if n[g] > 0 {
			counts = append(counts, fmt.Sprintf("%d %s", n[g], strings.ToLower(deckGroupNames[g])))
		}
	}
	if len(counts) > 0 {
		b.WriteString("\n" + indent + ui.Dim.Render(strings.Join(counts, " · ")) + "\n")
	}

	// What's been happening, newest first, across missions still in play.
	type entry struct {
		slug, title string
		e           mission.Event
	}
	var recent []entry
	for _, dm := range m.data.Missions {
		if dm.State == mission.Archived {
			continue
		}
		// Newest first already, so entries logged in the same second keep
		// their order through the (stable) sort.
		for i := len(dm.Events) - 1; i >= 0; i-- {
			recent = append(recent, entry{dm.M.Slug, firstNonEmpty(dm.M.Title, dm.M.Slug), dm.Events[i]})
		}
	}
	slices.SortStableFunc(recent, func(a, b entry) int { return b.e.Time.Compare(a.e.Time) })
	b.WriteString("\n" + indent + ui.Bold.Render("Recently") + "\n")
	shown := 0
	for _, r := range recent {
		text, ok := eventSentence(r.e)
		if !ok {
			continue
		}
		b.WriteString(line(r.slug, ui.Dim.Render(eventClock(r.e.Time)), r.title, text))
		if shown++; shown == overviewRecent {
			break
		}
	}
	if shown == 0 {
		b.WriteString(indent + ui.Dim.Render("Nothing yet.") + "\n")
	}
	return b.String()
}

// openMission selects a mission's sidebar row, opening Past first if it's
// landed or archived (Past starts collapsed).
func (m *deckModel) openMission(slug string) {
	for _, r := range m.rows {
		if r.kind == deckRowMission && m.data.Missions[r.mission].M.Slug == slug {
			m.wantRow("m:" + slug)
			return
		}
	}
	m.pastOpen = true
	m.wantRow("m:" + slug)
}

// eventClock is when something happened, briefly: the time today, else the
// day.
func eventClock(t time.Time) string {
	t, now := t.Local(), nowFunc().Local()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04 ")
	}
	return t.Format("Jan 02")
}

// eventSentence is a log entry in plain English, for the overview. Entries
// that only say an orchestrator run started report nothing.
func eventSentence(e mission.Event) (string, bool) {
	text := strings.TrimSpace(e.Text)
	if first, _, cut := strings.Cut(text, "\n"); cut {
		text = first + " …"
	}
	who := firstNonEmpty(e.Crew, "someone")
	capsule := e.Repo + "/" + e.Capsule
	switch e.Kind {
	case mission.EventCreated:
		return "mission created", true
	case mission.EventCapsuleAdded:
		return "capsule " + capsule + " added", true
	case mission.EventCapsuleRemove:
		return "capsule " + capsule + " detached", true
	case mission.EventCapsuleBurned:
		return "capsule " + capsule + " burned", true
	case mission.EventNote:
		if rest, ok := strings.CutPrefix(text, "Orchestrator: "); ok {
			return "orchestrator: " + rest, true
		}
		return "note: " + text, true
	case mission.EventCrewStarted:
		return who + " started", true
	case mission.EventCrewEnded:
		return who + " ended (" + text + ")", true
	case mission.EventCrewResumed:
		return who + " resumed", true
	case mission.EventCrewAsked:
		return who + " asks: " + text, true
	case mission.EventCrewDone:
		return who + " done: " + text, true
	case mission.EventArtifact:
		return who + " added " + text, true
	case mission.EventFlag:
		return "asks you: " + text, true
	case mission.EventFlagAnswered:
		return "you answered: " + text, true
	case mission.EventConclude:
		return "suggested concluding: " + text, true
	case mission.EventResumeWith:
		return "suggested resuming " + who + ": " + text, true
	case mission.EventDispatch:
		return who + " dispatched " + strings.TrimPrefix(text, "by "), true
	case mission.EventDispatchQueued:
		return "next crew queued: " + text, true
	case mission.EventOrchestrate:
		if strings.HasPrefix(text, "looking at ") {
			return "", false
		}
		return "orchestrator: " + text, true
	case mission.EventState:
		return text, true
	}
	return e.Kind + ": " + text, true
}
