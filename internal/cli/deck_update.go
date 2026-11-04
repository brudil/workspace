package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/workspace"
)

func (m deckModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizePanes()
		return m, nil

	case deckLaunchReadyMsg:
		return m.finishLaunch(msg)

	case deckLaunchWithMsg:
		return m.startCrew(msg.harness)

	case deckLaunchNowMsg:
		m.launching[msg.slug] = true
		m.flash = "launching " + msg.harness + " on " + msg.slug + "…"
		return m, msg.launch

	case deckPaneMsg:
		return m.handlePaneMsg(msg)

	case deckQuitConfirmedMsg:
		return m.stopAll()

	case deckStoppedMsg:
		return m, tea.Quit

	case tea.PasteMsg:
		if r, ok := m.focusedRun(); ok && m.mode == deckModeNormal {
			r.scroll = 0
			r.term.Paste(msg.Content)
			return m, nil
		}

	case deckLoadedMsg:
		m.loaded = true
		m.err = msg.err
		if msg.err == nil {
			m.setData(msg.data)
		}
		return m, nil

	case deckPRsMsg:
		m.prs = msg.prs
		return m, loadDeckCmd(m.ws, m.prs)

	case deckTickMsg:
		return m, tea.Batch(loadDeckCmd(m.ws, m.prs), deckTick())

	case deckReloadMsg:
		if msg.flash != "" {
			m.flash = msg.flash
		}
		return m, loadDeckCmd(m.ws, m.prs)

	case deckFlashMsg:
		m.flash = string(msg)
		return m, nil

	case tea.MouseWheelMsg:
		return m.handleWheel(msg)

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && m.mode == deckModeNormal {
			if idx, ok := m.sidebarRowAt(msg.Mouse().X, msg.Mouse().Y); ok {
				m.cursor = idx
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	if m.mode == deckModeNewTitle {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m deckModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.stopping {
		return m, nil
	}
	if m.focusTerm && m.mode == deckModeNormal && !m.showHelp {
		return m.handleTermKey(msg)
	}
	if msg.String() == "ctrl+c" && m.mode != deckModeConfirm {
		return m.requestQuit()
	}
	switch m.mode {
	case deckModeNewTitle:
		return m.handleTitleKey(msg)
	case deckModePick:
		return m.handlePickKey(msg)
	case deckModeConfirm:
		return m.handleConfirmKey(msg)
	}

	if m.showHelp {
		if k := msg.String(); k == "?" || k == "esc" || k == "q" {
			m.showHelp = false
		}
		return m, nil
	}

	m.flash = ""
	switch msg.String() {
	case "q":
		return m.requestQuit()
	case "?":
		m.showHelp = true
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = max(len(m.rows)-1, 0)
	case "J", "shift+down":
		m.syncMain()
		m.mainVP.ScrollDown(3)
	case "K", "shift+up":
		m.syncMain()
		m.mainVP.ScrollUp(3)
	case "ctrl+d", "pgdown":
		m.syncMain()
		m.mainVP.HalfPageDown()
	case "ctrl+u", "pgup":
		m.syncMain()
		m.mainVP.HalfPageUp()
	case "enter", "space":
		r, ok := m.selected()
		if ok && r.kind == deckRowLooseHeader {
			m.looseOpen = !m.looseOpen
			m.rebuildRows()
		} else if ok && r.kind == deckRowCrew {
			m.focusTerm = true
		} else if ok && (r.kind == deckRowMission || r.kind == deckRowCapsule) && msg.String() == "enter" {
			return m.startCrew(harness.Default())
		}
	case "tab", "l", "right":
		if r, ok := m.selectedRun(); ok {
			m.focusRun(r)
		} else if dm, ok := m.selectedMission(); ok {
			if runs := m.reg.live(dm.M.Slug); len(runs) > 0 {
				m.focusRun(runs[len(runs)-1])
			}
		}
	case "]":
		return m.cycleRun(1)
	case "[":
		return m.cycleRun(-1)
	case "L":
		return m.chooseHarness()
	case "R":
		return m.resumeCrew()
	case "S":
		return m.stopSelected()
	case "s":
		m.sidebarHidden = !m.sidebarHidden
		m.resizePanes()
	case "A":
		m.showArchived = !m.showArchived
		m.rebuildRows()
		if m.showArchived {
			m.flash = "showing archived missions"
		}
	case "r":
		m.flash = "refreshing…"
		return m, tea.Batch(loadDeckCmd(m.ws, m.prs), fetchDeckPRsCmd(m.ws, m.gh))
	case "n":
		m.mode = deckModeNewTitle
		m.input.SetValue("")
		return m, m.input.Focus()
	case "e":
		return m.editBrief()
	case "a":
		return m.startAttach()
	case "d":
		return m.startDetach()
	case "p":
		return m.togglePark()
	case "x":
		return m.toggleArchive()
	}
	return m, nil
}

func (m *deckModel) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.rows)-1)
}

// --- new mission ---

func (m deckModel) handleTitleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = deckModeNormal
		m.input.Blur()
		return m, nil
	case "enter":
		title := strings.TrimSpace(m.input.Value())
		m.mode = deckModeNormal
		m.input.Blur()
		if title == "" {
			return m, nil
		}
		created, err := m.store.Create(title)
		if err != nil {
			m.flash = "create failed: " + err.Error()
			return m, nil
		}
		m.cursorKey = "m:" + created.Slug
		return m, m.editorFor(created.Slug, "created "+created.Slug)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// --- brief ---

