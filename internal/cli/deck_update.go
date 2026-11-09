package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/workspace"
)

func (m deckModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizePanes()
		return m, nil

	case tea.FocusMsg:
		m.windowFocused = true
		return m, nil

	case tea.BlurMsg:
		m.windowFocused = false
		return m, nil

	case deckLaunchReadyMsg:
		return m.finishLaunch(msg)

	case deckLaunchWithMsg:
		return m.startCrew(msg.harness)

	case deckLaunchChoiceMsg:
		return m.handleLaunchChoice(msg)

	case deckFlagChoiceMsg:
		return m.handleFlagChoice(msg)

	case deckZoomDoneMsg:
		return m.handleZoomDone(msg)

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
			r.term.Paste(msg.Content) // sent with enter, which clears what the crew said
			return m, nil
		}

	case deckLoadedMsg:
		m.loaded = true
		m.err = msg.err
		if msg.err == nil {
			if m.afterBriefing != nil {
				m.landAfterBriefing(msg.data)
			}
			m.setData(msg.data)
			return m, m.announceSignals()
		}
		return m, nil

	case deckPRsMsg:
		m.prs = msg.prs
		return m, loadDeckCmd(m.ws, m.prs)

	case deckActivityTickMsg:
		return m.handleActivityTick()

	case deckRecordsMsg:
		return m.handleRecords(msg)

	case deckAdoptMsg:
		return m.handleAdopt(msg)

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

	case deckClickMsg: // a click on a zone, found by the view (deck_zones.go)
		return m.handleClick(msg.id)

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
	k := m.keys()
	if k.Interrupt.matches(msg) && m.mode != deckModeConfirm {
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
		if k.CloseHelp.matches(msg) {
			m.showHelp = false
		}
		return m, nil
	}

	m.flash = ""
	switch {
	case k.Quit.matches(msg):
		return m.requestQuit()
	case k.Help.matches(msg):
		m.showHelp = true
	case k.Tabs.matches(msg):
		if r, ok := m.selected(); ok && r.kind == deckRowMission {
			m.missionTab = missionTab(msg.String()[0] - '1')
		}
	case k.Down.matches(msg):
		m.moveCursor(1)
	case k.Up.matches(msg):
		m.moveCursor(-1)
	case k.Top.matches(msg):
		m.cursor = 0
	case k.Bottom.matches(msg):
		m.cursor = max(len(m.rows)-1, 0)
	case k.ScrollDown.matches(msg):
		m.syncMain()
		m.mainVP.ScrollDown(3)
	case k.ScrollUp.matches(msg):
		m.syncMain()
		m.mainVP.ScrollUp(3)
	case k.HalfDown.matches(msg):
		m.syncMain()
		m.mainVP.HalfPageDown()
	case k.HalfUp.matches(msg):
		m.syncMain()
		m.mainVP.HalfPageUp()
	case k.Open.matches(msg):
		r, ok := m.selected()
		switch {
		case !ok:
		case r.kind == deckRowOverview:
			return m.askWhatToDo()
		case r.kind == deckRowPastHeader:
			m.pastOpen = !m.pastOpen
			m.rebuildRows()
		case r.kind == deckRowLooseHeader:
			m.looseOpen = !m.looseOpen
			m.rebuildRows()
		case r.kind == deckRowCrew:
			m.focusTerm = true
		case r.kind == deckRowMission && msg.String() == "enter":
			return m.launchOrOffer()
		}
	case k.Focus.matches(msg):
		if r, ok := m.crewToFocus(); ok {
			m.focusRun(r)
		}
	case k.ZoomToggle.matches(msg):
		// Zoom straight in; coming back out lands in its terminal.
		if r, ok := m.crewToFocus(); ok {
			m.focusRun(r)
			return m.zoom(r)
		}
	case k.NextCrew.matches(msg):
		return m.cycleRun(1)
	case k.PrevCrew.matches(msg):
		return m.cycleRun(-1)
	case k.Launch.matches(msg):
		return m.chooseHarness()
	case k.Resume.matches(msg):
		return m.resumeCrew()
	case k.Stop.matches(msg):
		return m.stopSelected()
	case k.Mouse.matches(msg):
		return m.toggleMouse()
	case k.Dismiss.matches(msg):
		// One thing at a time, the flag first: answering a flag mustn't
		// throw away queued crew you haven't seen.
		if dm, ok := m.selectedMission(); ok && (dm.M.Flag != nil || dm.M.Dispatch != nil) {
			store, slug, flag := m.store, dm.M.Slug, dm.M.Flag != nil
			return m, func() tea.Msg {
				store.UserActed(slug)
				if flag {
					store.SetFlag(slug, "", "")
					return deckReloadMsg{flash: "flag dismissed"}
				}
				store.QueueDispatch(slug, nil)
				return deckReloadMsg{flash: "queued crew dismissed"}
			}
		}
	case k.Sidebar.matches(msg):
		m.sidebarHidden = !m.sidebarHidden
		m.resizePanes()
	case k.Past.matches(msg):
		m.pastOpen = !m.pastOpen
		m.rebuildRows()
	case k.Refresh.matches(msg):
		m.flash = "refreshing…"
		return m, tea.Batch(loadDeckCmd(m.ws, m.prs), fetchDeckPRsCmd(m.ws, m.gh))
	case k.New.matches(msg):
		// Say what the mission is and a briefing crew sets it up.
		return m.askWhatToDo()
	case k.NewNamed.matches(msg):
		// Name it and write the brief yourself.
		m.mode = deckModeNewTitle
		m.newBriefing = false
		m.input.SetValue("")
		m.input.CharLimit = 120
		m.input.Placeholder = "Mission title"
		return m, m.input.Focus()
	case k.Edit.matches(msg):
		return m.editBrief()
	case k.Web.matches(msg):
		path := "/"
		if dm, ok := m.selectedMission(); ok {
			path = "/missions/" + dm.M.Slug
		}
		m.flash = "opening the web UI…"
		return m, openWebCmd(m.ws.Root, path)
	case k.Review.matches(msg):
		return m.review()
	case k.Attach.matches(msg):
		return m.startAttach()
	case k.Detach.matches(msg):
		return m.startDetach()
	case k.Park.matches(msg):
		return m.togglePark()
	case k.Archive.matches(msg):
		return m.toggleArchive()
	}
	return m, nil
}

