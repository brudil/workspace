package cli

import (
	"github.com/brudil/workspace/internal/crew"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
)

// shellCrewDeck is a deck on a mission owning the given capsules (in
// frontend), set to launch the shell harness running /bin/sh.
func shellCrewDeck(t *testing.T, capsules ...string) (deckModel, string) {
	t.Helper()
	if len(capsules) == 0 {
		capsules = []string{"work"}
	}
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("ENV", "") // no sh startup file
	t.Setenv("PS1", "$ ")
	t.Setenv(harness.EnvDefault, "shell")
	ws := deckWorkspace(t, map[string][]string{"frontend": capsules})
	var owned [][2]string
	for _, c := range capsules {
		owned = append(owned, [2]string{"frontend", c})
	}
	slug := seedMission(t, ws, "Crewed", "", owned...)
	m := loadedDeck(t, ws, prIndex{}) // its cleanup stops the crew
	return selectMission(t, m, slug), slug
}

// selectMission puts the cursor on a mission's row (the deck opens on the
// overview, and missions move between groups as their crew change).
func selectMission(t *testing.T, m deckModel, slug string) deckModel {
	t.Helper()
	for i, r := range m.rows {
		if r.kind == deckRowMission && m.data.Missions[r.mission].M.Slug == slug {
			m.cursor = i
			return m
		}
	}
	t.Fatalf("no sidebar row for mission %s", slug)
	return m
}

// launch presses enter on the selected row and completes the launch.
func launch(t *testing.T, m deckModel) deckModel {
	t.Helper()
	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatalf("enter didn't launch: flash %q", m.flash)
	}
	return completeLaunch(t, m, cmd)
}

func completeLaunch(t *testing.T, m deckModel, cmd tea.Cmd) deckModel {
	t.Helper()
	msg := cmd()
	if now, ok := msg.(deckLaunchNowMsg); ok {
		next, c := m.Update(now)
		m, msg = next.(deckModel), c()
	}
	ready, ok := msg.(deckLaunchReadyMsg)
	if !ok {
		t.Fatalf("launch command returned %T", msg)
	}
	next, _ := m.Update(ready)
	return next.(deckModel)
}

// liveRun is the mission's first live crew.
func liveRun(m deckModel, slug string) (*deckCrewRun, bool) {
	runs := m.reg.live(slug)
	if len(runs) == 0 {
		return nil, false
	}
	return runs[0], true
}

