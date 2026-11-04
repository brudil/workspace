package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/termpane"
	"github.com/brudil/workspace/internal/ui"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// deckPrefixKey, pressed while a crew terminal has focus, makes the next key
// a deck command instead of input for the agent.
const deckPrefixKey = "ctrl+g"

// deckStopGrace is how long crew get to exit after SIGHUP before SIGKILL.
const deckStopGrace = 3 * time.Second

// deckCrewRun is crew the deck launched for a mission.
type deckCrewRun struct {
	slug    string
	crewID  string
	harness string
	pane    *termpane.Pane

	// ended and exit are UI state, touched only by Update.
	ended bool
	exit  string

	// recordOnce makes sure the crew's end is written to the mission once,
	// whichever path (exit, stop, quit, deck dying) gets there first.
	recordOnce sync.Once
}

func (r *deckCrewRun) live() bool { return r != nil && !r.ended }

// record writes the crew's end to the mission, once.
func (r *deckCrewRun) record(store mission.Store, exit string) {
	r.recordOnce.Do(func() { store.EndCrew(r.slug, r.crewID, exit) })
}

// --- messages ---

// deckLaunchReadyMsg carries a prepared launch back to Update, which records
// the crew and starts its terminal.
type deckLaunchReadyMsg struct {
	slug    string
	harness string
	launch  harness.Launch
	err     error
}

// deckPaneMsg means a crew terminal changed (or its program exited).
type deckPaneMsg struct{ slug string }

// deckStoppedMsg is sent once quitting has stopped every crew.
type deckStoppedMsg struct{}

// --- launching ---

// liveRun returns the live crew for a mission, if any.
func (m deckModel) liveRun(slug string) (*deckCrewRun, bool) {
	r, ok := m.crew[slug]
	return r, ok && r.live()
}

func (m deckModel) liveCount() int {
	n := 0
	for _, r := range m.crew {
		if r.live() {
			n++
		}
	}
	return n
}

// crewDir picks where crew start: the mission's first existing capsule.
func crewDir(dm *deckMission) (string, bool) {
	for _, c := range dm.Capsules {
		if c.Exists {
			return c.Path, true
		}
	}
	return "", false
}

// startCrew begins launching crew with the named harness on the selected
// mission. The slow part (the harness may ask the agent for a chat id) runs
// as a command; deckLaunchReadyMsg finishes the job.
func (m deckModel) startCrew(harnessName string) (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	if !ok {
		m.flash = "select a mission to launch crew on"
		return m, nil
	}
	slug := dm.M.Slug
	if _, live := m.liveRun(slug); live {
		m.focusTerm = true
		return m, nil
	}
	if m.launching[slug] {
		return m, nil
	}
	dir, ok := crewDir(dm)
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

	m.launching[slug] = true
	m.flash = "launching " + harnessName + " on " + slug + "…"
	b := harness.Brief{Slug: slug, Title: dm.M.Title, Brief: dm.Brief, Dir: dir}
	return m, func() tea.Msg {
		l, err := h.Start(b)
		return deckLaunchReadyMsg{slug: slug, harness: harnessName, launch: l, err: err}
	}
}

// resumeCrew relaunches the mission's most recent crew that recorded a
// conversation id.
func (m deckModel) resumeCrew() (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	if !ok {
		return m, nil
	}
	slug := dm.M.Slug
	if _, live := m.liveRun(slug); live {
		m.focusTerm = true
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
	dir, ok := crewDir(dm)
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
	return m, func() tea.Msg {
		l, err := h.Resume(dir, last.Conversation)
		return deckLaunchReadyMsg{slug: slug, harness: last.Harness, launch: l, err: err}
	}
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

// finishLaunch records the crew and starts its terminal.
func (m deckModel) finishLaunch(msg deckLaunchReadyMsg) (tea.Model, tea.Cmd) {
	delete(m.launching, msg.slug)
	if msg.err != nil {
		m.flash = "launch failed: " + msg.err.Error()
		return m, nil
	}
	crew, err := m.store.StartCrew(msg.slug, msg.harness, msg.launch.Conversation)
	if err != nil {
		m.flash = "launch failed: " + err.Error()
		return m, nil
	}

	cmd := exec.Command(msg.launch.Args[0], msg.launch.Args[1:]...)
	cmd.Dir = msg.launch.Dir
	cmd.Env = crewEnv(msg.slug, crew.ID)
	pane, err := termpane.Start(cmd, m.mainWidth(), m.bodyHeight())
	if err != nil {
		m.store.EndCrew(msg.slug, crew.ID, "failed to start: "+err.Error())
		m.flash = "launch failed: " + err.Error()
		return m, loadDeckCmd(m.ws, m.prs)
	}

	m.store.SetCrewPID(msg.slug, crew.ID, pane.Pid())
	m.crew[msg.slug] = &deckCrewRun{slug: msg.slug, crewID: crew.ID, harness: msg.harness, pane: pane}
	m.focusTerm = m.isSelected(msg.slug)
	m.flash = ""
	return m, tea.Batch(waitPane(msg.slug, pane), loadDeckCmd(m.ws, m.prs))
}

func (m deckModel) isSelected(slug string) bool {
	dm, ok := m.selectedMission()
	return ok && dm.M.Slug == slug
}

// waitPane blocks until the pane changes or exits.
func waitPane(slug string, p *termpane.Pane) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-p.Updates():
		case <-p.Done():
		}
		return deckPaneMsg{slug: slug}
	}
}

