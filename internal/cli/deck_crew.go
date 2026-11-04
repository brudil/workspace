package cli

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/crew"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/ui"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// deckPrefixKey, pressed while a crew terminal has focus, makes the next key
// a deck command instead of input for the agent.
const deckPrefixKey = "ctrl+g"

// deckStopGrace is how long crew get to exit after SIGHUP before SIGKILL.
const deckStopGrace = 3 * time.Second

// deckCrewRun is crew running on a mission.
type deckCrewRun struct {
	slug    string
	crewID  string
	harness string
	capsule string // repo/name it works in
	dir     string
	role    string // "" or mission.RoleBriefing
	term    crew.Term

	// UI state, touched only by Update.
	ended  bool
	exit   string
	scroll int // lines scrolled back into history; 0 is live

	// wasWorking is the activity seen at the last tick, to notice the
	// moment crew goes from working to waiting.
	wasWorking bool
	// notified is the crew status (and when it was set) already announced.
	notified string
}

func (r *deckCrewRun) live() bool { return r != nil && !r.ended }

func (r *deckCrewRun) key() string { return "r:" + r.slug + ":" + r.crewID }

// --- messages ---

// deckLaunchReadyMsg carries a started crew (or why it didn't start) back to
// Update.
type deckLaunchReadyMsg struct {
	slug    string
	harness string
	capsule string
	dir     string
	role    string
	crewID  string
	prompt  string // typed into the agent once it has drawn
	term    crew.Term
	err     error
}

// deckPaneMsg means a crew terminal changed (or its program exited).
type deckPaneMsg struct{ run *deckCrewRun }

// deckStoppedMsg is sent once quitting has stopped every crew.
type deckStoppedMsg struct{}

type deckQuitConfirmedMsg struct{}

type deckLaunchWithMsg struct{ harness string }

// --- selection ---

// selectedRun is the live crew whose row the cursor is on.
func (m deckModel) selectedRun() (*deckCrewRun, bool) {
	r, ok := m.selected()
	if !ok || r.kind != deckRowCrew || !r.run.live() {
		return nil, false
	}
	return r.run, true
}

// focusedRun is the live crew whose terminal has focus, if any.
func (m deckModel) focusedRun() (*deckCrewRun, bool) {
	if !m.focusTerm {
		return nil, false
	}
	return m.selectedRun()
}

// shownRun is the crew whose terminal the main pane shows.
func (m deckModel) shownRun() (*deckCrewRun, bool) {
	if m.mode == deckModePick {
		return nil, false
	}
	return m.selectedRun()
}

// focusRun moves the cursor to a crew's row and gives its terminal focus.
func (m *deckModel) focusRun(r *deckCrewRun) {
	m.wantRow(r.key())
	m.focusTerm = true
}

func (m deckModel) liveCount() int { return len(m.reg.live("")) }

// --- launching ---

// deckCapsules are a mission's capsules crew can work in.
func deckCapsules(dm *deckMission) []crew.Capsule {
	var out []crew.Capsule
	for _, c := range dm.Capsules {
		if c.Exists {
			out = append(out, crew.Capsule{Name: c.Repo + "/" + c.Name, Path: c.Path})
		}
	}
	return out
}

// placeFor picks where crew start on a mission: want (repo/name) if it
// still exists, else the first capsule nobody is working in, else the first
// (with Busy set, to ask before sharing). Busy counts this deck's crew and
// any the mission's records say are running elsewhere.
func (m deckModel) placeFor(dm *deckMission, want string) (crew.Place, bool) {
	busy := crew.BusyIn(dm.M)
	for _, r := range m.reg.live("") {
		busy[r.capsule] = r.crewID
	}
	caps := deckCapsules(dm)
	p, err := crew.PickPlace(caps, busy, want)
	if errors.As(err, new(*crew.UnknownCapsuleError)) {
		p, err = crew.PickPlace(caps, busy, "") // it's gone: anywhere will do
	}
	return p, err == nil
}

// startCrew launches crew with the named harness on the selected mission,
// asking first if every capsule already has crew working in it.
func (m deckModel) startCrew(harnessName string) (tea.Model, tea.Cmd) {
	return m.startCrewWith(harnessName, "", "")
}

// startDispatch launches the crew the orchestrator queued on the selected
// mission, in the capsule it chose if that still exists.
func (m deckModel) startDispatch() (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	if !ok || dm.M.Dispatch == nil {
		return m, nil
	}
	d := dm.M.Dispatch
	return m.startCrewWith(firstNonEmpty(d.Harness, harness.Default()), d.Capsule, d.Text)
}

