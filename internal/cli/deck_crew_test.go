package cli

import (
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
	m := loadedDeck(t, ws, prIndex{})
	t.Cleanup(func() {
		for _, r := range m.reg.all() {
			r.term.Close(time.Second)
		}
	})
	return m, slug
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
	if !strings.Contains(view, "M="+slug) || !strings.Contains(view, "●c1 shell frontend/work") {
		t.Errorf("view doesn't show the crew:\n%s", view)
	}
}

func TestDeckCrew_MissionRowShowsBriefWhileCrewRuns(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, _ = press(t, m, "g") // mission row
	if _, ok := m.shownRun(); ok {
		t.Fatal("mission row should show the mission, not a terminal")
	}
	view := plainView(m)
	if !strings.Contains(view, "●c1 shell in frontend/work") || !strings.Contains(view, "Capsules") {
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
	m, _ = press(t, m, "g")
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
	if !strings.Contains(plainView(m), "●2") {
		t.Errorf("sidebar doesn't count crew:\n%s", plainView(m))
	}
}

func TestDeckCrew_SharingACapsuleAsksFirst(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, _ = press(t, m, "g")

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

func TestDeckCrew_CycleBetweenCrew(t *testing.T) {
	m, slug := shellCrewDeck(t, "one", "two")
	m = launch(t, m)
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, _ = press(t, m, "g")
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
	if got.Crew[0].Exit != "stopped: hangup" {
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
	if got.Crew[0].Exit != "stopped: deck exited" {
		t.Errorf("exit = %q", got.Crew[0].Exit)
	}
	if n := crewEnded(t, m, slug); n != 1 {
		t.Errorf("crew_ended logged %d times", n)
	}
}

func TestDeckCrew_NeedsACapsule(t *testing.T) {
	t.Setenv(harness.EnvDefault, "shell")
	ws := deckWorkspace(t, map[string][]string{"frontend": nil})
	seedMission(t, ws, "Empty", "")
	m := loadedDeck(t, ws, prIndex{})
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
	m, _ = press(t, m, "esc") // leave scroll mode the wheel entered
	m = typeText(m, "true")
	if r.scroll != 0 {
		t.Errorf("typing should return to the live screen, scroll = %d", r.scroll)
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
