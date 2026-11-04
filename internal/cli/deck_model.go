package cli

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/workspace"
)

// deckRefreshEvery is how often the deck re-reads missions and git state.
// PRs are only fetched at start and on an explicit refresh.
const deckRefreshEvery = 10 * time.Second

type deckRowKind int

const (
	deckRowMission deckRowKind = iota
	deckRowCapsule
	deckRowLooseHeader
	deckRowLoose
)

// deckRow is one selectable line of the sidebar.
type deckRow struct {
	kind    deckRowKind
	mission int // index into data.Missions (mission and capsule rows)
	capsule int // index into the mission's Capsules, or into data.Loose
}

// key identifies a row across reloads so the cursor stays put.
func (r deckRow) key(d deckData) string {
	switch r.kind {
	case deckRowMission:
		return "m:" + d.Missions[r.mission].M.Slug
	case deckRowCapsule:
		c := d.Missions[r.mission].Capsules[r.capsule]
		return "c:" + d.Missions[r.mission].M.Slug + ":" + c.Repo + "/" + c.Name
	case deckRowLooseHeader:
		return "loose"
	default:
		c := d.Loose[r.capsule]
		return "l:" + c.Repo + "/" + c.Name
	}
}

type deckMode int

const (
	deckModeNormal deckMode = iota
	deckModeNewTitle
	deckModePick
	deckModeConfirm
)

// deckPick is an open picker: a list of labels and what choosing one does.
type deckPick struct {
	title  string
	items  []string
	cursor int
	choose func(i int) tea.Cmd
}

// deckConfirm is an open y/n prompt.
type deckConfirm struct {
	prompt string
	yes    tea.Cmd
}

type deckModel struct {
	ws    *workspace.Workspace
	gh    github.Client
	store mission.Store

	width, height int

	data   deckData
	prs    prIndex
	loaded bool
	err    error

	rows      []deckRow
	cursor    int
	cursorKey string // row key to restore after reload

	sidebarHidden bool
	showArchived  bool
	looseOpen     bool
	showHelp      bool

	mode    deckMode
	input   textinput.Model
	pick    *deckPick
	confirm *deckConfirm

	flash string

	mainVP  viewport.Model
	mainKey string // what the main pane shows; scroll resets when it changes
	md      *markdownCache
}

func newDeckModel(ws *workspace.Workspace, gh github.Client, focus string) deckModel {
	ti := textinput.New()
	ti.Placeholder = "Mission title"
	ti.CharLimit = 120
	m := deckModel{
		ws:     ws,
		gh:     gh,
		store:  mission.Store{Root: ws.Root},
		prs:    prIndex{},
		input:  ti,
		mainVP: viewport.New(),
		md:     newMarkdownCache(),
	}
	if focus != "" {
		m.cursorKey = "m:" + focus
	}
	return m
}

type deckTickMsg struct{}

type deckFlashMsg string

// deckReloadMsg asks for a reload, optionally with a status line.
type deckReloadMsg struct{ flash string }

func deckTick() tea.Cmd {
	return tea.Tick(deckRefreshEvery, func(time.Time) tea.Msg { return deckTickMsg{} })
}

func (m deckModel) Init() tea.Cmd {
	return tea.Batch(loadDeckCmd(m.ws, m.prs), fetchDeckPRsCmd(m.ws, m.gh), deckTick())
}

// setData swaps in a new snapshot, keeping the cursor on the same row. The
// cursor's key must be taken from the old data: row indices don't carry over.
func (m *deckModel) setData(d deckData) {
	if m.cursorKey == "" && m.cursor < len(m.rows) {
		m.cursorKey = m.rows[m.cursor].key(m.data)
	}
	m.data = d
	m.rebuildRows()
}

// rebuildRows recomputes the sidebar rows from data and keeps the cursor on
// the same row (by key) where possible. Rows must match m.data on entry.
func (m *deckModel) rebuildRows() {
	key := m.cursorKey
	if key == "" && m.cursor < len(m.rows) {
		key = m.rows[m.cursor].key(m.data)
	}

	m.rows = m.rows[:0]
	for i, dm := range m.data.Missions {
		if dm.State == mission.Archived && !m.showArchived {
			continue
		}
		m.rows = append(m.rows, deckRow{kind: deckRowMission, mission: i})
		for j := range dm.Capsules {
			m.rows = append(m.rows, deckRow{kind: deckRowCapsule, mission: i, capsule: j})
		}
	}
	if len(m.data.Loose) > 0 {
		m.rows = append(m.rows, deckRow{kind: deckRowLooseHeader})
		if m.looseOpen {
			for i := range m.data.Loose {
				m.rows = append(m.rows, deckRow{kind: deckRowLoose, capsule: i})
			}
		}
	}

	m.cursor = min(m.cursor, max(len(m.rows)-1, 0))
	for i, r := range m.rows {
		if r.key(m.data) == key {
			m.cursor = i
			break
		}
	}
	m.cursorKey = ""
}

// selected returns the row under the cursor.
func (m deckModel) selected() (deckRow, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return deckRow{}, false
	}
	return m.rows[m.cursor], true
}

// selectedMission returns the mission the cursor is on (directly or via one
// of its capsules).
func (m deckModel) selectedMission() (*deckMission, bool) {
	r, ok := m.selected()
	if !ok || (r.kind != deckRowMission && r.kind != deckRowCapsule) {
		return nil, false
	}
	return &m.data.Missions[r.mission], true
}

func (m deckModel) counts() string {
	live := 0
	for _, dm := range m.data.Missions {
		if dm.State == mission.Active || dm.State == mission.Review {
			live++
		}
	}
	n := len(m.data.Missions)
	noun := "missions"
	if n == 1 {
		noun = "mission"
	}
	return fmt.Sprintf("%d %s · %d live", n, noun, live)
}