func (m deckModel) handlePaneMsg(msg deckPaneMsg) (tea.Model, tea.Cmd) {
	r, ok := m.crew[msg.slug]
	if !ok || r.ended {
		return m, nil
	}
	if !r.pane.Exited() {
		return m, waitPane(msg.slug, r.pane)
	}
	r.ended = true
	r.exit = r.pane.ExitSummary()
	if m.stopping {
		return m, nil // the stop command records these
	}
	r.record(m.store, r.exit)
	if m.isSelected(r.slug) {
		m.focusTerm = false
	}
	m.flash = fmt.Sprintf("crew %s on %s ended (%s)", r.crewID, r.slug, r.exit)
	return m, loadDeckCmd(m.ws, m.prs)
}

// resizePanes keeps every crew terminal the size of the main pane.
func (m deckModel) resizePanes() {
	for _, r := range m.crew {
		if r.live() {
			r.pane.Resize(m.mainWidth(), m.bodyHeight())
		}
	}
}

// --- terminal focus ---

// handleTermKey routes keys while a crew terminal has focus: everything goes
// to the agent except the prefix key, which takes one deck command.
func (m deckModel) handleTermKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	r, live := (*deckCrewRun)(nil), false
	if ok {
		r, live = m.liveRun(dm.M.Slug)
	}
	if !live {
		m.focusTerm, m.prefix = false, false
		return m, nil
	}

	if !m.prefix {
		if msg.String() == deckPrefixKey {
			m.prefix = true
			return m, nil
		}
		r.pane.SendKey(uv.KeyPressEvent(msg))
		return m, nil
	}

	m.prefix = false
	switch msg.String() {
	case deckPrefixKey:
		r.pane.SendKey(uv.KeyPressEvent(msg))
	case "tab", "esc", "h", "left":
		m.focusTerm = false
	case "s", "z":
		m.sidebarHidden = !m.sidebarHidden
		m.resizePanes()
	case "S":
		return m.confirmStop(r)
	case "?":
		m.showHelp = true
	case "q":
		return m.requestQuit()
	}
	return m, nil
}

// --- stopping ---

func (m deckModel) confirmStop(r *deckCrewRun) (tea.Model, tea.Cmd) {
	slug := r.slug
	m.openConfirm(fmt.Sprintf("Stop crew %s on %s?", r.crewID, slug), func() tea.Msg {
		r.pane.Close(deckStopGrace)
		return deckPaneMsg{slug: slug}
	})
	return m, nil
}

func (m deckModel) stopSelected() (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	if !ok {
		return m, nil
	}
	r, live := m.liveRun(dm.M.Slug)
	if !live {
		m.flash = "no live crew on " + dm.M.Slug
		return m, nil
	}
	return m.confirmStop(r)
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
	m.focusTerm, m.prefix = false, false
	m.openConfirm(fmt.Sprintf("Stop %d live %s and quit?", n, noun), func() tea.Msg {
		return deckQuitConfirmedMsg{}
	})
	return m, nil
}

type deckQuitConfirmedMsg struct{}

// stopAll hangs up every live crew in parallel, records how each ended, then
// quits.
func (m deckModel) stopAll() (tea.Model, tea.Cmd) {
	m.stopping = true
	m.flash = "stopping crew…"
	var runs []*deckCrewRun
	for _, r := range m.crew {
		if r.live() {
			runs = append(runs, r)
		}
	}
	store := m.store
	return m, func() tea.Msg {
		var wg sync.WaitGroup
		for _, r := range runs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r.pane.Close(deckStopGrace)
				r.record(store, "stopped: "+r.pane.ExitSummary())
			}()
		}
		wg.Wait()
		return deckStoppedMsg{}
	}
}

// --- rendering ---

// renderPane draws a crew terminal at exactly width×height cells.
func renderPane(p *termpane.Pane, width, height int) string {
	lines := strings.Split(p.Render(), "\n")
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
	if m.launching[slug] {
		return ui.Dim.Render("launching crew…")
	}
	if r, ok := m.crew[slug]; ok && r.ended {
		line := fmt.Sprintf("crew %s (%s) ended: %s · ⏎ new crew", r.crewID, r.harness, r.exit)
		if canResume(dm.M) {
			line += " · R resume"
		}
		return ui.Dim.Render(line)
	}
	if len(dm.M.Crew) > 0 {
		last := dm.M.Crew[len(dm.M.Crew)-1]
		line := fmt.Sprintf("%d crew so far, last %s (%s) · ⏎ launch %s", len(dm.M.Crew), last.ID, last.Harness, harness.Default())
		if canResume(dm.M) {
			line += " · R resume"
		}
		return ui.Dim.Render(line)
	}
	return ui.Dim.Render("no crew yet · ⏎ launch " + harness.Default() + " · L choose harness")
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

type deckLaunchWithMsg struct{ harness string }

// stopLeftovers deals with crew still around when the program ends some
// other way than a confirmed quit (SIGTERM, a crash in Update, a closed
// terminal): it hangs up the running ones and records every unrecorded end.
// m.crew is shared by every copy of the model, so the initial model sees all
// crew the program launched.
func (m deckModel) stopLeftovers() {
	var wg sync.WaitGroup
	for _, r := range m.crew {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r.pane.Exited() {
				r.record(m.store, r.pane.ExitSummary())
				return
			}
			r.pane.Close(deckStopGrace)
			r.record(m.store, "stopped: deck exited")
		}()
	}
	wg.Wait()
}

// processAlive reports whether pid is a running process.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
