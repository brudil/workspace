package cli

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/ui"
	"github.com/charmbracelet/x/ansi"
)

// overviewRecent is how many log entries the overview's activity shows.
const overviewRecent = 12

// renderOverview is the deck's front page: what you want to do next, what
// needs you, and what's been happening across every mission.
func (m deckModel) renderOverview(indent string, inner int) string {
	var b strings.Builder
	b.WriteString("\n" + indent + ui.Bold.Render("What do you want to do?") + "\n")
	if m.mode == deckModeNewTitle && m.newBriefing {
		b.WriteString(indent + m.input.View() + "\n")
	} else {
		b.WriteString(indent + ui.Dim.Render(ansi.Truncate("⏎ to say: an idea, a bug, or an issue like 'fe issue 45'. An agent turns it into a mission.", inner, "…")) + "\n")
	}

	const titleCol = 26
	line := func(icon, title, text string) string {
		title = ansi.Truncate(title, titleCol-2, "…")
		title += strings.Repeat(" ", titleCol-ansi.StringWidth(title))
		return indent + icon + " " + title + ansi.Truncate(text, max(inner-titleCol-2, 10), "…") + "\n"
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
			b.WriteString(line(ui.Orange.Render("•"), firstNonEmpty(dm.M.Title, dm.M.Slug), why))
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
		title string
		e     mission.Event
	}
	var recent []entry
	for _, dm := range m.data.Missions {
		if dm.State == mission.Archived {
			continue
		}
		// Newest first already, so entries logged in the same second keep
		// their order through the (stable) sort.
		for i := len(dm.Events) - 1; i >= 0; i-- {
			recent = append(recent, entry{firstNonEmpty(dm.M.Title, dm.M.Slug), dm.Events[i]})
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
		b.WriteString(line(ui.Dim.Render(eventClock(r.e.Time)), r.title, text))
		if shown++; shown == overviewRecent {
			break
		}
	}
	if shown == 0 {
		b.WriteString(indent + ui.Dim.Render("Nothing yet.") + "\n")
	}
	return b.String()
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
		return "flagged for you: " + text, true
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
