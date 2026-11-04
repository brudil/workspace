package cli

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
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
	term    crewTerm

	// UI state, touched only by Update.
	ended  bool
	exit   string
	scroll int // lines scrolled back into history; 0 is live

	// recordOnce makes sure the crew's end is written to the mission once,
	// whichever path (exit, stop, quit, deck dying) gets there first.
	recordOnce sync.Once
}

func (r *deckCrewRun) live() bool { return r != nil && !r.ended }

func (r *deckCrewRun) key() string { return "r:" + r.slug + ":" + r.crewID }

// record writes the crew's end to the mission, once.
func (r *deckCrewRun) record(store mission.Store, exit string) {
	r.recordOnce.Do(func() { store.EndCrew(r.slug, r.crewID, exit) })
}

// --- messages ---

// deckLaunchReadyMsg carries a started crew (or why it didn't start) back to
// Update.
type deckLaunchReadyMsg struct {
	slug    string
	harness string
	capsule string
	dir     string
	crewID  string
	term    crewTerm
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
	m.cursorKey = r.key()
	m.rebuildRows()
	m.focusTerm = true
}

func (m deckModel) liveCount() int { return len(m.reg.live("")) }

// --- launching ---

type crewPlace struct {
	dir, capsule string
	sharedWith   *deckCrewRun // live crew already working there, if any
}

// crewPlaceFor picks where new crew start: the mission's first capsule no
// live crew is working in, else its first capsule (shared).
func (m deckModel) crewPlaceFor(dm *deckMission) (crewPlace, bool) {
	busy := map[string]*deckCrewRun{}
	for _, r := range m.reg.live("") {
		busy[r.dir] = r
	}
	var first *crewPlace
	for _, c := range dm.Capsules {
		if !c.Exists {
			continue
		}
		p := crewPlace{dir: c.Path, capsule: c.Repo + "/" + c.Name, sharedWith: busy[c.Path]}
		if p.sharedWith == nil {
			return p, true
		}
		if first == nil {
			first = &p
		}
	}
	if first != nil {
		return *first, true
	}
	return crewPlace{}, false
}

