package cli

import (
	"strings"
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
	content, links := m.overviewWithLinks("  ", max(m.mainWidth()-4, 10))
	lines := strings.Split(content, "\n")
	target := -1
	for i, l := range lines {
		if strings.Contains(l, "the orchestrator asks: Commit and open PRs?") {
			target = i
		}
	}
	if target < 0 || links[target] != slug {
		t.Fatalf("Needs you line %d links to %q (links %v)", target, links[target], links)
	}

	// Rows 0 and 1 are the header and rule; the pane isn't scrolled.
	next, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: target + 2})
	m = next.(deckModel)
	if dm, ok := m.selectedMission(); !ok || dm.M.Slug != slug {
		t.Errorf("the click didn't open %s (cursor %d)", slug, m.cursor)
	}

	// A line that names no mission does nothing.
	m.cursor = 0
	next, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: 3})
	if !next.(deckModel).onOverview() {
		t.Error("clicking the ask box left the overview")
	}
}