func (m deckModel) editBrief() (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	if !ok {
		m.flash = "select a mission to edit its brief"
		return m, nil
	}
	return m, m.editorFor(dm.M.Slug, "")
}

// editorFor suspends the deck, edits the mission's brief in $EDITOR, then
// reloads.
func (m deckModel) editorFor(slug, flash string) tea.Cmd {
	return tea.ExecProcess(editorCmd(m.store.BriefPath(slug)), func(err error) tea.Msg {
		if err != nil {
			return deckReloadMsg{flash: "editor: " + err.Error()}
		}
		return deckReloadMsg{flash: flash}
	})
}

// --- attach / detach ---

func (m deckModel) startAttach() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	ws := m.ws

	if r.kind == deckRowLoose {
		c := m.data.Loose[r.capsule]
		var slugs, labels []string
		for _, dm := range m.data.Missions {
			if dm.State == mission.Archived {
				continue
			}
			slugs = append(slugs, dm.M.Slug)
			labels = append(labels, dm.M.Slug+"  "+dm.M.Title)
		}
		if len(slugs) == 0 {
			m.flash = "no missions to attach to (n creates one)"
			return m, nil
		}
		m.openPick("Attach "+c.Repo+"/"+c.Name+" to", labels, func(i int) tea.Cmd {
			return deckAttachCmd(ws, slugs[i], c.Repo, c.Name)
		})
		return m, nil
	}

	dm, ok := m.selectedMission()
	if !ok {
		m.flash = "select a mission or a loose capsule to attach"
		return m, nil
	}
	if dm.State == mission.Archived {
		m.flash = "reopen the mission first (p)"
		return m, nil
	}
	if len(m.data.Loose) == 0 {
		m.flash = "no loose capsules to attach (ws lift creates one)"
		return m, nil
	}
	loose := m.data.Loose
	labels := make([]string, len(loose))
	for i, c := range loose {
		labels[i] = c.Repo + "/" + c.Name
	}
	slug := dm.M.Slug
	m.openPick("Attach to "+slug, labels, func(i int) tea.Cmd {
		return deckAttachCmd(ws, slug, loose[i].Repo, loose[i].Name)
	})
	return m, nil
}

func deckAttachCmd(ws *workspace.Workspace, slug, repo, name string) tea.Cmd {
	return func() tea.Msg {
		if err := attachCapsule(ws, slug, repo, name); err != nil {
			return deckReloadMsg{flash: "attach failed: " + err.Error()}
		}
		return deckReloadMsg{flash: fmt.Sprintf("%s/%s → %s", repo, name, slug)}
	}
}

func (m deckModel) startDetach() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok || r.kind != deckRowCapsule {
		m.flash = "select a mission capsule to detach"
		return m, nil
	}
	dm := m.data.Missions[r.mission]
	c := dm.Capsules[r.capsule]
	ws, slug := m.ws, dm.M.Slug
	m.openConfirm(fmt.Sprintf("Detach %s/%s from %s? The capsule is kept.", c.Repo, c.Name, slug), func() tea.Msg {
		if _, err := detachCapsule(ws, slug, c.Repo, c.Name); err != nil {
			return deckReloadMsg{flash: "detach failed: " + err.Error()}
		}
		return deckReloadMsg{flash: fmt.Sprintf("%s/%s detached", c.Repo, c.Name)}
	})
	return m, nil
}

// --- park / archive ---

func (m deckModel) togglePark() (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	if !ok {
		return m, nil
	}
	next := mission.Parked
	if dm.State == mission.Parked || dm.State == mission.Archived {
		next = ""
	}
	return m, deckStateCmd(m.ws, dm.M.Slug, next)
}

func (m deckModel) toggleArchive() (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	if !ok {
		return m, nil
	}
	if dm.State == mission.Archived {
		return m, deckStateCmd(m.ws, dm.M.Slug, "")
	}
	ws, slug := m.ws, dm.M.Slug
	m.openConfirm(fmt.Sprintf("Archive %s? Its capsules are kept.", slug), deckStateCmd(ws, slug, mission.Archived))
	return m, nil
}

func deckStateCmd(ws *workspace.Workspace, slug string, state mission.State) tea.Cmd {
	return func() tea.Msg {
		label, err := setMissionState(ws, slug, state)
		if err != nil {
			return deckReloadMsg{flash: "failed: " + err.Error()}
		}
		return deckReloadMsg{flash: slug + " " + label}
	}
}

// --- picker / confirm ---

func (m *deckModel) openPick(title string, items []string, choose func(int) tea.Cmd) {
	m.mode = deckModePick
	m.pick = &deckPick{title: title, items: items, choose: choose}
}

func (m deckModel) handlePickKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.pick
	switch msg.String() {
	case "esc", "q":
		m.mode, m.pick = deckModeNormal, nil
	case "j", "down":
		p.cursor = min(p.cursor+1, len(p.items)-1)
	case "k", "up":
		p.cursor = max(p.cursor-1, 0)
	case "enter":
		m.mode, m.pick = deckModeNormal, nil
		return m, p.choose(p.cursor)
	}
	return m, nil
}

func (m *deckModel) openConfirm(prompt string, yes tea.Cmd) {
	m.mode = deckModeConfirm
	m.confirm = &deckConfirm{prompt: prompt, yes: yes}
}

func (m deckModel) handleConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	c := m.confirm
	m.mode, m.confirm = deckModeNormal, nil
	if k := msg.String(); k == "y" || k == "Y" {
		return m, c.yes
	}
	return m, nil
}
