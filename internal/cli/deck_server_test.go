package cli

import (
	"errors"
	"github.com/brudil/workspace/internal/crew"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/deckserver"
	"github.com/brudil/workspace/internal/harness"
)

func TestDeckServer_DetachAndReattach(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("ENV", "")
	t.Setenv("PS1", "$ ")
	t.Setenv(harness.EnvDefault, "shell")
	ws := deckWorkspace(t, map[string][]string{"frontend": {"work"}})
	slug := seedMission(t, ws, "Served", "", [2]string{"frontend", "work"})

	srv := &deckserver.Server{Root: ws.Root, IdleTimeout: time.Minute}
	if err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Shutdown("test over") })

	open := func() deckModel {
		m := newDeckModel(ws, nil, "")
		m.host = deckserver.NewHost(ws.Root)
		for _, c := range m.adoptHosted() {
			_ = c // wait commands; the test polls screens directly
		}
		next, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 24})
		data, _ := loadDeckData(ws, prIndex{})
		next, _ = next.Update(deckLoadedMsg{data: data})
		return next.(deckModel)
	}

	// First deck: launch crew and type into it.
	m := open()
	m = launch(t, m)
	r, ok := liveRun(m, slug)
	if !ok {
		t.Fatal("no crew")
	}
	waitScreen(t, r, "$")
	m = typeText(m, "echo first-deck")
	waitScreen(t, r, "\nfirst-deck")

	// Quit, choosing to detach.
	m.focusTerm = false
	m, _ = press(t, m, "q")
	if m.mode != deckModePick || !strings.Contains(m.pick.items[0], "Detach") {
		t.Fatalf("quit didn't offer detach: mode %v", m.mode)
	}
	m, cmd := press(t, m, "enter")
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("detach didn't quit")
	}
	m.stopLeftovers()

	c, err := deckserver.Dial(ws.Root)
	if err != nil {
		t.Fatal(err)
	}
	crews, _ := c.List()
	c.Close()
	if len(crews) != 1 || crews[0].Spec.Slug != slug {
		t.Fatalf("crew didn't survive detaching: %+v", crews)
	}

	// Second deck: reattaches, sees the old screen, keeps going.
	m2 := open()
	r2, ok := liveRun(m2, slug)
	if !ok {
		t.Fatal("second deck didn't reattach the crew")
	}
	if r2.crewID != "c1" || r2.capsule != "frontend/work" {
		t.Errorf("reattached run = %+v", r2)
	}
	waitScreen(t, r2, "first-deck")
	found := false
	for _, row := range m2.rows {
		if row.kind == deckRowCrew && row.run == r2 {
			found = true
		}
	}
	if !found {
		t.Error("reattached crew has no sidebar row")
	}
	m2.focusRun(r2)
	m2 = typeText(m2, "echo second-deck")
	waitScreen(t, r2, "\nsecond-deck")

	// Stopping goes through the server, which records the end.
	m2, _ = prefixKey(m2, tea.KeyPressMsg{Code: 'S', Text: "S"})
	m2, cmd = press(t, m2, "y")
	next, _ := m2.Update(cmd())
	m2 = next.(deckModel)
	if _, live := liveRun(m2, slug); live {
		t.Error("crew still live after stop")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, _ := m2.store.Load(slug)
		if got.Crew[0].Ended != nil {
			if got.Crew[0].Exit != "hangup" {
				t.Errorf("exit = %q", got.Crew[0].Exit)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server didn't record the end")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := crewEnded(t, m2, slug); n != 1 {
		t.Errorf("crew_ended logged %d times", n)
	}
}

// fakeTerm records resizes; everything else is inert.
type fakeTerm struct {
	crew.Term
	resizes [][2]int
	done    chan struct{}
}

func (f *fakeTerm) Resize(w, h int)          { f.resizes = append(f.resizes, [2]int{w, h}) }
func (f *fakeTerm) Updates() <-chan struct{} { return nil }
func (f *fakeTerm) Done() <-chan struct{}    { return f.done }
func (f *fakeTerm) Close(time.Duration)      {}
func (f *fakeTerm) Exited() bool             { return false }

// fakeHost is a detachable host already running some crew.
type fakeHost struct {
	running []crew.Hosted
}

func (fakeHost) Launch(crew.Spec) (crew.Term, error) {
	return nil, errors.New("fake host launches nothing")
}
func (f fakeHost) Running(skip func(string) bool) []crew.Hosted {
	var out []crew.Hosted
	for _, h := range f.running {
		if skip == nil || !skip(h.Spec.Key()) {
			out = append(out, h)
		}
	}
	return out
}
func (fakeHost) Detachable() bool { return true }
func (fakeHost) Leave()           {}

func TestDeckServer_AdoptDoesntResizeBeforeSizeKnown(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": nil})
	term := &fakeTerm{done: make(chan struct{})}
	m := newDeckModel(ws, nil, "")
	m.host = fakeHost{running: []crew.Hosted{{Spec: crew.Spec{Slug: "s", CrewID: "c1"}, Term: term}}}

	m.adoptHosted() // as Init does, before any WindowSizeMsg
	if len(term.resizes) != 0 {
		t.Fatalf("resized before the deck knew its size: %v", term.resizes)
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 24})
	m = next.(deckModel)
	if len(term.resizes) != 1 || term.resizes[0] != [2]int{m.mainWidth(), m.bodyHeight()} {
		t.Errorf("resizes = %v, want one to %d×%d", term.resizes, m.mainWidth(), m.bodyHeight())
	}
}
