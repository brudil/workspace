package cli

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Clicking a mission under Needs you (or Recently) on the overview opens it.
func TestDeckOverview_ClickingAMissionOpensIt(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m.store.SetFlag(slug, "Commit and open PRs?", "orchestrator", "Commit both", "Leave them")
	m, _ = reloadRecords(m)
	m.cursor = 0 // the overview
	if !m.onOverview() {
		t.Fatal("not on the overview")
	}

	x, y := onScreen(t, m, "the orchestrator asks: Commit and open PRs?", 0)
	if dm, ok := click(t, m, x+4, y).selectedMission(); !ok || dm.M.Slug != slug {
		t.Errorf("the click didn't open %s", slug)
	}

	// A line that names no mission does nothing.
	x, y = onScreen(t, m, "Needs you", 0)
	if got := click(t, m, x+1, y); !got.onOverview() || got.mode != deckModeNormal {
		t.Errorf("clicking a heading did something: mode %v", got.mode)
	}

	// The "What do you want to do?" box asks, as ⏎ does.
	x, y = onScreen(t, m, "What do you want to do?", 0)
	if got := click(t, m, x, y); got.mode != deckModeNewTitle || !got.newBriefing {
		t.Errorf("clicking the box didn't ask: mode %v", got.mode)
	}
}

// Clicking a picker's item chooses it.
func TestDeckPick_ClickChooses(t *testing.T) {
	m, _ := shellCrewDeck(t)
	chosen := -1
	m.openPick("Pick one", []string{"first item", "second item"}, func(i int) tea.Cmd {
		chosen = i
		return nil
	})
	x, y := onScreen(t, m, "second item", 0)
	if m = click(t, m, x, y); chosen != 1 || m.mode != deckModeNormal {
		t.Errorf("chose %d, mode %v", chosen, m.mode)
	}
}