// askWhatToDo opens the "what do you want to do?" field: the answer becomes
// a mission, and a briefing crew sets it up.
func (m deckModel) askWhatToDo() (tea.Model, tea.Cmd) {
	m.mode = deckModeNewTitle
	m.newBriefing = true
	m.input.SetValue("")
	m.input.CharLimit = 1000
	m.input.Placeholder = "an idea, or an issue like 'fe issue 45'"
	return m, m.input.Focus()
}

// crewToFocus is the selected crew, or the selected mission's latest live
// crew.
func (m deckModel) crewToFocus() (*deckCrewRun, bool) {
	if r, ok := m.selectedRun(); ok {
		return r, true
	}
	if dm, ok := m.selectedMission(); ok {
		if runs := m.reg.live(dm.M.Slug); len(runs) > 0 {
			return runs[len(runs)-1], true
		}
	}
	return nil, false
}

func (m *deckModel) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.rows)-1)
}

// --- new mission ---

func (m deckModel) handleTitleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys()
	switch {
	case k.Discard.matches(msg):
		m.mode, m.answering = deckModeNormal, false
		m.input.Blur()
		return m, nil
	case k.Submit.matches(msg):
		title := strings.TrimSpace(m.input.Value())
		m.mode = deckModeNormal
		m.input.Blur()
		if title == "" {
			m.answering = false
			return m, nil
		}
		if m.answering {
			m.answering = false
			if dm, ok := m.selectedMission(); ok && dm.M.Flag != nil {
				return m, m.answerFlag(dm.M.Slug, title)
			}
			return m, nil
		}
		if m.newBriefing {
			created, err := m.store.CreateBriefing(title)
			if err != nil {
				m.flash = "create failed: " + err.Error()
				return m, nil
			}
			m.wantRow("m:" + created.Slug)
			next, launch := m.launchBriefing(created, "", m.roles.ForBriefing().Harness)
			return next, tea.Batch(launch, loadDeckCmd(m.ws, m.prs))
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
	dm, ok := m.selectedMission()
	if !ok || len(dm.Capsules) == 0 {
		m.flash = "select a mission with capsules to detach one"
		return m, nil
	}
	caps := dm.Capsules
	labels := make([]string, len(caps))
	for i, c := range caps {
		labels[i] = c.Repo + "/" + c.Name
	}
	ws, slug := m.ws, dm.M.Slug
	m.openPick("Detach from "+slug+" (the capsule is kept)", labels, func(i int) tea.Cmd {
		c := caps[i]
		return func() tea.Msg {
			if _, err := detachCapsule(ws, slug, c.Repo, c.Name); err != nil {
				return deckReloadMsg{flash: "detach failed: " + err.Error()}
			}
			return deckReloadMsg{flash: fmt.Sprintf("%s/%s detached", c.Repo, c.Name)}
		}
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
	p, k := m.pick, m.keys()
	switch {
	case k.Cancel.matches(msg):
		m.mode, m.pick = deckModeNormal, nil
	case k.PickDown.matches(msg):
		p.cursor = min(p.cursor+1, len(p.items)-1)
	case k.PickUp.matches(msg):
		p.cursor = max(p.cursor-1, 0)
	case k.Choose.matches(msg):
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
	if m.keys().Yes.matches(msg) {
		return m, c.yes
	}
	return m, nil
}