// startCrewWith launches crew, in capsule (repo/name) if given and it
// exists, else wherever placeFor picks, with a handoff if given.
func (m deckModel) startCrewWith(harnessName, capsule, handoff string) (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	if !ok {
		m.flash = "select a mission to launch crew on"
		return m, nil
	}
	slug := dm.M.Slug
	if m.launching[slug] {
		return m, nil
	}
	if dm.M.State == mission.Briefing {
		return m.launchBriefing(dm.M, dm.Brief, harnessName)
	}
	place, ok := m.placeFor(dm, capsule)
	if !ok {
		m.flash = "no capsule to work in yet: a attaches one, or WS_MISSION=" + slug + " ws lift <repo> <name>"
		return m, nil
	}
	h, err := harness.Get(harnessName)
	if err == nil {
		err = h.Available()
	}
	if err != nil {
		m.flash = err.Error()
		return m, nil
	}
	b := harness.Brief{Slug: slug, Title: dm.M.Title, Brief: dm.Brief, Dir: place.Dir, Handoff: handoff}
	launch := m.launchCmd(crew.Request{Slug: slug, Harness: harnessName, Place: place, Handoff: handoff, By: "you",
		Prepare: func() (harness.Launch, error) { return h.Start(b) }})

	if place.Busy != "" {
		m.openConfirm(fmt.Sprintf("%s is already working in %s. Launch another crew there too?", place.Busy, place.Capsule),
			func() tea.Msg { return deckLaunchNowMsg{slug: slug, harness: harnessName, launch: launch} })
		return m, nil
	}
	m.launching[slug] = true
	m.flash = "launching " + harnessName + " on " + slug + "…"
	return m, launch
}

// deckLaunchNowMsg is a confirmed launch into a shared capsule.
type deckLaunchNowMsg struct {
	slug, harness string
	launch        tea.Cmd
}

// resumableCrew is the mission's most recent crew that recorded a
// conversation and isn't running now.
func (m deckModel) resumableCrew(dm *deckMission) (mission.Crew, bool) {
	live := map[string]bool{}
	for _, r := range m.reg.live(dm.M.Slug) {
		live[r.crewID] = true
	}
	// A crew the orchestrator suggested resuming comes first.
	for i := len(dm.M.Crew) - 1; i >= 0; i-- {
		if c := dm.M.Crew[i]; c.ResumeWith != "" && c.Conversation != "" && !live[c.ID] && !c.Done(dm.M) {
			return c, true
		}
	}
	return dm.M.LastResumableCrew(func(id string) bool { return live[id] })
}

// resumeCrew continues the mission's most recent resumable crew: same crew
// id, same conversation, back in its capsule if that still exists.
func (m deckModel) resumeCrew() (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	if !ok {
		return m, nil
	}
	slug := dm.M.Slug
	if m.launching[slug] {
		return m, nil
	}
	last, ok := m.resumableCrew(dm)
	if !ok {
		m.flash = "no crew on " + slug + " to resume (none recorded a conversation, or they're running)"
		return m, nil
	}
	// Back where it worked if that's still there, whoever else is in it now.
	place, ok := m.placeFor(dm, last.Capsule)
	place.Busy = ""
	if last.Role == mission.RoleBriefing {
		place, ok = crew.Place{Dir: m.ws.Root, Capsule: briefingPlace}, true
	}
	if !ok {
		m.flash = "no capsule left to resume in"
		return m, nil
	}
	h, err := harness.Get(last.Harness)
	if err == nil {
		err = h.Available()
	}
	if err != nil {
		m.flash = err.Error()
		return m, nil
	}
	m.launching[slug] = true
	m.flash = "resuming " + last.ID + " on " + slug + "…"
	return m, m.launchCmd(crew.Request{Slug: slug, Harness: last.Harness, Place: place, Role: last.Role, Resume: last.ID,
		Prepare: func() (harness.Launch, error) { return h.Resume(place.Dir, last.Conversation) }})
}

// launchOrOffer is ⏎ on a mission: launch new crew straight away, unless
// there is crew to resume, in which case ask (resume first).
func (m deckModel) launchOrOffer() (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	if !ok {
		return m, nil
	}
	// A mission being briefed has one briefing crew at a time: go to it.
	if runs := m.reg.live(dm.M.Slug); dm.M.State == mission.Briefing && len(runs) > 0 {
		m.focusRun(runs[len(runs)-1])
		return m, nil
	}
	// The orchestrator queued crew for you to launch: ⏎ launches it.
	if dm.M.Dispatch != nil {
		return m.startDispatch()
	}
	last, ok := m.resumableCrew(dm)
	if !ok {
		return m.startCrew(harness.Default())
	}
	when := ""
	if c, found := crewRecord(dm.M, last.ID); found && c.Ended != nil {
		when = ", ended " + agoText(*c.Ended)
	}
	if last.ResumeWith != "" {
		when += ": " + last.ResumeWith
	}
	m.openPick("Launch crew on "+dm.M.Slug, []string{
		fmt.Sprintf("Resume %s (%s%s)", last.ID, last.Harness, when),
		"New crew (" + harness.Default() + ")",
		"New crew with another harness…",
	}, func(i int) tea.Cmd {
		return func() tea.Msg { return deckLaunchChoiceMsg{choice: i} }
	})
	return m, nil
}

