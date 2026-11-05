package cli

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The footers in a crew terminal come from the same bindings the keys run.
func TestDeckKeys_CrewFooters(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m) // focused
	r, _ := liveRun(m, slug)
	waitScreen(t, r, "$")

	footer := func(m deckModel) string {
		v := plainView(m)
		return strings.TrimSpace(v[strings.LastIndex(v, "\n")+1:])
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	next, _ = next.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	m = next.(deckModel)
	want := "ctrl+g then: tab sidebar · [ history · z zoom · m mouse · n/p next/prev crew · s sidebar · S stop · q quit · ? help · ctrl+g send it"
	if got := footer(m); got != want {
		t.Errorf("prefix footer:\n got %q\nwant %q", got, want)
	}
	// Narrower: whole hints drop off the end, with an ellipsis.
	next, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := footer(next.(deckModel)); !strings.HasPrefix(got, "ctrl+g then: tab sidebar") || !strings.HasSuffix(got, " …") {
		t.Errorf("narrow prefix footer: %q", got)
	}
}

// Every entry on the help screen says what it does, and the footers' keys
// are on it.
func TestDeckKeys_HelpScreenIsComplete(t *testing.T) {
	k := deckModel{}.keys()
	onHelp := map[string]bool{}
	for _, e := range k.helpScreen() {
		if e.about == "" || e.helpKey() == "" || !e.Enabled() {
			t.Errorf("help entry %+v says nothing or matches nothing", e.Help())
		}
		onHelp[e.Help().Key] = true
	}
	for _, overview := range []bool{false, true} {
		for _, b := range k.footer(overview) {
			if !onHelp[b.Help().Key] {
				t.Errorf("footer key %q isn't on the help screen", b.Help().Key)
			}
		}
	}
}

// ctrl+] zooms in one key: from the crew's terminal, and from the sidebar
// (which focuses the crew first, so coming back out lands in it).
func TestDeckKeys_ZoomToggle(t *testing.T) {
	zoomed := func(cmd tea.Cmd) bool {
		return cmd != nil && fmt.Sprintf("%T", cmd()) == "tea.execMsg"
	}
	ctrlBracket := tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl}
	if ctrlBracket.String() != deckZoomKey {
		t.Fatalf("ctrl+] reads as %q", ctrlBracket.String())
	}

	m, slug := shellCrewDeck(t)
	m = launch(t, m) // focused
	r, _ := liveRun(m, slug)
	waitScreen(t, r, "$")
	next, cmd := m.Update(ctrlBracket)
	if !zoomed(cmd) {
		t.Error("ctrl+] in the crew's terminal didn't zoom")
	}

	m = next.(deckModel)
	m.focusTerm = false
	m = selectMission(t, m, slug) // the mission row, not the crew's
	next, cmd = m.Update(ctrlBracket)
	if m = next.(deckModel); !zoomed(cmd) || !m.focusTerm {
		t.Errorf("ctrl+] on the mission: zoomed %v, crew focused %v", cmd != nil, m.focusTerm)
	}
}
