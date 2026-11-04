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
	deckRowCrew
)

// deckRow is one selectable line of the sidebar.
type deckRow struct {
	kind    deckRowKind
	mission int // index into data.Missions (mission and capsule rows)
	capsule int // index into the mission's Capsules, or into data.Loose
	run     *deckCrewRun
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
	case deckRowCrew:
		return r.run.key()
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

	mode        deckMode
	newBriefing bool // the title input is asking for a briefing request
	input       textinput.Model
	pick        *deckPick
	confirm     *deckConfirm

	flash string

	// Crew: where they run, every run so far, and launches in progress.
	host       crewHost
	reg        *crewRegistry
	launching  map[string]bool
	focusTerm  bool // keys go to the selected crew's terminal
	prefix     bool // the prefix key was pressed; next key is a deck command
	scrollMode bool // keys move through the focused crew's history
	stopping   bool // quitting: stopAll records crew ends

	// afterBriefing is a briefing crew that just ended; the next data load
	// lands on its mission.
	afterBriefing *deckCrewRun

	// keyPending: cursorKey names a row that may not exist until the next
	// data load (see wantRow).
	keyPending bool

	mainVP  viewport.Model
	mainKey string // what the main pane shows; scroll resets when it changes
	md      *markdownCache
}

func newDeckModel(ws *workspace.Workspace, gh github.Client, focus string) deckModel {
	ti := textinput.New()
	ti.Placeholder = "Mission title"
	ti.CharLimit = 120
	m := deckModel{
		ws:        ws,
		gh:        gh,
		store:     mission.Store{Root: ws.Root},
		prs:       prIndex{},
		input:     ti,
		mainVP:    viewport.New(),
		md:        newMarkdownCache(),
		host:      localHost{},
		reg:       &crewRegistry{},
		launching: map[string]bool{},
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
	cmds := []tea.Cmd{loadDeckCmd(m.ws, m.prs), fetchDeckPRsCmd(m.ws, m.gh), deckTick(), activityTick()}
	return tea.Batch(append(cmds, m.adoptHosted()...)...)
}

// setData swaps in a new snapshot, keeping the cursor on the same row. The
// cursor's key must be taken from the old data: row indices don't carry over.
func (m *deckModel) setData(d deckData) {
	if m.cursorKey == "" && m.cursor < len(m.rows) {
		m.cursorKey = m.rows[m.cursor].key(m.data)
	}
	m.data = d
	m.rebuildRows()
	// A wanted row gets one data load to appear; after that the cursor's
	// own row is what's kept.
	if m.keyPending {
		m.keyPending = false
		m.cursorKey = ""
	}
}

// wantRow puts the cursor on the row with this key as soon as it exists.
// Launches and new missions use it: their row only appears once fresh data
// has loaded, which may arrive before or after the launch completes.
func (m *deckModel) wantRow(key string) {
	m.cursorKey = key
	m.keyPending = true
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
		for _, r := range m.reg.live(dm.M.Slug) {
			m.rows = append(m.rows, deckRow{kind: deckRowCrew, mission: i, run: r})
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
	if !m.keyPending || m.rows == nil || rowKeyFound(m.rows, m.data, key) {
		m.cursorKey = ""
	}
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
	if !ok || (r.kind != deckRowMission && r.kind != deckRowCapsule && r.kind != deckRowCrew) {
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

func rowKeyFound(rows []deckRow, d deckData, key string) bool {
	for _, r := range rows {
		if r.key(d) == key {
			return true
		}
	}
	return false
}