// deckLaunchChoiceMsg is the answer to launchOrOffer's question.
type deckLaunchChoiceMsg struct{ choice int }

func (m deckModel) handleLaunchChoice(msg deckLaunchChoiceMsg) (tea.Model, tea.Cmd) {
	switch msg.choice {
	case 0:
		return m.resumeCrew()
	case 1:
		return m.startCrew(harness.Default())
	default:
		return m.chooseHarness()
	}
}

func agoText(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func crewRecord(m *mission.Mission, id string) (mission.Crew, bool) {
	for _, c := range m.Crew {
		if c.ID == id {
			return c, true
		}
	}
	return mission.Crew{}, false
}

// launchCmd starts crew off the UI thread (the harness may ask the agent
// for a chat id, and the host may have to start the deck server), then
// records that you acted on the mission.
func (m deckModel) launchCmd(req crew.Request) tea.Cmd {
	req.Store, req.Host, req.Root = m.store, m.host, m.ws.Root
	req.Width, req.Height = m.mainWidth(), m.bodyHeight()
	store, slug := m.store, req.Slug
	return func() tea.Msg {
		msg := deckLaunchReadyMsg{slug: slug, harness: req.Harness, capsule: req.Place.Capsule, role: req.Role}
		started, err := crew.Start(req)
		if err != nil {
			msg.err = err
			return msg
		}
		if req.Handoff != "" {
			store.QueueDispatch(slug, nil) // the queued dispatch is launched
		}
		// You're acting on the mission: a flag is answered, and the
		// orchestrator's run of dispatches without you starts again.
		if mm, err := store.Load(slug); err == nil && mm.Flag != nil {
			store.SetFlag(slug, "", "")
		}
		store.UserActed(slug)
		msg.crewID, msg.dir, msg.prompt, msg.term = started.Crew.ID, started.Dir, started.Prompt, started.Term
		return msg
	}
}

// finishLaunch adds a started crew to the deck and focuses it.
func (m deckModel) finishLaunch(msg deckLaunchReadyMsg) (tea.Model, tea.Cmd) {
	delete(m.launching, msg.slug)
	if msg.err != nil {
		m.flash = "launch failed: " + msg.err.Error()
		return m, loadDeckCmd(m.ws, m.prs)
	}
	r := &deckCrewRun{slug: msg.slug, crewID: msg.crewID, harness: msg.harness, capsule: msg.capsule, dir: msg.dir, role: msg.role, term: msg.term}
	r.term.Resize(m.mainWidth(), m.bodyHeight())
	m.reg.add(r)
	m.focusRun(r)
	m.flash = ""
	return m, tea.Batch(waitPane(r), loadDeckCmd(m.ws, m.prs), deliverPrompt(r, msg.prompt))
}

// deliverPrompt types the opening prompt in once the agent has drawn.
func deliverPrompt(r *deckCrewRun, prompt string) tea.Cmd {
	if strings.TrimSpace(prompt) == "" {
		return nil
	}
	return func() tea.Msg {
		if !crew.TypePrompt(r.term, prompt) {
			return nil
		}
		return deckFlashMsg("sent the opening prompt to " + r.crewID)
	}
}

// adoptHosted takes over crew a server host was already running.
func (m *deckModel) adoptHosted() []tea.Cmd {
	known := m.reg.liveKeys()
	return m.adopt(m.host.Running(func(key string) bool { return known[key] }))
}

func (m *deckModel) adopt(hosted []crew.Hosted) []tea.Cmd {
	var cmds []tea.Cmd
	for _, h := range hosted {
		if m.reg.liveKey(h.Spec.Slug + "/" + h.Spec.CrewID) {
			continue
		}
		r := &deckCrewRun{slug: h.Spec.Slug, crewID: h.Spec.CrewID, harness: h.Spec.Harness, capsule: h.Spec.Capsule, dir: h.Spec.Dir, role: h.Spec.Role, term: h.Term}
		m.reg.add(r)
		// Before the first WindowSizeMsg the deck has no size; resizing now
		// would squash the crew's screen to nothing. resizePanes follows.
		if m.width > 0 {
			h.Term.Resize(m.mainWidth(), m.bodyHeight())
		}
		cmds = append(cmds, waitPane(r))
	}
	return cmds
}

// deckAdoptMsg is crew found running on the server that the deck didn't
// launch: dispatched by the orchestrator, or from another deck.
type deckAdoptMsg struct{ hosted []crew.Hosted }

// adoptNewCmd looks for crew the mission records say are running but the
// deck doesn't have, and attaches to them.
func (m deckModel) adoptNewCmd(missions map[string]*mission.Mission) tea.Cmd {
	missing := false
	for slug, mm := range missions {
		if m.launching[slug] {
			continue // our own launch, still on its way
		}
		for _, c := range mm.Crew {
			if c.Ended == nil && c.PID != 0 && !m.reg.liveKey(slug+"/"+c.ID) {
				missing = true
			}
		}
	}
	if !missing || !m.reg.adopting.CompareAndSwap(false, true) {
		return nil
	}
	// Run states are the deck goroutine's to read: hand the lookup a
	// snapshot. Crew being launched right now aren't in it, but the server
	// client refuses to follow crew twice, so they can't be attached again.
	host, reg, known := m.host, m.reg, m.reg.liveKeys()
	return func() tea.Msg {
		defer reg.adopting.Store(false)
		return deckAdoptMsg{hosted: host.Running(func(key string) bool { return known[key] })}
	}
}

func (m deckModel) handleAdopt(msg deckAdoptMsg) (tea.Model, tea.Cmd) {
	before := len(m.reg.live(""))
	cmds := m.adopt(msg.hosted)
	if len(cmds) == 0 {
		return m, nil
	}
	runs := m.reg.live("")
	var names []string
	for _, r := range runs[before:] {
		names = append(names, r.crewID+" on "+r.slug)
	}
	text := strings.Join(names, ", ") + " joined (dispatched)"
	m.flash = text
	return m, tea.Batch(append(cmds, loadDeckCmd(m.ws, m.prs), notifyCmd(text))...)
}

// waitPane blocks until the crew's terminal changes or exits.
func waitPane(r *deckCrewRun) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-r.term.Updates():
		case <-r.term.Done():
		}
		return deckPaneMsg{run: r}
	}
}

