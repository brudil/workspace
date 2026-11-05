package cli

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Clicks. A renderer marks clickable text with zone(id, text). The marks are
// zero-width escape sequences, so they survive styling, padding, truncation
// and the viewport. Laying out a frame (deckModel.frame) strips them, and
// every marked span becomes a Lip Gloss layer with that id, drawn over the
// text it covers. View.OnMouse hit-tests a click against the frame you saw
// and sends Update a deckClickMsg naming the zone: nothing re-runs the
// layout to work out what was under the mouse.

// zoneOSC is the private OSC number the marks use. They never reach the
// terminal.
const zoneOSC = "7777"

const (
	zoneOpenPrefix = "\x1b]" + zoneOSC + ";"
	zoneClose      = zoneOpenPrefix + "\a"
)

// Zone ids are a kind and an argument: "row:m:cache-tokens".
const (
	zoneRow     = "row"     // a sidebar row, by its key
	zoneMission = "mission" // a line about a mission: opens it
	zoneTab     = "tab"     // a mission view tab, by number
	zonePick    = "pick"    // a picker item, by number
	zoneAsk     = "ask"     // the overview's "What do you want to do?"
)

// zone marks text as clickable. Zones may nest; the innermost wins.
func zone(kind, arg, text string) string {
	return zoneOpenPrefix + kind + ":" + arg + "\a" + text + zoneClose
}

// zoneLines marks each line of text separately, so whatever pads the lines
// afterwards (centring, a column's indent) isn't part of the zone.
func zoneLines(kind, arg, text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = zone(kind, arg, l)
	}
	return strings.Join(lines, "\n")
}

// zoneSpan is where one zone landed: a line (from 0) and columns [x0, x1).
// A zone that runs over several lines is a span per line.
type zoneSpan struct {
	id           string
	line, x0, x1 int
	depth        int // how many zones it's inside
}

// zoneMark reports whether seq is a zone mark, and the id it opens ("" for
// a close).
func zoneMark(seq string) (string, bool) {
	if !strings.HasPrefix(seq, zoneOpenPrefix) {
		return "", false
	}
	id := strings.TrimPrefix(seq, zoneOpenPrefix)
	id = strings.TrimSuffix(strings.TrimSuffix(id, "\a"), "\x1b\\")
	return id, true
}

// stripZones removes the zone marks from content and says where each zone
// landed.
func stripZones(content string) (string, []zoneSpan) {
	if !strings.Contains(content, zoneOpenPrefix) {
		return content, nil
	}
	type open struct {
		id string
		x0 int
	}
	var stack []open
	var spans []zoneSpan
	lines := strings.Split(content, "\n")
	for li, line := range lines {
		for i := range stack {
			stack[i].x0 = 0 // carried over from the line before
		}
		var b strings.Builder
		col := 0
		var state byte
		for len(line) > 0 {
			seq, width, n, next := ansi.DecodeSequence(line, state, nil)
			state, line = next, line[n:]
			id, mark := zoneMark(seq)
			switch {
			case !mark:
				b.WriteString(seq)
				col += width
			case id != "":
				stack = append(stack, open{id, col})
			case len(stack) > 0:
				o := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if col > o.x0 {
					spans = append(spans, zoneSpan{id: o.id, line: li, x0: o.x0, x1: col, depth: len(stack)})
				}
			}
		}
		for d, o := range stack {
			if col > o.x0 {
				spans = append(spans, zoneSpan{id: o.id, line: li, x0: o.x0, x1: col, depth: d})
			}
		}
		lines[li] = b.String()
	}
	return strings.Join(lines, "\n"), spans
}

// paneLayer is content placed at x, y on the screen, with a child layer for
// each zone in it, above the content it covers.
func paneLayer(content string, x, y int) *lipgloss.Layer {
	clean, spans := stripZones(content)
	l := lipgloss.NewLayer(clean).X(x).Y(y)
	if len(spans) == 0 {
		return l
	}
	lines := strings.Split(clean, "\n")
	for _, s := range spans {
		l.AddLayers(lipgloss.NewLayer(ansi.Cut(lines[s.line], s.x0, s.x1)).
			ID(s.id).X(s.x0).Y(s.line).Z(1 + s.depth))
	}
	return l
}

// renderFrame draws laid-out layers to text, measuring characters as
// lipgloss does when laying them out (by grapheme), so every line is as wide
// on the canvas as it was laid out. Compositor.Render counts rune by rune,
// which splits emoji sequences in crew output and pushes lines past the
// edge.
func renderFrame(f *lipgloss.Compositor) string {
	b := f.Bounds()
	scr := uv.NewScreenBuffer(b.Max.X, b.Max.Y)
	scr.Method = ansi.GraphemeWidth
	f.Draw(scr, scr.Bounds())
	return uv.TrimSpace(scr.Render())
}

// deckClickMsg is a left click on a zone, found by View.OnMouse.
type deckClickMsg struct{ id string }

// clickHandler hit-tests left clicks against a laid-out frame.
func clickHandler(f *lipgloss.Compositor) func(tea.MouseMsg) tea.Cmd {
	return func(msg tea.MouseMsg) tea.Cmd {
		click, ok := msg.(tea.MouseClickMsg)
		if !ok || click.Button != tea.MouseLeft {
			return nil
		}
		hit := f.Hit(click.X, click.Y)
		if hit.Empty() {
			return nil
		}
		id := hit.ID()
		return func() tea.Msg { return deckClickMsg{id: id} }
	}
}

// handleClick does what clicking a zone means.
func (m deckModel) handleClick(id string) (tea.Model, tea.Cmd) {
	if m.showHelp || m.stopping {
		return m, nil
	}
	kind, arg, _ := strings.Cut(id, ":")
	if m.mode == deckModePick {
		if i, err := strconv.Atoi(arg); kind == zonePick && err == nil && m.pick != nil && i >= 0 && i < len(m.pick.items) {
			p := m.pick
			m.mode, m.pick = deckModeNormal, nil
			return m, p.choose(i)
		}
		return m, nil
	}
	if m.mode != deckModeNormal {
		return m, nil
	}
	switch kind {
	case zoneRow:
		for i, r := range m.rows {
			if r.key(m.data) == arg {
				m.cursor = i
			}
		}
	case zoneMission:
		m.openMission(arg)
	case zoneTab:
		n, err := strconv.Atoi(arg)
		if r, ok := m.selected(); ok && err == nil && r.kind == deckRowMission && n >= 0 && n < len(missionTabNames) {
			m.missionTab = missionTab(n)
		}
	case zoneAsk:
		if m.onOverview() {
			return m.askWhatToDo()
		}
	}
	return m, nil
}