func typeText(m deckModel, s string) deckModel {
	for _, r := range s {
		next, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(deckModel)
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return next.(deckModel)
}

func prefixKey(m deckModel, k tea.KeyPressMsg) (deckModel, tea.Cmd) {
	next, _ := m.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	next, cmd := next.Update(k)
	return next.(deckModel), cmd
}

func waitScreen(t *testing.T, r *deckCrewRun, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(r.term.Text(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q:\n%s", want, r.term.Text())
}

func crewEnded(t *testing.T, m deckModel, slug string) int {
	t.Helper()
	events, _ := m.store.ReadLog(slug, 0)
	n := 0
	for _, e := range events {
		if e.Kind == mission.EventCrewEnded {
			n++
		}
	}
	return n
}

func TestDeckCrew_LaunchRunsInCapsuleWithMissionEnv(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)

	r, live := liveRun(m, slug)
	if !live || r.crewID != "c1" || r.harness != "shell" || r.capsule != "frontend/work" {
		t.Fatalf("run = %+v", r)
	}
	if sel, ok := m.selectedRun(); !ok || sel != r || !m.focusTerm {
		t.Error("launching should select and focus the new crew")
	}
	got, _ := m.store.Load(slug)
	if len(got.Crew) != 1 || got.Crew[0].Harness != "shell" || got.Crew[0].Ended != nil || got.Crew[0].PID != r.term.Pid() {
		t.Errorf("crew record = %+v", got.Crew)
	}

	m = typeText(m, `echo "M=$WS_MISSION C=$WS_CREW D=${PWD##*/} T=$TERM"`)
	waitScreen(t, r, "M="+slug+" C=c1 D=work T=xterm-256color")

	view := plainView(m)
	if !strings.Contains(view, "M="+slug) || !strings.Contains(view, "c1 working") {
		t.Errorf("view doesn't show the crew:\n%s", view)
	}
}

func TestDeckCrew_MissionRowShowsBriefWhileCrewRuns(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = selectMission(t, m, slug)
	if _, ok := m.shownRun(); ok {
		t.Fatal("mission row should show the mission, not a terminal")
	}
	view := plainView(m)
	if !strings.Contains(view, "c1 working shell in frontend/work") || !strings.Contains(view, "Capsules") {
		t.Errorf("mission view doesn't list live crew:\n%s", view)
	}
	// tab from the mission row jumps to its crew.
	m, _ = press(t, m, "tab")
	if r, ok := m.focusedRun(); !ok || r.slug != slug {
		t.Error("tab on mission row didn't focus its crew")
	}
}

func TestDeckCrew_SecondCrewTakesAFreeCapsule(t *testing.T) {
	m, slug := shellCrewDeck(t, "one", "two")
	m = launch(t, m)
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = selectMission(t, m, slug)
	m = launch(t, m) // free capsule: no confirm

	runs := m.reg.live(slug)
	if len(runs) != 2 || runs[0].capsule != "frontend/one" || runs[1].capsule != "frontend/two" {
		t.Fatalf("runs = %+v", runs)
	}
	if runs[1].crewID != "c2" {
		t.Errorf("second crew id = %s", runs[1].crewID)
	}
	// Both have rows; the mission row counts them.
	crewRows := 0
	for _, r := range m.rows {
		if r.kind == deckRowCrew {
			crewRows++
		}
	}
	if crewRows != 2 {
		t.Errorf("crew rows = %d", crewRows)
	}
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if view := plainView(m); !strings.Contains(view, "c1 ") || !strings.Contains(view, "c2 ") {
		t.Errorf("sidebar doesn't list both crew:\n%s", view)
	}
}

func TestDeckCrew_SharingACapsuleAsksFirst(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = selectMission(t, m, slug)

	m, cmd := press(t, m, "enter")
	if m.mode != deckModeConfirm || !strings.Contains(m.confirm.prompt, "c1 is already working in frontend/work") {
		t.Fatalf("no share confirm: mode %v", m.mode)
	}
	m, cmd = press(t, m, "n")
	if cmd != nil || len(m.reg.live(slug)) != 1 {
		t.Fatal("n should cancel the launch")
	}

	m, _ = press(t, m, "enter")
	m, cmd = press(t, m, "y")
	m = completeLaunch(t, m, cmd)
	if runs := m.reg.live(slug); len(runs) != 2 || runs[1].capsule != "frontend/work" {
		t.Errorf("runs = %+v", runs)
	}
}

// Crew that reported done have handed back: launching new crew in their
// capsule doesn't ask about sharing it, but offers to end their session
// first, so two agents never work in one worktree.
func TestDeckCrew_LaunchingOverCrewThatReportedDone(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	first, _ := liveRun(m, slug)
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m.store.SetCrewStatus(slug, "c1", mission.CrewDone, "stage one done") // what `ws mission done` does
	m, _ = reloadRecords(m)
	m = selectMission(t, m, slug)

	m, _ = press(t, m, "enter")
	if m.mode != deckModeConfirm || !strings.Contains(m.confirm.prompt, "c1 handed back its work in frontend/work. End its session") {
		t.Fatalf("no handed-back confirm: mode %v, prompt %q", m.mode, m.confirm.prompt)
	}
	m, cmd := press(t, m, "y")
	m = completeLaunch(t, m, cmd)
	select {
	case <-first.term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("c1's session wasn't ended")
	}
	runs := m.reg.all()
	if len(runs) != 2 || runs[1].crewID != "c2" || runs[1].capsule != "frontend/work" {
		t.Fatalf("runs = %+v", runs)
	}
	events, _ := m.store.ReadLog(slug, 0)
	noted := false
	for _, e := range events {
		noted = noted || strings.Contains(e.Text, "ended c1's session in frontend/work")
	}
	if !noted {
		t.Error("the log doesn't record ending c1's session")
	}
}

func TestDeckCrew_CycleBetweenCrew(t *testing.T) {
	m, slug := shellCrewDeck(t, "one", "two")
	m = launch(t, m)
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = selectMission(t, m, slug)
	m = launch(t, m)
	runs := m.reg.live(slug)

	m, _ = prefixKey(m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if r, _ := m.focusedRun(); r != runs[0] {
		t.Errorf("ctrl+g n went to %v", r)
	}
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if r, _ := m.focusedRun(); r != runs[1] {
		t.Errorf("ctrl+g n didn't wrap")
	}
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: 'p', Text: "p"})
	if r, _ := m.focusedRun(); r != runs[0] {
		t.Errorf("ctrl+g p went to %v", r)
	}
}

func TestDeckCrew_PrefixKeyReturnsToSidebar(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)

	// Plain keys go to the shell, not the deck: "q" must not quit.
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = next.(deckModel)
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("q quit the deck while the crew terminal had focus")
		}
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	m = next.(deckModel)
	if !m.prefix {
		t.Fatal("ctrl+g didn't arm the prefix")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(deckModel)
	if m.focusTerm || m.prefix {
		t.Errorf("focusTerm=%v prefix=%v after ctrl+g tab", m.focusTerm, m.prefix)
	}
	if _, live := liveRun(m, slug); !live {
		t.Error("leaving the terminal stopped the crew")
	}
	m, _ = press(t, m, "tab")
	if !m.focusTerm {
		t.Error("tab didn't refocus the crew")
	}
}

func TestDeckCrew_ExitIsRecordedAndRowGoes(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)

	m = typeText(m, "exit 4")
	select {
	case <-r.term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("shell didn't exit")
	}
	next, _ := m.Update(deckPaneMsg{run: r})
	m = next.(deckModel)

	if _, live := liveRun(m, slug); live || m.focusTerm {
		t.Errorf("still live/focused after exit")
	}
	if sel, _ := m.selected(); sel.kind != deckRowMission {
		t.Errorf("cursor should fall back to the mission row, is on %v", sel.kind)
	}
	got, _ := m.store.Load(slug)
	if got.Crew[0].Ended == nil || got.Crew[0].Exit != "exit 4" {
		t.Errorf("crew = %+v", got.Crew[0])
	}
	if !strings.Contains(plainView(m), "c1 (shell) ended: exit 4") {
		t.Errorf("mission view doesn't say how crew ended:\n%s", plainView(m))
	}
	m = launch(t, m)
	if r2, _ := liveRun(m, slug); r2.crewID != "c2" {
		t.Errorf("second crew id = %s", r2.crewID)
	}
}