func (m deckModel) handlePaneMsg(msg deckPaneMsg) (tea.Model, tea.Cmd) {
	r := msg.run
	if r == nil || r.ended {
		return m, nil
	}
	if !r.term.Exited() {
		return m, waitPane(r)
	}
	r.ended = true
	r.exit = r.term.ExitSummary()
	if m.stopping {
		return m, nil // quitting: nothing left to show
	}
	// The host has recorded the end (and run the orchestrator if it's due).
	if sel, ok := m.selected(); ok && sel.kind == deckRowCrew && sel.run == r {
		m.focusTerm = false
		m.cursorKey = "m:" + r.slug
	}
	if r.role == mission.RoleBriefing {
		// The mission may have been renamed by ws mission ready; the next
		// load finds it by this crew and lands there.
		m.afterBriefing = r
	}
	m.rebuildRows()
	m.flash = fmt.Sprintf("crew %s on %s ended (%s)", r.crewID, r.slug, r.exit)
	return m, loadDeckCmd(m.ws, m.prs)
}

// resizePanes keeps every crew terminal the size of the main pane.
func (m deckModel) resizePanes() {
	for _, r := range m.reg.live("") {
		r.term.Resize(m.mainWidth(), m.bodyHeight())
	}
}

// cycleRun moves to the next (or previous) live crew of any mission.
func (m deckModel) cycleRun(delta int) (tea.Model, tea.Cmd) {
	runs := m.reg.live("")
	if len(runs) == 0 {
		return m, nil
	}
	cur, _ := m.selectedRun()
	i := 0
	for j, r := range runs {
		if r == cur {
			i = j + delta
		}
	}
	i = ((i % len(runs)) + len(runs)) % len(runs)
	focus := m.focusTerm
	m.focusRun(runs[i])
	m.focusTerm = focus
	return m, nil
}

// --- terminal focus ---

