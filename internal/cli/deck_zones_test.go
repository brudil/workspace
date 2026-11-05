package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// click clicks at x, y as the terminal would: the view on screen hit-tests
// it (View.OnMouse), then Update gets the click and, after it, what it hit.
func click(t *testing.T, m deckModel, x, y int) deckModel {
	t.Helper()
	msg := tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y}
	cmd := m.View().OnMouse(msg)
	next, _ := m.Update(msg)
	m = next.(deckModel)
	if cmd != nil {
		next, _ = m.Update(cmd())
		m = next.(deckModel)
	}
	return m
}

// onScreen is where text first appears on the deck's screen at or right of
// column fromX.
func onScreen(t *testing.T, m deckModel, text string, fromX int) (x, y int) {
	t.Helper()
	for y, l := range strings.Split(plainView(m), "\n") {
		for off := 0; ; {
			i := strings.Index(l[off:], text)
			if i < 0 {
				break
			}
			if x := ansi.StringWidth(l[:off+i]); x >= fromX {
				return x, y
			}
			off += i + len(text)
		}
	}
	t.Fatalf("%q isn't on screen right of column %d:\n%s", text, fromX, plainView(m))
	return 0, 0
}

func TestStripZones(t *testing.T) {
	in := "ab " + zone("k", "1", "\x1b[1mcd\x1b[0m") + " " + zone("k", "2", "e"+zone("k", "3", "f")) +
		"\n" + zone("k", "4", "gh\nij") + "!"
	clean, spans := stripZones(in)
	if ansi.Strip(clean) != "ab cd ef\ngh\nij!" {
		t.Errorf("clean = %q", clean)
	}
	want := []zoneSpan{
		{id: "k:1", line: 0, x0: 3, x1: 5},
		{id: "k:3", line: 0, x0: 7, x1: 8, depth: 1},
		{id: "k:2", line: 0, x0: 6, x1: 8},
		{id: "k:4", line: 1, x0: 0, x1: 2},
		{id: "k:4", line: 2, x0: 0, x1: 2},
	}
	if len(spans) != len(want) {
		t.Fatalf("spans = %+v", spans)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Errorf("span %d = %+v, want %+v", i, spans[i], want[i])
		}
	}
}

func TestRenderFrame_MeasuresByGrapheme(t *testing.T) {
	// A family emoji is one grapheme, two columns wide, as lipgloss
	// measures it: the bar after it is at column 2. Counted rune by rune
	// it would be six wide, and the x would land inside it.
	line := "👨‍👩‍👧|"
	f := lipgloss.NewCompositor(lipgloss.NewLayer(line), lipgloss.NewLayer("x").X(2))
	if got := ansi.Strip(renderFrame(f)); got != "👨‍👩‍👧x" {
		t.Errorf("render = %q", got)
	}
}

func TestPaneLayer_ZonesKeepTheirLookAndAreHit(t *testing.T) {
	content := "  " + zone("k", "a", "\x1b[1;31mred\x1b[0m") + " plain"
	// As in the deck, something sits at the origin: the canvas starts there.
	f := lipgloss.NewCompositor(lipgloss.NewLayer("header"), paneLayer(content, 0, 1))
	if got := renderFrame(f); !strings.Contains(got, "\x1b[31;1mred") || strings.Contains(got, "7777") {
		t.Errorf("render = %q", got)
	}
	if hit := f.Hit(3, 1); hit.ID() != "k:a" {
		t.Errorf("hit at the zone = %q", hit.ID())
	}
	if hit := f.Hit(7, 1); !hit.Empty() {
		t.Errorf("hit beside the zone = %q", hit.ID())
	}
}