func TestDeckCrew_StopAsksThenHangsUp(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)

	m, _ = prefixKey(m, tea.KeyPressMsg{Code: 'S', Text: "S"})
	if m.mode != deckModeConfirm {
		t.Fatal("ctrl+g S didn't ask to stop")
	}
	m, cmd := press(t, m, "y")
	next, _ := m.Update(cmd())
	m = next.(deckModel)

	got, _ := m.store.Load(slug)
	if got.Crew[0].Exit != "hangup" {
		t.Errorf("exit = %q", got.Crew[0].Exit)
	}
}

func TestDeckCrew_QuitWithLiveCrewStopsThem(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	m.focusTerm = false

	m, cmd := press(t, m, "q")
	if cmd != nil || m.mode != deckModeConfirm || !strings.Contains(m.confirm.prompt, "Stop 1 live crew member") {
		t.Fatalf("q with live crew should confirm; mode %v", m.mode)
	}
	m, cmd = press(t, m, "y")
	next, cmd := m.Update(cmd())
	m = next.(deckModel)
	if !m.stopping {
		t.Fatal("not stopping")
	}
	if _, ok := cmd().(deckStoppedMsg); !ok {
		t.Fatal("stop command didn't finish")
	}
	m.stopLeftovers() // deck.go always calls this after Run
	got, _ := m.store.Load(slug)
	// Recorded as the server records a stopped crew.
	if got.Crew[0].Exit != "hangup" {
		t.Errorf("exit = %q", got.Crew[0].Exit)
	}
	if n := crewEnded(t, m, slug); n != 1 {
		t.Errorf("crew_ended logged %d times", n)
	}
}

func TestDeckCrew_LeftoversAreStoppedAndRecordedOnce(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)

	m.stopLeftovers()
	if !r.term.Exited() {
		t.Fatal("crew still running")
	}
	next, _ := m.Update(deckPaneMsg{run: r})
	m = next.(deckModel)
	m.stopLeftovers()

	got, _ := m.store.Load(slug)
	// The host shutting down, as the server records it.
	if got.Crew[0].Exit != "stopped: hangup" {
		t.Errorf("exit = %q", got.Crew[0].Exit)
	}
	if n := crewEnded(t, m, slug); n != 1 {
		t.Errorf("crew_ended logged %d times", n)
	}
}

func TestDeckCrew_NeedsACapsule(t *testing.T) {
	t.Setenv(harness.EnvDefault, "shell")
	ws := deckWorkspace(t, map[string][]string{"frontend": nil})
	slug := seedMission(t, ws, "Empty", "")
	m := selectMission(t, loadedDeck(t, ws, prIndex{}), slug)
	m, cmd := press(t, m, "enter")
	if cmd != nil || !strings.Contains(m.flash, "no capsule to work in") {
		t.Errorf("flash = %q", m.flash)
	}
}

func TestDeckCrew_ResizeFollowsMainPane(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)
	pane := r.term.(interface{ Size() (int, int) })
	if w, h := pane.Size(); w != m.mainWidth() || h != m.bodyHeight() {
		t.Fatalf("pane %d×%d, main %d×%d", w, h, m.mainWidth(), m.bodyHeight())
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = next.(deckModel)
	if w, h := pane.Size(); w != m.mainWidth() || h != m.bodyHeight() {
		t.Errorf("after resize pane %d×%d, main %d×%d", w, h, m.mainWidth(), m.bodyHeight())
	}
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: 's', Text: "s"})
	if w, _ := pane.Size(); w != 140 {
		t.Errorf("sidebar-hidden width = %d", w)
	}
}

func TestDeckCrew_ScrollModeShowsHistory(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)
	m = typeText(m, `i=1; while [ $i -le 60 ]; do echo row$i; i=$((i+1)); done`)
	waitScreen(t, r, "row60")

	if strings.Contains(plainView(m), "row10\n") {
		t.Fatal("row10 visible before scrolling")
	}
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: '[', Text: "["})
	if !m.scrollMode || r.scroll == 0 {
		t.Fatalf("scrollMode=%v scroll=%d", m.scrollMode, r.scroll)
	}
	m, _ = press(t, m, "g") // to the top of history
	view := plainView(m)
	if !strings.Contains(view, "row1") || !strings.Contains(view, "↑") {
		t.Errorf("top of history not shown:\n%s", view)
	}
	// Keys in scroll mode don't reach the agent.
	before := r.term.Text()
	m, _ = press(t, m, "k", "j")
	if r.term.Text() != before {
		t.Error("scroll keys were typed into the shell")
	}
	m, _ = press(t, m, "esc")
	if m.scrollMode || r.scroll != 0 {
		t.Errorf("esc: scrollMode=%v scroll=%d", m.scrollMode, r.scroll)
	}
}

