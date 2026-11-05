package cli

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/mission"
	"github.com/charmbracelet/x/ansi"
)

func TestMissionStatus_GroupsByWhatYouNeedToDo(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		state mission.State
		edit  func(m *mission.Mission, dm *deckMission)
		group deckGroup
		label string
	}{
		{"ready, never started", mission.Draft, nil, groupUpNext, "ready"},
		{"waiting to be briefed", mission.Briefing, nil, groupUpNext, "to brief"},
		{"parked", mission.Parked, nil, groupUpNext, "parked"},
		{"in review", mission.Review, nil, groupInProgress, "in review"},
		{"landed", mission.Landed, nil, groupPast, "landed"},
		{"archived", mission.Archived, nil, groupPast, "archived"},
		{"flagged", mission.Active, func(m *mission.Mission, _ *deckMission) {
			m.Flag = &mission.Proposal{Text: "CI is red", By: "orchestrator"}
		}, groupNeedsYou, "! asks you"},
		{"crew queued", mission.Active, func(m *mission.Mission, _ *deckMission) {
			m.Dispatch = &mission.Dispatch{Text: "implement it"}
		}, groupNeedsYou, "→ next crew"},
		{"looks finished", mission.Review, func(m *mission.Mission, _ *deckMission) {
			m.Conclude = &mission.Proposal{Text: "all merged"}
		}, groupNeedsYou, "✓ wrap up"},
		{"crew worked, nobody now", mission.Active, func(m *mission.Mission, _ *deckMission) {
			m.Crew = []mission.Crew{{ID: "c1", Ended: &now}}
		}, groupNeedsYou, "idle"},
		{"only briefed so far", mission.Draft, func(m *mission.Mission, _ *deckMission) {
			m.Crew = []mission.Crew{{ID: "c1", Role: mission.RoleBriefing, Ended: &now}}
		}, groupUpNext, "ready"},
		{"orchestrator deciding", mission.Active, func(m *mission.Mission, dm *deckMission) {
			m.Crew = []mission.Crew{{ID: "c1", Ended: &now}}
			dm.Events = []mission.Event{{Time: now, Kind: mission.EventOrchestrate, Text: "looking at c1 (done) with x"}}
		}, groupInProgress, "◆ deciding"},
		{"orchestrator decided", mission.Active, func(m *mission.Mission, dm *deckMission) {
			m.Crew = []mission.Crew{{ID: "c1", Ended: &now}}
			dm.Events = []mission.Event{
				{Time: now, Kind: mission.EventOrchestrate, Text: "looking at c1 (done) with x"},
				{Time: now, Kind: mission.EventOrchestrate, Text: "Nothing to do."},
			}
		}, groupNeedsYou, "idle"},
	}
	m := deckModel{reg: &crewRegistry{}, launching: map[string]bool{}}
	for _, c := range cases {
		mm := &mission.Mission{Slug: "s", Title: "T"}
		dm := &deckMission{missionView: missionView{M: mm, State: c.state}}
		if c.edit != nil {
			c.edit(mm, dm)
		}
		st := m.missionStatus(dm)
		if st.group != c.group || ansi.Strip(st.label) != c.label {
			t.Errorf("%s: group %s, label %q; want %s, %q", c.name, deckGroupNames[st.group], ansi.Strip(st.label), deckGroupNames[c.group], c.label)
		}
		if (st.group == groupNeedsYou) != (len(st.why) > 0) {
			t.Errorf("%s: needs you without a reason, or the other way round: %v", c.name, st.why)
		}
	}
}

func TestDeckSidebar_GroupsAndPast(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": {"a"}})
	store := mission.Store{Root: ws.Root}
	ready := seedMission(t, ws, "Ready one", "")
	flagged := seedMission(t, ws, "Flagged one", "", [2]string{"frontend", "a"})
	store.StartCrew(flagged, "cursor", "conv")
	store.EndCrew(flagged, "c1", "exit 0")
	store.SetFlag(flagged, "decide something", "orchestrator")
	old := seedMission(t, ws, "Old one", "")
	if _, err := setMissionState(ws, old, mission.Archived); err != nil {
		t.Fatal(err)
	}

	m := loadedDeck(t, ws, prIndex{})
	view := plainView(m)
	needs, next := strings.Index(view, "NEEDS YOU"), strings.Index(view, "UP NEXT")
	if needs < 0 || next < needs || !strings.Contains(view[needs:next], "Flagged one") || !strings.Contains(view[next:], "Ready one") {
		t.Errorf("groups:\n%s", view)
	}
	if strings.Contains(view, "Old one") || !strings.Contains(view, "▸ Past (1)") {
		t.Errorf("archived mission shown, or no Past entry:\n%s", view)
	}
	lines, _ := m.sidebarLines(deckSidebarWidth)
	if side := ansi.Strip(strings.Join(lines, "\n")); strings.Contains(side, "frontend/a") {
		t.Errorf("sidebar lists capsules:\n%s", side)
	}
	if m.counts() != "1 need you" {
		t.Errorf("header counts = %q", m.counts())
	}
	_ = ready
}