// startCrew launches crew with the named harness on the selected mission,
// asking first if every capsule already has crew working in it.
func (m deckModel) startCrew(harnessName string) (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	if !ok {
		m.flash = "select a mission to launch crew on"
		return m, nil
	}
	slug := dm.M.Slug
	if m.launching[slug] {
		return m, nil
	}
	place, ok := m.crewPlaceFor(dm)
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
	b := harness.Brief{Slug: slug, Title: dm.M.Title, Brief: dm.Brief, Dir: place.dir}
	launch := m.launchCmd(slug, harnessName, place.capsule, func() (harness.Launch, error) { return h.Start(b) })

	if place.sharedWith != nil {
		m.openConfirm(fmt.Sprintf("%s is already working in %s. Launch another crew there too?", place.sharedWith.crewID, place.capsule),
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

// resumeCrew relaunches the mission's most recent crew that recorded a
// conversation id.
func (m deckModel) resumeCrew() (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	if !ok {
		return m, nil
	}
	slug := dm.M.Slug
	if m.launching[slug] {
		return m, nil
	}
	var last mission.Crew
	found := false
	for i := len(dm.M.Crew) - 1; i >= 0; i-- {
		if dm.M.Crew[i].Conversation != "" {
			last, found = dm.M.Crew[i], true
			break
		}
	}
	if !found {
		m.flash = "no crew on " + slug + " recorded a conversation to resume"
		return m, nil
	}
	for _, r := range m.reg.live(slug) {
		if c, ok := crewRecord(dm.M, r.crewID); ok && c.Conversation == last.Conversation {
			m.focusRun(r)
			return m, nil
		}
	}
	place, ok := m.crewPlaceFor(dm)
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
	return m, m.launchCmd(slug, last.Harness, place.capsule, func() (harness.Launch, error) {
		return h.Resume(place.dir, last.Conversation)
	})
}

func crewRecord(m *mission.Mission, id string) (mission.Crew, bool) {
	for _, c := range m.Crew {
		if c.ID == id {
			return c, true
		}
	}
	return mission.Crew{}, false
}

// crewEnv is the environment for crew: ours, minus anything that would
// mislead it, plus the mission and crew ids and a capable TERM.
func crewEnv(slug, crewID string) []string {
	drop := map[string]bool{mission.EnvMission: true, mission.EnvCrew: true, ShellPWD: true, "TERM": true, "COLORTERM": true}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			env = append(env, kv)
		}
	}
	return append(env,
		mission.EnvMission+"="+slug,
		mission.EnvCrew+"="+crewID,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)
}

// launchCmd does all of a launch off the UI thread: the harness may ask the
// agent for a chat id, and a server host may have to start the server.
func (m deckModel) launchCmd(slug, harnessName, capsule string, prepare func() (harness.Launch, error)) tea.Cmd {
	store, host := m.store, m.host
	w, h := m.mainWidth(), m.bodyHeight()
	return func() tea.Msg {
		msg := deckLaunchReadyMsg{slug: slug, harness: harnessName, capsule: capsule}
		l, err := prepare()
		if err != nil {
			msg.err = err
			return msg
		}
		msg.dir = l.Dir
		crew, err := store.StartCrew(slug, harnessName, l.Conversation)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.crewID = crew.ID
		term, err := host.Launch(crewSpec{
			Slug: slug, CrewID: crew.ID, Harness: harnessName, Capsule: capsule,
			Args: l.Args, Dir: l.Dir, Env: crewEnv(slug, crew.ID),
			Width: w, Height: h,
		})
		if err != nil {
			store.EndCrew(slug, crew.ID, "failed to start: "+err.Error())
			msg.err = err
			return msg
		}
		store.SetCrewPID(slug, crew.ID, term.Pid())
		msg.term = term
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
	r := &deckCrewRun{slug: msg.slug, crewID: msg.crewID, harness: msg.harness, capsule: msg.capsule, dir: msg.dir, term: msg.term}
	r.term.Resize(m.mainWidth(), m.bodyHeight())
	m.reg.add(r)
	m.focusRun(r)
	m.flash = ""
	return m, tea.Batch(waitPane(r), loadDeckCmd(m.ws, m.prs))
}

// adoptHosted takes over crew a server host was already running.
func (m *deckModel) adoptHosted() []tea.Cmd {
	var cmds []tea.Cmd
	for _, h := range m.host.Running() {
		r := &deckCrewRun{slug: h.Spec.Slug, crewID: h.Spec.CrewID, harness: h.Spec.Harness, capsule: h.Spec.Capsule, dir: h.Spec.Dir, term: h.Term}
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
		return m, nil // the stop command records these
	}
	if m.host.OwnsLifecycle() {
		r.record(m.store, r.exit)
	}
	if sel, ok := m.selected(); ok && sel.kind == deckRowCrew && sel.run == r {
		m.focusTerm = false
		m.cursorKey = "m:" + r.slug
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
		return m, nil
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
	if !m.host.OwnsLifecycle() {
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

// stopAll hangs up every live crew in parallel, records how each ended, then
// quits.
func (m deckModel) stopAll() (tea.Model, tea.Cmd) {
	m.stopping = true
	m.flash = "stopping crew…"
	runs := m.reg.live("")
	store, owns := m.store, m.host.OwnsLifecycle()
	return m, func() tea.Msg {
		var wg sync.WaitGroup
		for _, r := range runs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r.term.Close(deckStopGrace)
				if owns {
					r.record(store, "stopped: "+r.term.ExitSummary())
				}
			}()
		}
		wg.Wait()
		return deckStoppedMsg{}
	}
}

// stopLeftovers runs after the program ends. With a server host it just
// detaches: crew keep running. Otherwise crew can't outlive the deck, so it
// hangs up the running ones and records every unrecorded end (SIGTERM, a
// crash in Update, a closed terminal).
func (m deckModel) stopLeftovers() {
	if !m.host.OwnsLifecycle() {
		m.host.Detach()
		return
	}
	var wg sync.WaitGroup
	for _, r := range m.reg.all() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r.term.Exited() {
				r.record(m.store, r.term.ExitSummary())
				return
			}
			r.term.Close(deckStopGrace)
			r.record(m.store, "stopped: deck exited")
		}()
	}
	wg.Wait()
}

// processAlive reports whether pid is a running process.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// --- rendering ---

// renderTerm draws a crew terminal at exactly width×height cells, scrolled
// back offset lines.
func renderTerm(t crewTerm, offset, width, height int) string {
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
		lines = append(lines, ui.Green.Render("●"+r.crewID)+" "+r.harness+ui.Dim.Render(" in "+r.capsule))
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
	if canResume(dm.M) {
		hint += " · R resume"
	}
	switch {
	case last != nil:
		lines = append(lines, ui.Dim.Render(fmt.Sprintf("%s (%s) ended: %s · %s", last.crewID, last.harness, last.exit, hint)))
	case len(dm.M.Crew) > 0 && len(lines) == 0:
		prev := dm.M.Crew[len(dm.M.Crew)-1]
		lines = append(lines, ui.Dim.Render(fmt.Sprintf("%d crew so far, last %s (%s) · %s", len(dm.M.Crew), prev.ID, prev.Harness, hint)))
	case len(lines) == 0:
		lines = append(lines, ui.Dim.Render("no crew yet · "+hint))
	default:
		lines = append(lines, ui.Dim.Render(hint))
	}
	return strings.Join(lines, "\n  ")
}

func canResume(m *mission.Mission) bool {
	for _, c := range m.Crew {
		if c.Conversation != "" {
			return true
		}
	}
	return false
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