func TestDeckCrew_WheelScrollsHistoryAndTypingReturnsToLive(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)
	m = typeText(m, `i=1; while [ $i -le 60 ]; do echo row$i; i=$((i+1)); done`)
	waitScreen(t, r, "row60")

	next, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 5, Y: 5})
	m = next.(deckModel)
	if r.scroll != 3 {
		t.Fatalf("scroll = %d after one wheel notch", r.scroll)
	}
	// The wheel's scroll is soft: typing returns to the live screen and the
	// keys reach the crew; ctrl+g still starts a deck command.
	m = typeText(m, "echo back-live")
	if r.scroll != 0 || m.scrollMode {
		t.Errorf("typing should return to the live screen, scroll = %d", r.scroll)
	}
	m, _ = press(t, m, "enter")
	waitScreen(t, r, "back-live")

	next, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 5, Y: 5})
	m = next.(deckModel)
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focusTerm || m.scrollMode || r.scroll != 0 {
		t.Errorf("ctrl+g tab after a wheel scroll: focus %v, scroll mode %v, scroll %d", m.focusTerm, m.scrollMode, r.scroll)
	}
}

func TestDeckCrew_ScrollUnavailableFullScreen(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)
	m = typeText(m, `printf '\033[?1049hFULL'; sleep 5`)
	waitScreen(t, r, "FULL")
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: '[', Text: "["})
	if m.scrollMode || !strings.Contains(m.flash, "full-screen") {
		t.Errorf("scrollMode=%v flash=%q", m.scrollMode, m.flash)
	}
}

func TestDeckCrew_CrewFindsTheDecksWs(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)
	waitScreen(t, r, "$")
	// Only look the commands up: running the test binary as ws would rerun the tests.
	shim := filepath.Join(m.ws.Root, ".ws", "bin")
	m = typeText(m, `for c in ws workspace; do [ "$(command -v $c)" = "`+shim+`/$c" ] && echo "ok-$c"; done`)
	waitScreen(t, r, "\nok-ws\nok-workspace")
}

// resumableHarness is a shell that hands out conversation ids, so resume can
// be tested without an agent.
type resumableHarness struct{}

func (resumableHarness) Name() string     { return "resumable" }
func (resumableHarness) Available() error { return nil }
func (resumableHarness) Start(b harness.Brief) (harness.Launch, error) {
	return harness.Launch{Args: []string{"/bin/sh", "-i"}, Dir: b.Dir, Conversation: "conv-" + filepath.Base(b.Dir)}, nil
}
func (resumableHarness) Resume(dir, conv string) (harness.Launch, error) {
	return harness.Launch{Args: []string{"/bin/sh", "-i"}, Dir: dir, Conversation: conv}, nil
}

func resumableDeck(t *testing.T, capsules ...string) (deckModel, string) {
	t.Helper()
	m, slug := shellCrewDeck(t, capsules...)
	harness.Register(resumableHarness{})
	t.Setenv(harness.EnvDefault, "resumable")
	return m, slug
}

func stopRun(t *testing.T, m deckModel, r *deckCrewRun) deckModel {
	t.Helper()
	m.focusRun(r)
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: 'S', Text: "S"})
	m, cmd := press(t, m, "y")
	next, reload := m.Update(cmd())
	// The deck reloads missions after crew end, as the real program does.
	if reload != nil {
		next, _ = next.Update(reload())
	}
	return next.(deckModel)
}

// choose picks item i in an open picker and completes the launch it starts.
func choose(t *testing.T, m deckModel, i int) deckModel {
	t.Helper()
	if m.mode != deckModePick {
		t.Fatalf("no picker open (mode %v)", m.mode)
	}
	for range i {
		m, _ = press(t, m, "j")
	}
	m, cmd := press(t, m, "enter")
	next, launch := m.Update(cmd())
	return completeLaunch(t, next.(deckModel), launch)
}

func TestDeckCrew_EnterOffersResumeAndKeepsCrewID(t *testing.T) {
	m, slug := resumableDeck(t)
	m = launch(t, m) // nothing to resume yet: launches straight away
	r1, _ := liveRun(m, slug)
	m = stopRun(t, m, r1)

	// ⏎ on the mission now asks, with resume first.
	m = selectMission(t, m, slug)
	m, _ = press(t, m, "enter")
	if m.mode != deckModePick || !strings.HasPrefix(m.pick.items[0], "Resume c1 (resumable") || !strings.HasPrefix(m.pick.items[1], "New crew") {
		t.Fatalf("picker = %+v", m.pick)
	}
	m = choose(t, m, 0)

	r, ok := liveRun(m, slug)
	if !ok || r.crewID != "c1" {
		t.Fatalf("resume should continue c1, got %+v", r)
	}
	got, _ := m.store.Load(slug)
	if len(got.Crew) != 1 || got.Crew[0].Ended != nil || got.Crew[0].Conversation != "conv-work" {
		t.Errorf("crew after resume = %+v", got.Crew)
	}
	events, _ := m.store.ReadLog(slug, 0)
	if last := events[len(events)-1]; last.Kind != "crew_resumed" && events[len(events)-2].Kind != "crew_resumed" {
		t.Errorf("no crew_resumed in log tail: %+v", events[len(events)-2:])
	}
	waitScreen(t, r, "$")
	m = typeText(m, `echo "crew=$WS_CREW"`)
	waitScreen(t, r, "\ncrew=c1")

	// Choosing "New crew" still makes c2.
	m = stopRun(t, m, r)
	m = selectMission(t, m, slug)
	m, _ = press(t, m, "enter")
	m = choose(t, m, 1)
	if r2, _ := liveRun(m, slug); r2.crewID != "c2" {
		t.Errorf("new crew id = %s", r2.crewID)
	}
}

