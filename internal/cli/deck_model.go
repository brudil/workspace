package cli

import (
	"fmt"
	"github.com/brudil/workspace/internal/crew"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/workspace"
)

// deckRefreshEvery is how often the deck re-reads missions and git state.
// PRs are only fetched at start and on an explicit refresh.
const deckRefreshEvery = 10 * time.Second

type deckRowKind int

const (
	deckRowOverview deckRowKind = iota
	deckRowMission
	deckRowCrew
	deckRowPastHeader
	deckRowLooseHeader
	deckRowLoose
)

// deckRow is one selectable line of the sidebar.
type deckRow struct {
	kind    deckRowKind
	mission int // index into data.Missions (mission and crew rows)
	capsule int // index into data.Loose
	run     *deckCrewRun
	// status is where the mission stood when the rows were built, so its
	// heading and its label always agree.
	status deckStatus
}

// key identifies a row across reloads so the cursor stays put.
func (r deckRow) key(d deckData) string {
	switch r.kind {
	case deckRowOverview:
		return "overview"
	case deckRowMission:
		return "m:" + d.Missions[r.mission].M.Slug
	case deckRowPastHeader:
		return "past"
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
	// roles is which harness and model crew and briefing agents launch
	// with (the zero value means the defaults).
	roles harness.Roles

	width, height int

	data   deckData
	prs    prIndex
	loaded bool
	err    error

	rows      []deckRow
	cursor    int
	cursorKey string // row key to restore after reload

	sidebarHidden bool
	pastOpen      bool // the Past entry is expanded
	looseOpen     bool
	showHelp      bool

	mode        deckMode
	newBriefing bool // the title input is asking for a briefing request
	answering   bool // the title input is your answer to the selected mission's flag
	// missionTab is the mission view's tab; it stays as you move between
	// missions.
	missionTab missionTab
	// orchestratorOff is [orchestrator] enabled = false: answers to flags
	// aren't handed to it.
	orchestratorOff bool
	input           textinput.Model
	pick            *deckPick
	confirm         *deckConfirm

	flash string

	// Crew: where they run, every run so far, and launches in progress.
	host       crew.Host
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

	ticks int // activity ticks so far (records re-read every few)

	// wheelScrolled marks scroll mode the wheel started, rather than
	// ctrl+g [: any key returns to the live screen and goes on as usual.
	wheelScrolled bool
	// mouseOff stops the deck capturing the mouse anywhere (M), so the
	// terminal's own selection, copy and link-clicking work throughout.
	mouseOff bool

	mainVP  viewport.Model
	mainKey string // what the main pane shows; scroll resets when it changes
	md      *markdownCache
}

func newDeckModel(ws *workspace.Workspace, gh github.Client, focus string) deckModel {
	ti := textinput.New()
	ti.Placeholder = "Mission title"
	ti.CharLimit = 120
	store := mission.Store{Root: ws.Root}
	m := deckModel{
		ws:     ws,
		gh:     gh,
		store:  store,
		prs:    prIndex{},
		input:  ti,
		mainVP: viewport.New(),
		md:     newMarkdownCache(),
		// Crew run in this process until the deck picks the server (deck.go).
		host:      &crew.LocalHost{Store: store, OnEnded: orchestrateHook(ws.Root, store)},
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
	m.syncActivity() // groups and badges read what crew reported
	key := m.cursorKey
	if key == "" && m.cursor < len(m.rows) {
		key = m.rows[m.cursor].key(m.data)
	}

	// Overview first, then missions by what they ask of you, each with its
	// live crew. Past missions and loose capsules wait behind an entry row.
	m.rows = append(m.rows[:0], deckRow{kind: deckRowOverview})
	var groups [4][]deckRow
	for i := range m.data.Missions {
		st := m.missionStatus(&m.data.Missions[i])
		groups[st.group] = append(groups[st.group], deckRow{kind: deckRowMission, mission: i, status: st})
	}
	addMissions := func(rows []deckRow) {
		slices.SortStableFunc(rows, func(a, b deckRow) int { return a.status.rank - b.status.rank })
		for _, r := range rows {
			m.rows = append(m.rows, r)
			for _, run := range m.reg.live(m.data.Missions[r.mission].M.Slug) {
				m.rows = append(m.rows, deckRow{kind: deckRowCrew, mission: r.mission, run: run, status: r.status})
			}
		}
	}
	for g := groupNeedsYou; g < groupPast; g++ {
		addMissions(groups[g])
	}
	if len(groups[groupPast]) > 0 {
		m.rows = append(m.rows, deckRow{kind: deckRowPastHeader})
		if m.pastOpen {
			addMissions(groups[groupPast])
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
// of its crew).
func (m deckModel) selectedMission() (*deckMission, bool) {
	r, ok := m.selected()
	if !ok || (r.kind != deckRowMission && r.kind != deckRowCrew) {
		return nil, false
	}
	return &m.data.Missions[r.mission], true
}

// counts is the header's summary: what needs you and what's moving.
func (m deckModel) counts() string {
	n := m.groupCounts()
	var parts []string
	if n[groupNeedsYou] > 0 {
		parts = append(parts, fmt.Sprintf("%d need you", n[groupNeedsYou]))
	}
	if n[groupInProgress] > 0 {
		parts = append(parts, fmt.Sprintf("%d in progress", n[groupInProgress]))
	}
	if len(parts) == 0 {
		return "all quiet"
	}
	return strings.Join(parts, " · ")
}

func rowKeyFound(rows []deckRow, d deckData, key string) bool {
	for _, r := range rows {
		if r.key(d) == key {
			return true
		}
	}
	return false
}
