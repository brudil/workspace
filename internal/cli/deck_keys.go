package cli

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/ui"
)

// Every key the deck answers is declared here, once: the keys that run a
// command, the footer's short label (the binding's help), and the help
// screen's explanation. Matching, the footers and the help screen all read
// these, so they can't disagree.

// deckKey is one deck command.
type deckKey struct {
	key.Binding
	// about is its line on the help screen; empty keeps it off.
	about string
	// longKey labels it on the help screen when that says more than the
	// footer does ("j/k ↑/↓" for "j/k").
	longKey string
}

func newKey(label, short, about string, keys ...string) deckKey {
	return deckKey{Binding: key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, short)), about: about}
}

// pair is one entry for two commands that go opposite ways (j and k): it
// matches either, for help and footers only.
func pair(a, b deckKey, label, short, about string) deckKey {
	return newKey(label, short, about, append(a.Keys(), b.Keys()...)...)
}

// as is the same keys under another label, for a footer or help line that
// describes them where you are ("⏎ start a mission" on the overview).
func (k deckKey) as(label, short string) deckKey {
	k.SetHelp(label, short)
	return k
}

// matches reports whether msg is one of its keys.
func (k deckKey) matches(msg tea.KeyPressMsg) bool { return key.Matches(msg, k.Binding) }

// helpKey is its label on the help screen.
func (k deckKey) helpKey() string {
	if k.longKey != "" {
		return k.longKey
	}
	return k.Help().Key
}

// deckKeyMap is the deck's keys, grouped by where you are.
type deckKeyMap struct {
	// Anywhere.
	Interrupt deckKey // ctrl+c: quit, asking first if crew run

	// The sidebar and main pane.
	Down, Up, Move        deckKey
	Top, Bottom           deckKey
	ScrollDown, ScrollUp  deckKey
	HalfDown, HalfUp      deckKey
	Scroll                deckKey // J/K, for help
	Tabs                  deckKey
	Open, OpenPast        deckKey // ⏎ (space too, which never launches)
	Focus                 deckKey
	NextCrew, PrevCrew    deckKey
	Launch, Resume, Stop  deckKey
	Dismiss, Mouse        deckKey
	New, NewNamed, Edit   deckKey
	Attach, Detach        deckKey
	Park, Archive, Past   deckKey
	Sidebar, Refresh      deckKey
	Help, CloseHelp, Quit deckKey
	PrefixHelp            deckKey // the prefix, explained on the help screen

	// In a crew terminal, after the prefix key.
	Prefix                   deckKey // ctrl+g itself: the next key is the deck's
	SendPrefix               deckKey // ctrl+g again: send it to the crew
	Back, ZoomIn, History    deckKey
	CrewNext, CrewPrev, Crew deckKey
	CrewSidebar, CrewMouse   deckKey
	CrewStop, CrewHelp       deckKey
	CrewQuit                 deckKey

	// Scrolled back through a crew's history.
	LineUp, LineDown, Line      deckKey
	PageUp, PageDown, Page      deckKey
	Oldest, Newest, Leave, Live deckKey

	// A picker, a y/n question and a text field.
	PickDown, PickUp, PickMove deckKey
	Choose, Cancel             deckKey
	Yes                        deckKey
	Submit, Discard            deckKey
}