func TestDeckCrew_ResumeReturnsToItsCapsule(t *testing.T) {
	m, slug := resumableDeck(t, "one", "two")
	m = launch(t, m) // c1 in one
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = selectMission(t, m, slug)
	m = launch(t, m) // c2 in two
	runs := m.reg.live(slug)
	m = stopRun(t, m, runs[1])
	m = stopRun(t, m, runs[0])

	// The most recent resumable crew is c2; "one" is free, but c2 worked in two.
	m = selectMission(t, m, slug)
	m, cmd := press(t, m, "R")
	m = completeLaunch(t, m, cmd)
	r, _ := liveRun(m, slug)
	if r.crewID != "c2" || r.capsule != "frontend/two" {
		t.Errorf("resumed %s in %s, want c2 in frontend/two", r.crewID, r.capsule)
	}
}

// promptHarness is a shell whose opening prompt is a command, so the test
// can see the deck typing it in.
type promptHarness struct{}

func (promptHarness) Name() string     { return "prompted" }
func (promptHarness) Available() error { return nil }
func (promptHarness) Start(b harness.Brief) (harness.Launch, error) {
	return harness.Launch{Args: []string{"/bin/sh", "-i"}, Dir: b.Dir, Prompt: `echo "delivered to $WS_CREW"`}, nil
}
func (promptHarness) Resume(string, string) (harness.Launch, error) {
	return harness.Launch{}, harness.ErrNoResume
}

func TestDeckCrew_OpeningPromptIsTypedInOnceDrawn(t *testing.T) {
	old := crew.PromptSettle
	crew.PromptSettle = 300 * time.Millisecond
	t.Cleanup(func() { crew.PromptSettle = old })

	m, slug := shellCrewDeck(t)
	harness.Register(promptHarness{})
	t.Setenv(harness.EnvDefault, "prompted")

	m, cmd := press(t, m, "enter")
	ready := cmd().(deckLaunchReadyMsg)
	if ready.prompt == "" {
		t.Fatal("launch didn't carry the prompt")
	}
	next, follow := m.Update(ready)
	m = next.(deckModel)
	r, _ := liveRun(m, slug)

	// Run the follow-up commands as the program would; one of them types
	// the prompt in.
	var flashes []string
	for _, c := range follow().(tea.BatchMsg) {
		if c == nil {
			continue
		}
		if f, ok := c().(deckFlashMsg); ok {
			flashes = append(flashes, string(f))
		}
	}
	waitScreen(t, r, "\ndelivered to c1")
	if strings.Join(flashes, "|") != "sent the opening prompt to c1" {
		t.Errorf("flashes = %q", flashes)
	}
}

func TestDeckCrew_PromptNotTypedIntoExitedCrew(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)
	r.term.Close(time.Second)
	if msg := deliverPrompt(m.ws.Root, r, "echo late")(); msg != nil {
		t.Errorf("typed into exited crew: %v", msg)
	}
}

func shortIdle(t *testing.T) {
	t.Helper()
	old := crewIdleAfter
	crewIdleAfter = 400 * time.Millisecond
	t.Cleanup(func() { crewIdleAfter = old })
}

func tick(m deckModel) (deckModel, tea.Cmd) {
	next, cmd := m.Update(deckActivityTickMsg{})
	return next.(deckModel), cmd
}

func TestDeckActivity_WorkingThenWaitingNotifies(t *testing.T) {
	shortIdle(t)
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)
	waitScreen(t, r, "$")
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // look away: sidebar

	r.term.(interface{ SendText(string) }).SendText("echo busy\r")
	waitScreen(t, r, "\nbusy")
	m, _ = tick(m)
	if !r.wasWorking {
		t.Fatal("crew that just printed isn't working")
	}
	if !strings.Contains(plainView(m), "●c1 working") {
		t.Errorf("sidebar:\n%s", plainView(m))
	}

	time.Sleep(600 * time.Millisecond)
	m, cmd := tick(m)
	if !strings.Contains(m.flash, "c1 on "+slug+" is waiting for you") {
		t.Errorf("flash = %q", m.flash)
	}
	if !strings.Contains(plainView(m), "◐c1 waiting") {
		t.Errorf("sidebar:\n%s", plainView(m))
	}
	// The tick's commands include the bell/notification (a raw write).
	raw := false
	for _, c := range cmd().(tea.BatchMsg) {
		if c == nil {
			continue
		}
		if _, ok := c().(tea.RawMsg); ok {
			raw = true
		}
	}
	if !raw {
		t.Error("no bell/notification sent")
	}

	// Still waiting: no second notification.
	m.flash = ""
	m, _ = tick(m)
	if m.flash != "" {
		t.Errorf("notified twice: %q", m.flash)
	}
}

func TestDeckActivity_NoNotificationWhileWatching(t *testing.T) {
	shortIdle(t)
	m, slug := shellCrewDeck(t)
	m = launch(t, m) // focused
	r, _ := liveRun(m, slug)
	m = typeText(m, "echo watched")
	waitScreen(t, r, "\nwatched")
	m, _ = tick(m)
	time.Sleep(600 * time.Millisecond)
	m, _ = tick(m)
	if m.flash != "" {
		t.Errorf("notified about the crew you're looking at: %q", m.flash)
	}
}

func reloadRecords(m deckModel) (deckModel, tea.Cmd) {
	next, cmd := m.Update(loadRecordsCmd(m.store)())
	return next.(deckModel), cmd
}