// handleTermKey routes keys while a crew terminal has focus: everything goes
// to the agent except the prefix key, which takes one deck command.
func (m deckModel) handleTermKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r, ok := m.focusedRun()
	if !ok {
		m.focusTerm, m.prefix, m.scrollMode = false, false, false
		return m, nil
	}
	if m.scrollMode {
		return m.handleScrollKey(r, msg)
	}

	if !m.prefix {
		if msg.String() == deckPrefixKey {
			m.prefix = true
			return m, nil
		}
		r.scroll = 0
		r.term.SendKey(uv.KeyPressEvent(msg))
		return m, m.clearStatusCmd(r)
	}

	m.prefix = false
	switch msg.String() {
	case deckPrefixKey:
		r.term.SendKey(uv.KeyPressEvent(msg))
	case "tab", "esc", "h", "left":
		m.focusTerm = false
	case "s":
		m.sidebarHidden = !m.sidebarHidden
		m.resizePanes()
	case "z":
		return m.zoom(r)
	case "m":
		return m.toggleMouse(), nil
	case "[", "pgup":
		if r.term.ScrollbackLen() == 0 {
			m.flash = "no history to scroll (the agent is drawing full-screen)"
			return m, nil
		}
		m.scrollMode = true
		r.scroll = min(max(r.scroll, m.bodyHeight()/2), r.term.ScrollbackLen())
	case "n", "]":
		return m.cycleRun(1)
	case "p":
		return m.cycleRun(-1)
	case "S":
		return m.confirmStop(r)
	case "?":
		m.showHelp = true
	case "q":
		return m.requestQuit()
	}
	return m, nil
}

// handleScrollKey moves through a crew's history until scroll mode ends.
func (m deckModel) handleScrollKey(r *deckCrewRun, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	maxScroll := r.term.ScrollbackLen()
	half := max(m.bodyHeight()/2, 1)
	switch msg.String() {
	case "k", "up":
		r.scroll++
	case "j", "down":
		r.scroll--
	case "ctrl+u", "pgup", "b":
		r.scroll += half
	case "ctrl+d", "pgdown", "space", "f":
		r.scroll -= half
	case "g", "home":
		r.scroll = maxScroll
	case "G", "end":
		r.scroll = 0
	case "esc", "q", "enter", deckPrefixKey:
		r.scroll = 0
		m.scrollMode = false
		return m, nil
	}
	r.scroll = min(max(r.scroll, 0), maxScroll)
	if r.scroll == 0 && msg.String() != "k" && msg.String() != "up" {
		// Scrolled back to the live screen: leave scroll mode.
		m.scrollMode = false
	}
	return m, nil
}

// handleWheel scrolls history, or forwards the wheel to an agent drawing
// full-screen (which may use it itself).
func (m deckModel) handleWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	r, ok := m.shownRun()
	mouse := msg.Mouse()
	if !ok || mouse.X >= m.mainWidth() {
		m.syncMain()
		var cmd tea.Cmd
		m.mainVP, cmd = m.mainVP.Update(msg)
		return m, cmd
	}
	if r.term.AltScreen() {
		mouse.Y -= 2 // below header and rule
		r.term.SendMouse(uv.MouseWheelEvent(uv.Mouse(mouse)))
		return m, nil
	}
	switch mouse.Button {
	case tea.MouseWheelUp:
		r.scroll += 3
	case tea.MouseWheelDown:
		r.scroll -= 3
	}
	r.scroll = min(max(r.scroll, 0), r.term.ScrollbackLen())
	m.scrollMode = m.focusTerm && r.scroll > 0
	return m, nil
}

// --- stopping ---

func (m deckModel) confirmStop(r *deckCrewRun) (tea.Model, tea.Cmd) {
	m.openConfirm(fmt.Sprintf("Stop crew %s on %s?", r.crewID, r.slug), func() tea.Msg {
		r.term.Close(deckStopGrace)
		<-r.term.Done() // its end is recorded
		return deckPaneMsg{run: r}
	})
	return m, nil
}

func (m deckModel) stopSelected() (tea.Model, tea.Cmd) {
	if r, ok := m.selectedRun(); ok {
		return m.confirmStop(r)
	}
	dm, ok := m.selectedMission()
	if !ok {
		return m, nil
	}
	runs := m.reg.live(dm.M.Slug)
	switch len(runs) {
	case 0:
		m.flash = "no live crew on " + dm.M.Slug
		return m, nil
	case 1:
		return m.confirmStop(runs[0])
	default:
		m.flash = "select which crew to stop"
		return m, nil
	}
}

// requestQuit quits, asking first if crew are still running.
func (m deckModel) requestQuit() (tea.Model, tea.Cmd) {
	n := m.liveCount()
	if n == 0 {
		return m, tea.Quit
	}
	noun := "crew member"
	if n > 1 {
		noun = "crew members"
	}
	m.focusTerm, m.prefix, m.scrollMode = false, false, false
	if m.host.Detachable() {
		m.openPick(fmt.Sprintf("%d live %s", n, noun), []string{
			"Detach: quit and leave crew running (ws deck reattaches)",
			"Stop crew and quit",
			"Cancel",
		}, func(i int) tea.Cmd {
			switch i {
			case 0:
				return tea.Quit
			case 1:
				return func() tea.Msg { return deckQuitConfirmedMsg{} }
			}
			return nil
		})
		return m, nil
	}
	m.openConfirm(fmt.Sprintf("Stop %d live %s and quit?", n, noun), func() tea.Msg {
		return deckQuitConfirmedMsg{}
	})
	return m, nil
}