// keys is the deck's key map. Some help lines name the harness crew and
// briefing agents launch with.
func (m deckModel) keys() deckKeyMap {
	k := deckKeyMap{
		Interrupt: newKey("ctrl+c", "quit", "", "ctrl+c"),

		Down:       newKey("j", "down", "", "j", "down"),
		Up:         newKey("k", "up", "", "k", "up"),
		Top:        newKey("g", "top", "", "g", "home"),
		Bottom:     newKey("G", "bottom", "", "G", "end"),
		ScrollDown: newKey("J", "scroll down", "", "J", "shift+down"),
		ScrollUp:   newKey("K", "scroll up", "", "K", "shift+up"),
		HalfDown:   newKey("ctrl+d", "half page down", "", "ctrl+d", "pgdown"),
		HalfUp:     newKey("ctrl+u", "half page up", "", "ctrl+u", "pgup"),
		Tabs:       newKey("1–4", "tabs", "Mission tabs: Summary (what needs you), Brief, Work (capsules, artifacts), Log (and crew); or click one", "1", "2", "3", "4"),
		Open: newKey("⏎", "crew", "Launch crew ("+m.roles.ForCrew().Harness+") on the mission, focus its live crew, or answer its question",
			"enter", "space"),
		Focus:    newKey("tab", "focus", "Focus the mission's live crew terminal", "tab", "l", "right"),
		NextCrew: newKey("]", "next crew", "", "]"),
		PrevCrew: newKey("[", "previous crew", "", "["),
		Launch:   newKey("L", "launch", "Launch crew with a chosen harness", "L"),
		Resume:   newKey("R", "resume", "Resume the mission's last crew conversation", "R"),
		Stop:     newKey("S", "stop", "Stop the mission's live crew", "S"),
		Dismiss:  newKey("F", "dismiss", "Dismiss the orchestrator's question, then any queued crew", "F"),
		Mouse: newKey("M", "mouse", "Mouse: let the terminal select text and open links without Shift (the wheel won't scroll; again: the deck takes the mouse back)",
			"M"),
		New: newKey("n", "new", "New mission: say what you want (⏎ on the overview too); a briefing agent ("+m.roles.ForBriefing().Harness+") writes the brief and lifts capsules",
			"n"),
		NewNamed:  newKey("N", "new, named", "New mission you name and brief yourself (in $EDITOR)", "N"),
		Edit:      newKey("e", "brief", "Edit the selected mission's brief in $EDITOR", "e"),
		Attach:    newKey("a", "attach", "Attach: a loose capsule to this mission, or this loose capsule to a mission", "a"),
		Detach:    newKey("d", "detach", "Detach one of the mission's capsules (it's kept)", "d"),
		Park:      newKey("p", "park", "Park / reopen the selected mission", "p"),
		Archive:   newKey("x", "archive", "Archive / reopen the selected mission", "x"),
		Past:      newKey("A", "past", "Show or hide past missions (landed and archived; also ⏎ on Past)", "A"),
		Sidebar:   newKey("s", "sidebar", "Toggle the sidebar", "s"),
		Refresh:   newKey("r", "refresh", "Refresh (including PRs)", "r"),
		Help:      newKey("?", "help", "Toggle this help", "?"),
		CloseHelp: newKey("?", "close", "", "?", "esc", "q"),
		Quit:      newKey("q", "quit", "Quit", "q"),

		Prefix:      newKey(deckPrefixKey, "deck commands", "", deckPrefixKey),
		SendPrefix:  newKey(deckPrefixKey, "send it", "", deckPrefixKey),
		Back:        newKey("tab", "sidebar", "", "tab", "esc", "h", "left"),
		History:     newKey("[", "history", "", "[", "pgup"),
		ZoomIn:      newKey("z", "zoom", "", "z"),
		CrewMouse:   newKey("m", "mouse", "", "m"),
		CrewNext:    newKey("n", "next crew", "", "n", "]"),
		CrewPrev:    newKey("p", "previous crew", "", "p"),
		CrewSidebar: newKey("s", "sidebar", "", "s"),
		CrewStop:    newKey("S", "stop", "", "S"),
		CrewQuit:    newKey("q", "quit", "", "q"),
		CrewHelp:    newKey("?", "help", "", "?"),

		LineUp:   newKey("k", "line up", "", "k", "up"),
		LineDown: newKey("j", "line down", "", "j", "down"),
		PageUp:   newKey("ctrl+u", "half page up", "", "ctrl+u", "pgup", "b"),
		PageDown: newKey("ctrl+d", "half page down", "", "ctrl+d", "pgdown", "space", "f"),
		Oldest:   newKey("g", "top", "", "g", "home"),
		Newest:   newKey("G", "newest", "", "G", "end"),
		Leave:    newKey("esc", "back to live", "", "esc", "q", "enter", deckPrefixKey),

		PickDown: newKey("j", "down", "", "j", "down"),
		PickUp:   newKey("k", "up", "", "k", "up"),
		Choose:   newKey("⏎", "choose", "", "enter"),
		Cancel:   newKey("esc", "cancel", "", "esc", "q"),
		Yes:      newKey("y/n", "", "", "y", "Y"),
		Submit:   newKey("⏎", "start it", "", "enter"),
		Discard:  newKey("esc", "cancel", "", "esc"),
	}
	k.Move = pair(k.Down, k.Up, "j/k", "move", "Move through the overview, missions and their crew")
	k.Move.longKey = "j/k ↑/↓"
	k.Scroll = pair(k.ScrollDown, k.ScrollUp, "J/K", "scroll", "Scroll the main pane (also ctrl+d/u, mouse wheel)")
	k.OpenPast = k.Open.as("⏎ (loose)", "show")
	k.OpenPast.about = "Show or hide capsules that belong to no mission"
	k.PrefixHelp = k.Prefix
	k.PrefixHelp.about = "In a crew terminal: then tab returns here, s hides the sidebar, S stops crew"
	k.Crew = pair(k.CrewNext, k.CrewPrev, "n/p", "next/prev crew", "")
	k.Line = pair(k.LineUp, k.LineDown, "k/j", "line", "")
	k.Page = pair(k.PageUp, k.PageDown, "ctrl+u/d", "half page", "")
	k.Live = pair(k.Newest, k.Leave, "G/esc", "back to live", "")
	k.PickMove = pair(k.PickDown, k.PickUp, "j/k", "move", "")
	return k
}