func TestDeckSignals_AskNotifiesAndClearsOnInput(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})

	// What `ws mission ask` does, from the crew.
	m.store.SetCrewStatus(slug, "c1", mission.CrewAsking, "Per-org or global?")
	m, cmd := reloadRecords(m)
	if !strings.Contains(m.flash, "c1 on "+slug+" asks: Per-org or global?") || cmd == nil {
		t.Fatalf("flash = %q", m.flash)
	}
	view := plainView(m)
	if !strings.Contains(view, "?c1 asks") || !strings.Contains(view, "? asks") || !strings.Contains(view, "NEEDS YOU") {
		t.Errorf("sidebar doesn't show the ask:\n%s", view)
	}
	// Announced once.
	m.flash = ""
	m, _ = reloadRecords(m)
	if m.flash != "" {
		t.Errorf("announced twice: %q", m.flash)
	}

	// Sending the crew a message clears it; typing, moving around and
	// scrolling (which a terminal may turn into arrow keys) don't.
	m, _ = press(t, m, "tab")
	for _, k := range []tea.KeyPressMsg{{Code: 'y', Text: "y"}, {Code: tea.KeyUp}, {Code: tea.KeyLeft}} {
		next, cmd := m.Update(k)
		m = next.(deckModel)
		if cmd != nil {
			t.Fatalf("%s cleared the crew's ask before a message was sent", k)
		}
	}
	next, clear := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(deckModel)
	if clear == nil {
		t.Fatal("sending a message to an asking crew didn't clear its status")
	}
	clear()
	if got, _ := m.store.Load(slug); got.Crew[0].Status != "" {
		t.Errorf("status = %q after the user replied", got.Crew[0].Status)
	}
	if strings.Contains(plainView(m), "?c1") {
		t.Error("sidebar still shows the ask")
	}
	_ = r
}

func TestDeckSignals_DoneShowsAndSilencesWaiting(t *testing.T) {
	shortIdle(t)
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	r.term.(interface{ SendText(string) }).SendText("echo finished\r")
	waitScreen(t, r, "\nfinished")
	m, _ = tick(m)

	m.store.SetCrewStatus(slug, "c1", mission.CrewDone, "Added the cache")
	m, _ = reloadRecords(m)
	if !strings.Contains(m.flash, "is done: Added the cache") {
		t.Errorf("flash = %q", m.flash)
	}
	if view := plainView(m); !strings.Contains(view, "✓c1 done") || !strings.Contains(view, "✓ done") {
		t.Errorf("sidebar:\n%s", view)
	}
	// Going quiet after reporting done isn't announced again as waiting.
	m.flash = ""
	time.Sleep(600 * time.Millisecond)
	m, _ = tick(m)
	if m.flash != "" {
		t.Errorf("waiting announced for done crew: %q", m.flash)
	}
	m = selectMission(t, m, slug)
	if !strings.Contains(plainView(m), "✓c1 done shell in frontend/work: Added the cache") {
		t.Errorf("mission view:\n%s", plainView(m))
	}
}

// The deck keeps the mouse even while a crew has the keyboard, so the wheel
// scrolls the crew's history: let go, the terminal would turn the wheel into
// up/down keys and page through the agent's prompt history. M lets go, and
// turns that conversion off until it takes the mouse back.
func TestDeck_MouseCapturedUnlessM(t *testing.T) {
	m, _ := shellCrewDeck(t)
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Errorf("sidebar: mouse mode %v, want captured", got)
	}
	m = launch(t, m) // the crew has the keyboard
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Errorf("focused crew: mouse mode %v, want captured (the wheel scrolls)", got)
	}
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, cmd := press(t, m, "M")
	if got := m.View().MouseMode; got != tea.MouseModeNone || !m.mouseOff {
		t.Errorf("after M: mouse mode %v", got)
	}
	if raw, ok := cmd().(tea.RawMsg); !ok || raw.Msg != altScrollOff {
		t.Errorf("M should save and turn off the terminal's alternate scroll, got %#v", cmd())
	}
	m, cmd = press(t, m, "M")
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Errorf("after M again: mouse mode %v", got)
	}
	if raw, ok := cmd().(tea.RawMsg); !ok || raw.Msg != altScrollRestore {
		t.Errorf("M again should restore alternate scroll, got %#v", cmd())
	}
}

func TestDeck_CrewHyperlinksSurvive(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)
	waitScreen(t, r, "$")
	m = typeText(m, `printf '\033]8;;https://github.com/acme/api/pull/88\033\\PR 88\033]8;;\033\\\n'`)
	waitScreen(t, r, "\nPR 88")
	pane := renderTerm(r.term, 0, m.mainWidth(), m.bodyHeight())
	if !strings.Contains(pane, "\x1b]8;") || !strings.Contains(pane, "https://github.com/acme/api/pull/88") {
		t.Errorf("hyperlink lost on the way to the deck's screen: %q", pane)
	}
}

type spawned struct{ slug, crew, trigger string }

func captureOrchestrations(t *testing.T) *[]spawned {
	t.Helper()
	var calls []spawned
	old := orchestrateSpawner
	orchestrateSpawner = func(root, slug, crewID, trigger string) { calls = append(calls, spawned{slug, crewID, trigger}) }
	t.Cleanup(func() { orchestrateSpawner = old })
	return &calls
}

