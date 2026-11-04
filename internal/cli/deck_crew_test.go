package cli

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
)

// shellCrewDeck is a deck on a mission with one capsule, set to launch the
// shell harness running /bin/sh.
func shellCrewDeck(t *testing.T) (deckModel, string) {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("ENV", "") // no sh startup file
	t.Setenv("PS1", "$ ")
	t.Setenv(harness.EnvDefault, "shell")
	ws := deckWorkspace(t, map[string][]string{"frontend": {"work"}})
	slug := seedMission(t, ws, "Crewed", "", [2]string{"frontend", "work"})
	return loadedDeck(t, ws, prIndex{}), slug
}

// launch presses enter on the selected mission and completes the launch.
func launch(t *testing.T, m deckModel) deckModel {
	t.Helper()
	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatalf("enter didn't launch: flash %q", m.flash)
	}
	ready, ok := cmd().(deckLaunchReadyMsg)
	if !ok {
		t.Fatal("launch command returned the wrong message")
	}
	next, _ := m.Update(ready)
	m = next.(deckModel)
	t.Cleanup(func() {
		for _, r := range m.crew {
			r.pane.Close(time.Second)
		}
	})
	return m
}

func typeText(m deckModel, s string) deckModel {
	for _, r := range s {
		next, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(deckModel)
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return next.(deckModel)
}

func waitScreen(t *testing.T, r *deckCrewRun, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(r.pane.Text(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q:\n%s", want, r.pane.Text())
}

func TestDeckCrew_LaunchRunsInCapsuleWithMissionEnv(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)

	r, live := m.liveRun(slug)
	if !live || r.crewID != "c1" || r.harness != "shell" {
		t.Fatalf("run = %+v", r)
	}
	if !m.focusTerm {
		t.Error("launching should focus the crew terminal")
	}
	got, _ := m.store.Load(slug)
	if len(got.Crew) != 1 || got.Crew[0].Harness != "shell" || got.Crew[0].Ended != nil {
		t.Errorf("crew record = %+v", got.Crew)
	}

	m = typeText(m, `echo "M=$WS_MISSION C=$WS_CREW D=${PWD##*/} T=$TERM"`)
	waitScreen(t, r, "M="+slug+" C=c1 D=work T=xterm-256color")

	// The main pane shows the terminal, and the sidebar marks the live crew.
	view := plainView(m)
	if !strings.Contains(view, "M="+slug) || !strings.Contains(view, "●c1") {
		t.Errorf("view doesn't show the crew:\n%s", view)
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
	if _, live := m.liveRun(slug); !live {
		t.Error("leaving the terminal stopped the crew")
	}

	m, _ = press(t, m, "tab")
	if !m.focusTerm {
		t.Error("tab didn't refocus the live crew")
	}
}

func TestDeckCrew_ExitIsRecorded(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := m.liveRun(slug)

	m = typeText(m, "exit 4")
	select {
	case <-r.pane.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("shell didn't exit")
	}
	next, _ := m.Update(deckPaneMsg{slug: slug})
	m = next.(deckModel)

	if _, live := m.liveRun(slug); live || m.focusTerm {
		t.Errorf("still live/focused after exit")
	}
	got, _ := m.store.Load(slug)
	if got.Crew[0].Ended == nil || got.Crew[0].Exit != "exit 4" {
		t.Errorf("crew = %+v", got.Crew[0])
	}
	if !strings.Contains(m.flash, "ended (exit 4)") {
		t.Errorf("flash = %q", m.flash)
	}
	// Enter launches fresh crew, numbered after the first.
	m.focusTerm = false
	m = launch(t, m)
	if r2, _ := m.liveRun(slug); r2.crewID != "c2" {
		t.Errorf("second crew id = %s", r2.crewID)
	}
}

func TestDeckCrew_StopAsksThenHangsUp(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)

	next, _ := m.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	m = next.(deckModel)
	next, _ = m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	m = next.(deckModel)
	if m.mode != deckModeConfirm {
		t.Fatal("ctrl+g S didn't ask to stop")
	}
	m, cmd := press(t, m, "y")
	next, _ = m.Update(cmd())
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
	got, _ := m.store.Load(slug)
	if got.Crew[0].Exit != "stopped: hangup" {
		t.Errorf("exit = %q", got.Crew[0].Exit)
	}
	events, _ := m.store.ReadLog(slug, 0)
	ended := 0
	for _, e := range events {
		if e.Kind == mission.EventCrewEnded {
			ended++
		}
	}
	if ended != 1 {
		t.Errorf("crew_ended logged %d times", ended)
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
	r, _ := m.liveRun(slug)
	if w, h := r.pane.Size(); w != m.mainWidth() || h != m.bodyHeight() {
		t.Fatalf("pane %d×%d, main %d×%d", w, h, m.mainWidth(), m.bodyHeight())
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = next.(deckModel)
	if w, h := r.pane.Size(); w != m.mainWidth() || h != m.bodyHeight() {
		t.Errorf("after resize pane %d×%d, main %d×%d", w, h, m.mainWidth(), m.bodyHeight())
	}
	// Hiding the sidebar widens the terminal.
	next, _ = m.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	m = next.(deckModel)
	next, _ = m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = next.(deckModel)
	if w, _ := r.pane.Size(); w != 140 {
		t.Errorf("zoomed width = %d", w)
	}
}

func TestDeckCrew_LeftoversAreStoppedAndRecordedOnce(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := m.liveRun(slug)

	got, _ := m.store.Load(slug)
	if got.Crew[0].PID != r.pane.Pid() || got.Crew[0].PID == 0 {
		t.Errorf("pid = %d, pane pid %d", got.Crew[0].PID, r.pane.Pid())
	}

	// The program ends without a confirmed quit.
	m.stopLeftovers()
	if !r.pane.Exited() {
		t.Fatal("crew still running")
	}
	// A late exit message must not record the end again.
	next, _ := m.Update(deckPaneMsg{slug: slug})
	m = next.(deckModel)
	m.stopLeftovers()

	got, _ = m.store.Load(slug)
	if got.Crew[0].Exit != "stopped: deck exited" {
		t.Errorf("exit = %q", got.Crew[0].Exit)
	}
	events, _ := m.store.ReadLog(slug, 0)
	ended := 0
	for _, e := range events {
		if e.Kind == mission.EventCrewEnded {
			ended++
		}
	}
	if ended != 1 {
		t.Errorf("crew_ended logged %d times", ended)
	}
}

func TestDeckCrew_QuitThenLeftoversDoesntDoubleRecord(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	m.focusTerm = false
	m, cmd := press(t, m, "q")
	m, cmd = press(t, m, "y")
	next, cmd := m.Update(cmd())
	m = next.(deckModel)
	cmd()
	m.stopLeftovers() // deck.go always calls this after Run

	events, _ := m.store.ReadLog(slug, 0)
	ended := 0
	for _, e := range events {
		if e.Kind == mission.EventCrewEnded {
			ended++
		}
	}
	if ended != 1 {
		t.Errorf("crew_ended logged %d times", ended)
	}
}