// helpScreen is the help screen's entries, in order.
func (k deckKeyMap) helpScreen() []deckKey {
	return []deckKey{
		k.Move, k.Open, k.Launch, k.Resume, k.Focus, k.PrefixHelp, k.Stop, k.Dismiss, k.Mouse,
		k.Tabs, k.Scroll, k.New, k.NewNamed, k.Edit, k.Attach, k.Detach, k.Park, k.Archive,
		k.Past, k.OpenPast, k.Sidebar, k.Refresh, k.Help, k.Quit,
	}
}

// footer is the sidebar's hints; the overview has fewer.
func (k deckKeyMap) footer(overview bool) []key.Binding {
	if overview {
		return bindings(k.Open.as("⏎", "start a mission"), k.Move, k.Help, k.Quit)
	}
	return bindings(k.Move, k.Open, k.Focus, k.New, k.Edit, k.Attach, k.Park, k.Resume, k.Stop, k.Help, k.Quit)
}

// prefixFooter is what can follow the prefix key in a crew terminal.
func (k deckKeyMap) prefixFooter() []key.Binding {
	return bindings(k.Back, k.History, k.ZoomIn, k.CrewMouse, k.Crew, k.CrewSidebar, k.CrewStop, k.CrewQuit, k.CrewHelp, k.SendPrefix)
}

func (k deckKeyMap) scrollFooter() []key.Binding {
	return bindings(k.Line, k.Page, k.Oldest, k.Live)
}

func (k deckKeyMap) pickFooter() []key.Binding {
	return bindings(k.PickMove, k.Choose, k.Cancel)
}

func (k deckKeyMap) fieldFooter() []key.Binding {
	return bindings(k.Submit, k.Discard)
}

func bindings(keys ...deckKey) []key.Binding {
	out := make([]key.Binding, len(keys))
	for i, k := range keys {
		out[i] = k.Binding
	}
	return out
}

// hintText is bindings as plain text, for text styled as a whole (inside
// the overview's box).
func hintText(bs []key.Binding, sep string) string {
	parts := make([]string, len(bs))
	for i, b := range bs {
		parts[i] = b.Help().Key + " " + b.Help().Desc
	}
	return strings.Join(parts, sep)
}

// hints renders bindings on one line, dimmed, separated by sep and cut to
// width with an ellipsis.
func hints(bs []key.Binding, sep string, width int) string {
	h := help.New()
	h.ShortSeparator = sep
	h.Styles.ShortKey, h.Styles.ShortDesc = ui.Dim, ui.Dim
	h.Styles.ShortSeparator, h.Styles.Ellipsis = ui.Dim, ui.Dim
	h.SetWidth(width)
	return h.ShortHelpView(bs)
}