func TestDeckCrew_ExitSavesTranscriptAndTriggersOrchestrator(t *testing.T) {
	calls := captureOrchestrations(t)
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)
	m = typeText(m, "echo remember-me; exit 3")
	<-r.term.Done()
	next, _ := m.Update(deckPaneMsg{run: r})
	m = next.(deckModel)

	data, err := os.ReadFile(m.store.TranscriptPath(slug, "c1"))
	if err != nil || !strings.Contains(string(data), "remember-me") || !strings.Contains(string(data), "=== c1 run ended") {
		t.Errorf("transcript = %q, %v", data, err)
	}
	if len(*calls) != 1 || (*calls)[0].trigger != "ended" {
		t.Errorf("review calls = %v", *calls)
	}
}

func TestDeckCrew_StoppedCrewNotOrchestrated(t *testing.T) {
	calls := captureOrchestrations(t)
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)
	stopRun(t, m, r)
	if len(*calls) != 0 {
		t.Errorf("stopped crew reviewed: %v", *calls)
	}
}

func TestDeckProposals_ResumeWithAndFlag(t *testing.T) {
	m, slug := resumableDeck(t)
	m = launch(t, m)
	r1, _ := liveRun(m, slug)
	m = stopRun(t, m, r1)

	// The orchestrator's proposals.
	m.store.SetResumeWith(slug, "c1", "add a test for the error path")
	m.store.SetFlag(slug, "CI is red", "orchestrator")
	m, _ = reloadRecords(m)
	m = selectMission(t, m, slug)
	view := plainView(m)
	for _, want := range []string{"! The orchestrator asks: CI is red", "Suggested: resume c1 with: add a test for the error path"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}

	// R resumes c1 and types the suggestion in; the flag clears on acting.
	m, cmd := press(t, m, "R")
	ready := cmd().(deckLaunchReadyMsg)
	if ready.crewID != "c1" || ready.prompt != "add a test for the error path" {
		t.Errorf("resume launch = crew %s prompt %q", ready.crewID, ready.prompt)
	}
	got, _ := m.store.Load(slug)
	if got.Crew[0].ResumeWith != "" || got.Flag != nil {
		t.Errorf("after resume: resume-with %q flag %+v", got.Crew[0].ResumeWith, got.Flag)
	}
	next, _ := m.Update(ready)
	m = next.(deckModel)

	// F dismisses a flag.
	m.store.SetFlag(slug, "look at this", "orchestrator")
	m, _ = reloadRecords(m)
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = selectMission(t, m, slug)
	m, cmd = press(t, m, "F")
	cmd()
	if got, _ := m.store.Load(slug); got.Flag != nil {
		t.Error("F didn't dismiss the flag")
	}
}

func TestDeckDispatch_QueuedLaunchesOnEnter(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m.store.QueueDispatch(slug, &mission.Dispatch{Text: "implement the proposal", Capsule: "frontend/work", By: "orchestrator"})
	m.store.CountDispatch(slug, 3)
	m, _ = reloadRecords(m)
	m = selectMission(t, m, slug)
	view := plainView(m)
	if !strings.Contains(view, "→ Next in frontend/work: implement the proposal (orchestrator)") || !strings.Contains(view, "⏎ launch the queued crew") {
		t.Errorf("view doesn't show the queued dispatch:\n%s", view)
	}

	m = launch(t, m)
	r, ok := liveRun(m, slug)
	if !ok || r.capsule != "frontend/work" {
		t.Fatalf("launched %+v", r)
	}
	got, _ := m.store.Load(slug)
	if got.Dispatch != nil || got.Dispatches != 0 || got.Crew[0].Dispatch != "implement the proposal" {
		t.Errorf("after launch: dispatch %+v, count %d, handoff %q", got.Dispatch, got.Dispatches, got.Crew[0].Dispatch)
	}
}

func TestDeckDispatch_FDismissesQueued(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m.store.QueueDispatch(slug, &mission.Dispatch{Text: "x", By: "orchestrator"})
	m, _ = reloadRecords(m)
	m = selectMission(t, m, slug)
	m, cmd := press(t, m, "F")
	if cmd == nil {
		t.Fatal("F did nothing")
	}
	cmd()
	if got, _ := m.store.Load(slug); got.Dispatch != nil {
		t.Error("queued dispatch not dismissed")
	}
}

func TestDeckAdoptsCrewLaunchedElsewhere(t *testing.T) {
	m, slug := shellCrewDeck(t)
	term := &fakeTerm{done: make(chan struct{})}
	m.host = fakeHost{running: []crew.Hosted{{Spec: crew.Spec{Slug: slug, CrewID: "c1", Capsule: "frontend/work"}, Term: term}}}

	// Nothing recorded as running: no need to ask the server.
	if _, cmd := reloadRecords(m); cmd != nil {
		if msg, ok := findMsg[deckAdoptMsg](cmd); ok {
			t.Fatalf("asked the server with nothing missing: %+v", msg)
		}
	}
	// The orchestrator dispatched c1: the record says it's running.
	m.store.StartCrew(slug, "shell", "")
	m.store.SetCrewProcess(slug, "c1", 4242, "frontend/work")
	m, cmd := reloadRecords(m)
	msg, ok := findMsg[deckAdoptMsg](cmd)
	if !ok {
		t.Fatal("deck didn't look for the dispatched crew")
	}
	next, _ := m.Update(msg)
	m = next.(deckModel)
	if _, ok := liveRun(m, slug); !ok || !strings.Contains(m.flash, "c1 on "+slug+" joined (dispatched)") {
		t.Fatalf("not adopted; flash %q", m.flash)
	}
	// Adopted once.
	next, _ = m.Update(msg)
	if n := len(next.(deckModel).reg.live(slug)); n != 1 {
		t.Errorf("adopted %d times", n)
	}
}