// stopAll hangs up every live crew in parallel, waits until their ends are
// recorded, then quits.
func (m deckModel) stopAll() (tea.Model, tea.Cmd) {
	m.stopping = true
	m.flash = "stopping crew…"
	runs := m.reg.live("")
	return m, func() tea.Msg {
		var wg sync.WaitGroup
		for _, r := range runs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r.term.Close(deckStopGrace)
				<-r.term.Done()
			}()
		}
		wg.Wait()
		return deckStoppedMsg{}
	}
}

// stopLeftovers runs after the program ends: it leaves the host. Crew on
// the deck server keep running; crew in this process are stopped, their
// ends recorded.
func (m deckModel) stopLeftovers() {
	m.host.Leave()
}

// --- rendering ---

// renderTerm draws a crew terminal at exactly width×height cells, scrolled
// back offset lines.
func renderTerm(t crew.Term, offset, width, height int) string {
	lines := strings.Split(t.RenderScrolled(offset), "\n")
	out := make([]string, height)
	for i := range height {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], width, "")
		}
		if pad := width - ansi.StringWidth(line); pad > 0 {
			line += "\x1b[m" + strings.Repeat(" ", pad)
		}
		out[i] = line
	}
	return strings.Join(out, "\n")
}

// crewSummary describes a mission's crew for the main pane.
func (m deckModel) crewSummary(dm deckMission) string {
	slug := dm.M.Slug
	var lines []string
	for _, r := range m.reg.live(slug) {
		line := m.crewBadge(r) + " " + r.harness + ui.Dim.Render(" in "+r.capsule)
		if status, note := m.crewStatus(r); status != "" && note != "" {
			line += ": " + note
		}
		lines = append(lines, line)
	}
	if m.launching[slug] {
		lines = append(lines, ui.Dim.Render("launching crew…"))
	}
	var last *deckCrewRun
	for _, r := range m.reg.all() {
		if r.slug == slug && r.ended {
			last = r
		}
	}
	hint := "⏎ launch " + harness.Default() + " · L choose harness"
	if c, ok := m.resumableCrew(&dm); ok {
		hint = "⏎ resume " + c.ID + " or launch new · R resume " + c.ID + " · L choose harness"
		if c.ResumeWith != "" {
			lines = append(lines, ui.Orange.Render("↻ ")+fmt.Sprintf("Suggested: resume %s with: %s", c.ID, c.ResumeWith)+ui.Dim.Render(" · R"))
		}
	}
	if dm.M.Dispatch != nil {
		hint = "⏎ launch the queued crew · F dismiss it · L choose harness"
	}
	briefedLast := last != nil && last.role == mission.RoleBriefing && dm.M.State != mission.Briefing
	switch {
	case last != nil && !briefedLast:
		lines = append(lines, ui.Dim.Render(fmt.Sprintf("%s (%s) ended: %s · %s", last.crewID, last.harness, last.exit, hint)))
	case len(dm.M.Crew) > 0 && len(lines) == 0:
		prev := dm.M.Crew[len(dm.M.Crew)-1]
		if prev.Done(dm.M) {
			lines = append(lines, ui.Dim.Render(fmt.Sprintf("briefed by %s · no crew working yet · %s", prev.ID, hint)))
			break
		}
		lines = append(lines, ui.Dim.Render(fmt.Sprintf("%d crew so far, last %s (%s) · %s", len(dm.M.Crew), prev.ID, prev.Harness, hint)))
	case len(lines) == 0:
		lines = append(lines, ui.Dim.Render("no crew yet · "+hint))
	default:
		lines = append(lines, ui.Dim.Render(hint))
	}
	return strings.Join(lines, "\n  ")
}

// chooseHarness opens a picker of available harnesses.
func (m deckModel) chooseHarness() (tea.Model, tea.Cmd) {
	if _, ok := m.selectedMission(); !ok {
		m.flash = "select a mission to launch crew on"
		return m, nil
	}
	names := harness.Names()
	labels := make([]string, len(names))
	for i, n := range names {
		labels[i] = n
		h, _ := harness.Get(n)
		if err := h.Available(); err != nil {
			labels[i] += "  (" + err.Error() + ")"
		}
	}
	m.openPick("Launch crew with", labels, func(i int) tea.Cmd {
		return func() tea.Msg { return deckLaunchWithMsg{harness: names[i]} }
	})
	return m, nil
}