func TestDeckOverview_NeedsYouAndRecent(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": nil})
	store := mission.Store{Root: ws.Root}
	slug := seedMission(t, ws, "Cache tokens", "")
	store.StartCrew(slug, "cursor", "conv")
	store.EndCrew(slug, "c1", "exit 0")
	store.SetFlag(slug, "CI is red", "orchestrator")
	store.AppendLog(slug, mission.Event{Kind: mission.EventOrchestrate, Crew: "orchestrator", Text: "looking at c1 (done) with x"})
	store.AppendLog(slug, mission.Event{Kind: mission.EventNote, Text: "Orchestrator: CI needs fixing first."})

	m := loadedDeck(t, ws, prIndex{})
	if !m.onOverview() {
		t.Fatal("the deck should open on the overview")
	}
	_, content := m.mainContent(m.mainWidth())
	view := ansi.Strip(content)
	for _, want := range []string{
		"What do you want to do?",
		"Cache tokens", "the orchestrator asks: CI is red",
		"orchestrator: CI needs fixing first.", "c1 ended (exit 0)",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("overview missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "looking at") {
		t.Errorf("overview shows the orchestrator starting:\n%s", view)
	}
	if strings.Index(view, "CI needs fixing") > strings.Index(view, "c1 ended") {
		t.Errorf("recent activity isn't newest first:\n%s", view)
	}
}

func TestDeckOverview_WhatDoYouWantToDo(t *testing.T) {
	m, _ := shellCrewDeck(t)
	m, _ = press(t, m, "g") // the overview
	m, _ = press(t, m, "enter")
	if m.mode != deckModeNewTitle || !m.newBriefing {
		t.Fatalf("⏎ on the overview didn't ask (mode %v)", m.mode)
	}
	for _, r := range "fe issue 45" {
		next, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(deckModel)
	}
	if view := plainView(m); !strings.Contains(view, "fe issue 45") || !strings.Contains(view, "⏎ start it") {
		t.Errorf("the field isn't on the overview:\n%s", view)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = runAll(t, next.(deckModel), cmd)
	got, err := m.store.Load("fe-issue-45")
	if err != nil || got.State != mission.Briefing {
		t.Fatalf("mission = %+v, %v", got, err)
	}
	if r, ok := liveRun(m, "fe-issue-45"); !ok || r.role != mission.RoleBriefing {
		t.Errorf("no briefing crew: %+v", r)
	}
}

func TestDeckProgramStatus(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": nil})
	store := mission.Store{Root: ws.Root}
	seedMission(t, ws, "Ready one", "")
	if ps := loadedDeck(t, ws, prIndex{}).programStatus(); ps != nil {
		t.Errorf("all quiet, yet reported %+v", *ps)
	}

	deciding := seedMission(t, ws, "Deciding one", "")
	store.StartCrew(deciding, "cursor", "conv")
	store.EndCrew(deciding, "c1", "exit 0")
	store.AppendLog(deciding, mission.Event{Kind: mission.EventOrchestrate, Crew: "orchestrator", Text: "looking at c1 (done) with x"})
	ps := loadedDeck(t, ws, prIndex{}).programStatus()
	if ps == nil || ps.State != tea.ProgramStateWorking || ps.Message != "1 in progress" {
		t.Errorf("orchestrator deciding: %+v", ps)
	}

	flagged := seedMission(t, ws, "Flagged one", "")
	store.StartCrew(flagged, "cursor", "conv")
	store.EndCrew(flagged, "c1", "exit 0")
	store.SetFlag(flagged, "ship it?", "orchestrator")
	m := loadedDeck(t, ws, prIndex{})
	ps = m.programStatus()
	if ps == nil || ps.State != tea.ProgramStateBlocked || ps.Kind != tea.ProgramStatusKindQuestion ||
		ps.Message != "Flagged one: the orchestrator asks: ship it?" || ps.App != "ws" {
		t.Errorf("flagged: %+v", ps)
	}
	if v := m.View(); v.ProgramStatus == nil || !v.ReportFocus {
		t.Errorf("view: status %+v, focus reports %v", v.ProgramStatus, v.ReportFocus)
	}
}

func TestDeckNotify_OnlyWhileYoureAway(t *testing.T) {
	m := deckModel{}
	if m.notify("x") == nil {
		t.Error("focus unknown (the terminal may not report it): no notification")
	}
	next, _ := m.Update(tea.FocusMsg{})
	if m = next.(deckModel); m.notify("x") != nil {
		t.Error("notified while you're looking at the deck")
	}
	next, _ = m.Update(tea.BlurMsg{})
	if m = next.(deckModel); m.notify("x") == nil {
		t.Error("no notification while you're elsewhere")
	}
}

// Long lines on the overview end in an ellipsis inside the pane, under
// Recently (whose times are wider than Needs you's bullets) as well.
func TestDeckOverview_LongLinesFit(t *testing.T) {
	ws := deckWorkspace(t, map[string][]string{"frontend": nil})
	store := mission.Store{Root: ws.Root}
	slug := seedMission(t, ws, "Cache tokens", "")
	long := strings.Repeat("a long note about caching ", 12)
	store.AppendLog(slug, mission.Event{Kind: mission.EventNote, Text: long})
	store.SetFlag(slug, long, "orchestrator")

	m := loadedDeck(t, ws, prIndex{})
	_, content := m.mainContent(m.mainWidth())
	seen := 0
	for _, l := range strings.Split(ansi.Strip(content), "\n") {
		l = strings.TrimRight(l, " ") // the ask box pads to the pane's width
		if w := ansi.StringWidth(l); w > m.mainWidth()-2 {
			t.Errorf("%d columns, past the pane's margin (%d):\n%s", w, m.mainWidth()-2, l)
		}
		if strings.Contains(l, "a long note") {
			seen++
			if !strings.HasSuffix(l, "…") {
				t.Errorf("cut without an ellipsis:\n%s", l)
			}
		}
	}
	if seen < 2 {
		t.Errorf("want the note under Needs you and Recently:\n%s", ansi.Strip(content))
	}
}
