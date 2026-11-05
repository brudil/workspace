package cli

import (
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/mission"
	"github.com/charmbracelet/x/ansi"
)

func missionPane(m deckModel) string {
	_, content := m.mainContent(m.mainWidth())
	return ansi.Strip(content)
}

// The mission view leads with Summary; 1–4 or a click switch tabs, and the
// tab stays as you move between missions.
func TestDeckMission_Tabs(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m.store.WriteBrief(slug, "## Goal\nCache the token lookup.\n\n## Constraints\nNo new deps.\n")
	m.store.SetFlag(slug, "Which cache?", "orchestrator", "Per-org", "Global")
	for i := range 5 {
		m.store.AppendLog(slug, mission.Event{Kind: mission.EventNote, Text: "note " + string(rune('a'+i)) + "\nwith a second line"})
	}
	next, _ := m.Update(loadDeckCmd(m.ws, m.prs)()) // the brief and log too, not just the records
	m = selectMission(t, next.(deckModel), slug)

	sum := missionPane(m)
	for _, want := range []string{"Summary   Brief   Work   Log", "! The orchestrator asks: Which cache?", "1. Per-org",
		"Goal       Cache the token lookup. · 2 for the brief", "Capsules   frontend/work", "4 for the whole log"} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary missing %q:\n%s", want, sum)
		}
	}
	if strings.Contains(sum, "No new deps") || strings.Contains(sum, m.ws.Root) {
		t.Errorf("summary shows the whole brief or capsule paths:\n%s", sum)
	}

	m, _ = press(t, m, "2")
	if v := missionPane(m); !strings.Contains(v, "No new deps") || strings.Contains(v, "Which cache?") {
		t.Errorf("brief tab:\n%s", v)
	}
	m, _ = press(t, m, "3")
	if v := missionPane(m); !strings.Contains(v, "Capsules") || !strings.Contains(v, m.ws.Root) {
		t.Errorf("work tab shows no capsule paths:\n%s", v)
	}
	m, _ = press(t, m, "4")
	log := missionPane(m)
	if !strings.Contains(log, "note a with a second line") || !strings.Contains(log, "Crew") {
		t.Errorf("log tab (multi-line events kept to one entry, then crew):\n%s", log)
	}

	// The tab stays as the cursor moves; the overview isn't a mission.
	m.cursor = 0
	m = selectMission(t, m, slug)
	if m.missionTab != tabLog {
		t.Errorf("tab = %d after moving away and back, want Log", m.missionTab)
	}

	// Clicking a tab's name switches to it.
	x, y := onScreen(t, m, " Brief ", 0)
	if got := click(t, m, x+2, y).missionTab; got != tabBrief {
		t.Errorf("clicked Brief, tab = %d", got)
	}
}

func TestBriefGoal(t *testing.T) {
	for brief, want := range map[string]string{
		"# Title\n\n## Goal\nCache the\ntoken lookup.\n\n## Constraints\nx": "Cache the token lookup.",
		"Just a sentence.\n\nMore.":                                         "Just a sentence.",
		"## Context\nBackground.\n\n## Goal\nDo it.":                        "Do it.",
		"": "",
	} {
		if got := briefGoal(brief); got != want {
			t.Errorf("briefGoal(%q) = %q, want %q", brief, got, want)
		}
	}
}