// briefingPlace is how a briefing crew's working place reads in the deck: it
// works from the workspace root, before any capsule exists.
const briefingPlace = "workspace"

// launchBriefing starts briefing crew on a mission being briefed: in the
// workspace root, with the user's request and the job of writing the brief
// and creating capsules.
func (m deckModel) launchBriefing(mm *mission.Mission, brief, harnessName string) (tea.Model, tea.Cmd) {
	h, err := harness.Get(harnessName)
	if err == nil {
		err = h.Available()
	}
	if err != nil {
		m.flash = err.Error() + " · L picks another harness"
		return m, loadDeckCmd(m.ws, m.prs)
	}
	b := harness.Brief{
		Slug: mm.Slug, Title: mm.Title, Brief: brief, Dir: m.ws.Root,
		Role: mission.RoleBriefing, Request: mm.Request, Org: m.ws.Org,
	}
	m.launching[mm.Slug] = true
	m.flash = "briefing " + mm.Slug + " with " + harnessName + "…"
	return m, m.launchCmd(crew.Request{Slug: mm.Slug, Harness: harnessName, Place: crew.Place{Dir: m.ws.Root, Capsule: briefingPlace}, Role: mission.RoleBriefing,
		Prepare: func() (harness.Launch, error) { return h.Start(b) }})
}

// landAfterBriefing runs when data arrives after a briefing crew ended: it
// finds the crew's mission (renamed or not) and puts the cursor on it, saying
// whether briefing finished.
func (m *deckModel) landAfterBriefing(d deckData) {
	r := m.afterBriefing
	m.afterBriefing = nil
	for _, dm := range d.Missions {
		for _, c := range dm.M.Crew {
			// Same crew on the same slug, or (renamed by ready) the crew
			// record with this briefing process's pid.
			sameCrew := c.ID == r.crewID && c.Role == mission.RoleBriefing
			if !sameCrew || (dm.M.Slug != r.slug && c.PID != r.term.Pid()) {
				continue
			}
			m.cursorKey = "m:" + dm.M.Slug
			if dm.M.State == mission.Briefing {
				m.flash = "briefing stopped before the mission was ready · ⏎ to resume or restart it"
				return
			}
			live := 0
			for _, cv := range dm.Capsules {
				if cv.Exists {
					live++
				}
			}
			m.flash = ui.Green.Render("✓") + fmt.Sprintf(" Briefed: %s · %d capsule(s) · ⏎ to launch crew", dm.M.Title, live)
			return
		}
	}
}

// --- activity ---

// crewIdleAfter is how long a crew's screen must stay still before it counts
// as waiting (for you) rather than working.
var crewIdleAfter = 5 * time.Second

const activityTickEvery = time.Second

type deckActivityTickMsg struct{}

func activityTick() tea.Cmd {
	return tea.Tick(activityTickEvery, func(time.Time) tea.Msg { return deckActivityTickMsg{} })
}

// activity says whether crew is working (its screen changed recently) and
// for how long it has been in that state's current stretch.
func (r *deckCrewRun) activity() (working bool, since time.Duration) {
	since = time.Since(r.term.LastChange())
	return since < crewIdleAfter, since
}

// handleActivityTick notices crew that just went quiet and tells you, unless
// you're already looking at that crew.
func (m deckModel) handleActivityTick() (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	focused, _ := m.focusedRun()
	for _, r := range m.reg.live("") {
		working, _ := r.activity()
		status, _ := m.crewStatus(r)
		if r.wasWorking && !working && r != focused && status == "" {
			msg := fmt.Sprintf("%s on %s is waiting for you", r.crewID, r.slug)
			m.flash = ui.Orange.Render("◐") + " " + msg
			cmds = append(cmds, notifyCmd("ws deck: "+msg))
		}
		r.wasWorking = working
	}
	m.ticks++
	if m.ticks%recordsEveryTicks == 0 {
		cmds = append(cmds, loadRecordsCmd(m.store))
	}
	return m, tea.Batch(append(cmds, activityTick())...)
}

// recordsEveryTicks: crew signals (ws mission ask/done) are picked up by a
// light re-read of mission records this often, rather than waiting for the
// full reload.
const recordsEveryTicks = 2

type deckRecordsMsg struct{ missions map[string]*mission.Mission }

func loadRecordsCmd(store mission.Store) tea.Cmd {
	return func() tea.Msg {
		list, _ := store.List()
		by := make(map[string]*mission.Mission, len(list))
		for _, mm := range list {
			by[mm.Slug] = mm
		}
		return deckRecordsMsg{missions: by}
	}
}

