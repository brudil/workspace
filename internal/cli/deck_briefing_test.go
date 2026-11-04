package cli

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/mission"
)

// runAll executes cmd (expanding batches) and feeds every message back into
// the model, as the program would, for messages the deck acts on.
func runAll(t *testing.T, m deckModel, cmd tea.Cmd) deckModel {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case deckLaunchReadyMsg, deckLoadedMsg:
			next, follow := m.Update(msg)
			m = next.(deckModel)
			if _, ok := msg.(deckLaunchReadyMsg); ok {
				queue = append(queue, follow) // the reload that follows a launch
			}
		}
	}
	return m
}

func newBriefing(t *testing.T, m deckModel, request string) deckModel {
	t.Helper()
	m, _ = press(t, m, "n")
	if m.mode != deckModeNewTitle || !m.newBriefing {
		t.Fatalf("n didn't ask for a request (mode %v)", m.mode)
	}
	for _, r := range request {
		next, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(deckModel)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return runAll(t, next.(deckModel), cmd)
}

func TestDeckBriefing_NewLaunchesBriefingCrewInRoot(t *testing.T) {
	m, _ := resumableDeck(t)
	m = newBriefing(t, m, "fe issue 45")

	got, err := m.store.Load("fe-issue-45")
	if err != nil || got.State != mission.Briefing || got.Request != "fe issue 45" {
		t.Fatalf("mission = %+v, %v", got, err)
	}
	r, ok := liveRun(m, "fe-issue-45")
	if !ok || r.role != mission.RoleBriefing || r.dir != m.ws.Root || r.capsule != "workspace" {
		t.Fatalf("briefing run = %+v", r)
	}
	if got.Crew[0].Role != mission.RoleBriefing {
		t.Errorf("crew record role = %q", got.Crew[0].Role)
	}
	if sel, _ := m.selectedRun(); sel != r || !m.focusTerm {
		t.Error("the briefing crew should be selected and focused")
	}
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	view := plainView(m)
	if !strings.Contains(view, "c1 working briefing") {
		t.Errorf("sidebar doesn't mark the briefing crew:\n%s", view)
	}
	m, _ = press(t, m, "g")
	if view := plainView(m); !strings.Contains(view, "Being briefed") || !strings.Contains(view, "Requested: fe issue 45") {
		t.Errorf("mission view doesn't show briefing:\n%s", view)
	}
	// ⏎ on a mission being briefed goes to its briefing crew, not a new one.
	m, _ = press(t, m, "enter")
	if sel, _ := m.focusedRun(); sel != r {
		t.Error("⏎ didn't focus the briefing crew")
	}
}

func TestDeckBriefing_ReadyLandsOnTheReadyMission(t *testing.T) {
	m, _ := resumableDeck(t)
	m = newBriefing(t, m, "fe issue 45")
	r, _ := liveRun(m, "fe-issue-45")

	// What the briefing agent does: brief, capsule, then ws mission ready.
	store := m.store
	store.WriteBrief("fe-issue-45", "## Goal\nFix issue 45.")
	attachCapsule(m.ws, "fe-issue-45", "frontend", "work")
	SetContextOverride(&Context{WS: m.ws})
	t.Cleanup(ClearContextOverride)
	t.Setenv(mission.EnvMission, "fe-issue-45")
	t.Setenv(mission.EnvCrew, r.crewID)
	cmd := NewRootCmd("test")
	cmd.SetArgs([]string{"mission", "ready", "--slug", "fe-45"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	// ready hangs the briefing crew up shortly after.
	select {
	case <-r.term.Done():
	case <-time.After(6 * time.Second):
		t.Fatal("briefing crew wasn't hung up")
	}
	next, reload := m.Update(deckPaneMsg{run: r})
	m = runAll(t, next.(deckModel), reload)

	dm, ok := m.selectedMission()
	if !ok || dm.M.Slug != "fe-45" || dm.State == mission.Briefing {
		t.Fatalf("landed on %+v", dm)
	}
	if !strings.Contains(m.flash, "Briefed: fe issue 45 · 1 capsule(s) · ⏎ to launch crew") {
		t.Errorf("flash = %q", m.flash)
	}
	got, _ := m.store.Load("fe-45")
	if got.Crew[0].Exit != "briefed" {
		t.Errorf("briefing crew exit = %q", got.Crew[0].Exit)
	}
	// The briefing crew is done, not resumable: the mission says who briefed
	// it, and the next ⏎ launches work crew in the capsule straight away.
	if view := plainView(m); !strings.Contains(view, "briefed by c1") || strings.Contains(view, "resume c1") {
		t.Errorf("mission view after briefing:\n%s", view)
	}
	m = launch(t, m)
	work, _ := liveRun(m, "fe-45")
	if work.role != "" || work.capsule != "frontend/work" || work.crewID != "c2" {
		t.Errorf("work crew = %+v", work)
	}
}

func TestDeckBriefing_StoppedEarlyCanResume(t *testing.T) {
	m, _ := resumableDeck(t)
	m = newBriefing(t, m, "api: cache the lookup")
	r, _ := liveRun(m, "api-cache-the-lookup")

	m = stopRun(t, m, r)
	if !strings.Contains(m.flash, "briefing stopped before the mission was ready") {
		t.Errorf("flash = %q", m.flash)
	}
	m, _ = press(t, m, "enter")
	m = choose(t, m, 0) // Resume c1
	again, ok := liveRun(m, "api-cache-the-lookup")
	if !ok || again.crewID != "c1" || again.role != mission.RoleBriefing || again.dir != m.ws.Root {
		t.Errorf("resumed briefing = %+v", again)
	}
}