// findMsg runs a (possibly batched) command and returns the first message
// of type T it produces, without running commands that block.
func findMsg[T any](cmd tea.Cmd) (T, bool) {
	var zero T
	if cmd == nil {
		return zero, false
	}
	switch msg := cmd().(type) {
	case T:
		return msg, true
	case tea.BatchMsg:
		for _, c := range msg {
			if c == nil {
				continue
			}
			if got, ok := findMsg[T](c); ok {
				return got, true
			}
		}
	}
	return zero, false
}

func TestDeckAdopt_LookupDoesntReadRunsConcurrently(t *testing.T) {
	m, slug := shellCrewDeck(t)
	running := &deckCrewRun{slug: slug, crewID: "c1", term: &fakeTerm{done: make(chan struct{})}}
	m.reg.add(running)
	m.host = fakeHost{running: []crew.Hosted{
		{Spec: crew.Spec{Slug: slug, CrewID: "c1"}, Term: &fakeTerm{done: make(chan struct{})}},
		{Spec: crew.Spec{Slug: slug, CrewID: "c2"}, Term: &fakeTerm{done: make(chan struct{})}},
	}}
	m.store.StartCrew(slug, "shell", "")
	m.store.StartCrew(slug, "shell", "")
	m.store.SetCrewProcess(slug, "c2", 4242, "frontend/work")
	records := loadRecordsCmd(m.store)().(deckRecordsMsg)

	cmd := m.adoptNewCmd(records.missions)
	if cmd == nil {
		t.Fatal("no lookup for the missing crew")
	}
	got := make(chan tea.Msg)
	go func() { got <- cmd() }()
	running.ended = true // c1 ends on the deck's goroutine meanwhile (go test -race checks this)
	msg := (<-got).(deckAdoptMsg)
	if len(msg.hosted) != 1 || msg.hosted[0].Spec.CrewID != "c2" {
		t.Errorf("found %+v; c1 was live when the lookup started", msg.hosted)
	}
}

func TestDeckF_FlagFirstThenQueuedCrew(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m.store.SetFlag(slug, "decide something", "orchestrator")
	m.store.QueueDispatch(slug, &mission.Dispatch{Text: "next stage", By: "orchestrator"})
	m, _ = reloadRecords(m)
	m = selectMission(t, m, slug)

	m, cmd := press(t, m, "F")
	if msg := cmd().(deckReloadMsg); msg.flash != "flag dismissed" {
		t.Errorf("flash = %q", msg.flash)
	}
	got, _ := m.store.Load(slug)
	if got.Flag != nil || got.Dispatch == nil {
		t.Fatalf("after one F: flag %+v, dispatch %+v", got.Flag, got.Dispatch)
	}
	m, _ = reloadRecords(m)
	m, cmd = press(t, m, "F")
	if msg := cmd().(deckReloadMsg); msg.flash != "queued crew dismissed" {
		t.Errorf("flash = %q", msg.flash)
	}
	if got, _ := m.store.Load(slug); got.Dispatch != nil {
		t.Error("second F didn't dismiss the queued crew")
	}
}

func TestDeckDispatch_QueuedIntoBusyCapsuleAsks(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m) // c1 works in frontend/work
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m.store.QueueDispatch(slug, &mission.Dispatch{Text: "next stage", Capsule: "frontend/work", By: "orchestrator"})
	m, _ = reloadRecords(m)
	m = selectMission(t, m, slug)
	m, _ = press(t, m, "enter")
	if m.confirm == nil || !strings.Contains(plainView(m), "c1 is already working in frontend/work") {
		t.Errorf("launched into a busy capsule without asking; flash %q", m.flash)
	}
}

func TestDeckCrewCopy_ReachesYourClipboard(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)
	waitScreen(t, r, "$")
	r.term.(interface{ SendText(string) }).SendText(`printf '\033]52;c;aGVsbG8=\a'; echo co''pied` + "\r")
	waitScreen(t, r, "\ncopied")
	// Don't run the commands: they'd copy to this machine's clipboard.
	next, cmd := m.handlePaneMsg(deckPaneMsg{run: r})
	m = next.(deckModel)
	if !strings.Contains(m.flash, `c1 copied "hello" to the clipboard`) || cmd == nil {
		t.Errorf("flash %q, cmd %v", m.flash, cmd != nil)
	}
	m.flash = ""
	next, _ = m.handlePaneMsg(deckPaneMsg{run: r})
	if f := next.(deckModel).flash; f != "" {
		t.Errorf("copied twice: %q", f)
	}
}

func TestCopiedSummary(t *testing.T) {
	if got := copiedSummary("one\ntwo\n"); got != "2 lines to the clipboard" {
		t.Errorf("got %q", got)
	}
	if got := copiedSummary("  hello\n"); got != `"hello" to the clipboard` {
		t.Errorf("got %q", got)
	}
}