// handleRecords swaps in fresh mission records (crew statuses, artifacts)
// and announces new signals.
func (m deckModel) handleRecords(msg deckRecordsMsg) (tea.Model, tea.Cmd) {
	for i := range m.data.Missions {
		if mm, ok := msg.missions[m.data.Missions[i].M.Slug]; ok {
			m.data.Missions[i].M = mm
		}
	}
	return m, tea.Batch(m.announceSignals(), m.adoptNewCmd(msg.missions))
}

// crewStatus is what a crew last said about itself (asking/done), per its
// mission record.
func (m deckModel) crewStatus(r *deckCrewRun) (status, note string) {
	for _, dm := range m.data.Missions {
		if dm.M.Slug != r.slug {
			continue
		}
		if c, ok := crewRecord(dm.M, r.crewID); ok {
			return c.Status, c.Note
		}
	}
	return "", ""
}

// announceSignals notifies once per new ask/done from live crew.
func (m *deckModel) announceSignals() tea.Cmd {
	var cmds []tea.Cmd
	for _, r := range m.reg.live("") {
		status, note := m.crewStatus(r)
		key := ""
		for _, dm := range m.data.Missions {
			if dm.M.Slug == r.slug {
				if c, ok := crewRecord(dm.M, r.crewID); ok && c.StatusAt != nil {
					key = status + c.StatusAt.String()
				}
			}
		}
		if status == "" {
			r.notified = ""
			continue
		}
		if key == r.notified {
			continue
		}
		r.notified = key
		var msg string
		switch status {
		case mission.CrewAsking:
			msg = fmt.Sprintf("%s on %s asks: %s", r.crewID, r.slug, note)
			m.flash = ui.Orange.Render("?") + " " + msg
		case mission.CrewDone:
			msg = fmt.Sprintf("%s on %s is done: %s", r.crewID, r.slug, note)
			m.flash = ui.Green.Render("✓") + " " + msg
		default:
			continue
		}
		cmds = append(cmds, notifyCmd("ws deck: "+msg))
	}
	return tea.Batch(cmds...)
}

// clearStatusCmd forgets a crew's asking/done once the user talks to it.
func (m deckModel) clearStatusCmd(r *deckCrewRun) tea.Cmd {
	if status, _ := m.crewStatus(r); status == "" {
		return nil
	}
	// Forget it locally now, so the sidebar updates before the re-read.
	for i := range m.data.Missions {
		if m.data.Missions[i].M.Slug != r.slug {
			continue
		}
		mm := *m.data.Missions[i].M
		mm.Crew = append([]mission.Crew(nil), mm.Crew...)
		for j := range mm.Crew {
			if mm.Crew[j].ID == r.crewID {
				mm.Crew[j].Status, mm.Crew[j].Note, mm.Crew[j].StatusAt = "", "", nil
			}
		}
		m.data.Missions[i].M = &mm
	}
	store, slug, id := m.store, r.slug, r.crewID
	return func() tea.Msg {
		store.ClearCrewStatus(slug, id)
		return nil
	}
}

// notifyCmd rings the terminal bell and asks for a desktop notification
// (OSC 9: iTerm2, kitty, Ghostty and WezTerm show it; others ignore it).
func notifyCmd(text string) tea.Cmd {
	text = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, text)
	return tea.Raw("\a\x1b]9;" + text + "\x1b\\")
}

// shortDuration is "12s", "4m" or "2h".
func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

// crewBadge is how crew reads in the sidebar: what it said about itself
// (asking, done) first, else its activity.
func (m deckModel) crewBadge(r *deckCrewRun) string {
	switch status, _ := m.crewStatus(r); status {
	case mission.CrewAsking:
		return ui.Orange.Render("?"+r.crewID) + ui.Dim.Render(" asks")
	case mission.CrewDone:
		return ui.Green.Render("✓"+r.crewID) + ui.Dim.Render(" done")
	}
	return r.activityBadge()
}

// activityBadge is how a crew's activity reads in the sidebar.
func (r *deckCrewRun) activityBadge() string {
	if working, _ := r.activity(); working {
		return ui.Green.Render("●"+r.crewID) + ui.Dim.Render(" working")
	}
	_, since := r.activity()
	return ui.Orange.Render("◐"+r.crewID) + ui.Dim.Render(" waiting "+shortDuration(since))
}

// toggleMouse turns the deck's mouse capture off everywhere, or back on.
func (m deckModel) toggleMouse() deckModel {
	m.mouseOff = !m.mouseOff
	if m.mouseOff {
		m.flash = "mouse: the terminal selects text and opens links everywhere · M (or " + deckPrefixKey + " m) to give the deck the mouse back"
	} else {
		m.flash = "mouse: the deck takes clicks and the wheel when the sidebar has focus"
	}
	return m
}
