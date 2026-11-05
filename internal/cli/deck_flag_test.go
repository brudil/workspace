package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/mission"
)

// spawnedRuns records orchestrator runs the deck starts, for the test.
func spawnedRuns(t *testing.T) *[]string {
	t.Helper()
	old := orchestrateSpawner
	var runs []string
	orchestrateSpawner = func(root, slug, crewID, trigger string) { runs = append(runs, slug+"/"+trigger) }
	t.Cleanup(func() { orchestrateSpawner = old })
	return &runs
}

// pickAndRun opens the picker on ⏎, picks item i, and runs what follows.
func pickAndRun(t *testing.T, m deckModel, i int) (deckModel, tea.Cmd) {
	t.Helper()
	for range i {
		m, _ = press(t, m, "j")
	}
	m, cmd := press(t, m, "enter")
	next, follow := m.Update(cmd())
	return next.(deckModel), follow
}

func TestDeckFlag_EnterOffersTheAnswersAndAnswerRunsTheOrchestrator(t *testing.T) {
	m, slug := shellCrewDeck(t)
	runs := spawnedRuns(t)
	m.store.SetFlag(slug, "Commit and open PRs?", "orchestrator", "Commit both and open PRs", "Leave them uncommitted")
	m, _ = reloadRecords(m)
	m = selectMission(t, m, slug)

	view := plainView(m)
	for _, want := range []string{"! The orchestrator asks: Commit and open PRs?", "1. Commit both and open PRs", "2. Leave them uncommitted", "⏎ answer · F dismiss", "! asks you"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}

	m, _ = press(t, m, "enter")
	if m.mode != deckModePick || strings.Join(m.pick.items, "|") != "Commit both and open PRs|Leave them uncommitted|Write my own answer…|Launch crew instead|Dismiss the question" {
		t.Fatalf("picker: mode %v, items %v", m.mode, m.pick)
	}
	m, answer := pickAndRun(t, m, 0)
	if msg, ok := answer().(deckReloadMsg); !ok || !strings.Contains(msg.flash, "the orchestrator is acting on it") {
		t.Errorf("answering said %#v", msg)
	}
	got, _ := m.store.Load(slug)
	if got.Flag != nil || got.Answer == nil || got.Answer.Text != "Commit both and open PRs" || strings.Join(*runs, ",") != slug+"/answer" {
		t.Errorf("flag %+v, answer %+v, runs %v", got.Flag, got.Answer, *runs)
	}
}

func TestDeckFlag_WriteYourOwnAnswer(t *testing.T) {
	m, slug := shellCrewDeck(t)
	runs := spawnedRuns(t)
	m.store.SetFlag(slug, "Which cache?", "orchestrator") // no options: only your own answer
	m, _ = reloadRecords(m)
	m = selectMission(t, m, slug)

	m, _ = press(t, m, "enter")
	m, _ = pickAndRun(t, m, 0) // Write my own answer…
	if m.mode != deckModeNewTitle || !m.answering {
		t.Fatalf("no answer field: mode %v", m.mode)
	}
	if !strings.Contains(m.renderFooter(), "Your answer:") {
		t.Errorf("footer = %q", m.renderFooter())
	}
	for _, r := range "Per-org, keyed by tenant" {
		next, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(deckModel)
	}
	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatal("enter didn't answer")
	}
	cmd()
	got, _ := m.store.Load(slug)
	if got.Answer == nil || got.Answer.Text != "Per-org, keyed by tenant" || len(*runs) != 1 || m.answering {
		t.Errorf("answer %+v, runs %v, answering %v", got.Answer, *runs, m.answering)
	}
	if events, _ := m.store.ReadLog(slug, 0); events[len(events)-1].Kind != mission.EventFlagAnswered {
		t.Errorf("last event %+v", events[len(events)-1])
	}
}
