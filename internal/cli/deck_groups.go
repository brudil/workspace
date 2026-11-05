package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/orchestrator"
	"github.com/brudil/workspace/internal/ui"
)

// deckGroup is what a mission asks of you, which is how the sidebar groups
// missions.
type deckGroup int

const (
	groupNeedsYou   deckGroup = iota // crew asking or waiting, a flag, queued crew, nothing moving
	groupInProgress                  // crew working, the orchestrator deciding, PRs in review
	groupUpNext                      // to brief, ready to start, parked
	groupPast                        // landed or archived: nothing left to do
)

var deckGroupNames = [...]string{"Needs you", "In progress", "Up next", "Past"}

// deckStatus is where a mission stands for you.
type deckStatus struct {
	group deckGroup
	label string   // short and styled, for the sidebar: "? asks", "● working"
	why   []string // needing you: each reason, for the overview
	rank  int      // order within the group, most pressing first
}

// missionStatus works out a mission's group from its records and its live
// crew. The most urgent reason it needs you sets its label.
func (m deckModel) missionStatus(dm *deckMission) deckStatus {
	slug := dm.M.Slug
	runs := m.reg.live(slug)
	var st deckStatus
	need := func(rank int, label, why string) {
		if len(st.why) == 0 {
			st.rank, st.label = rank, label
		}
		st.why = append(st.why, why)
	}

	working, done := 0, 0
	var waiting []*deckCrewRun
	for _, r := range runs {
		switch status, note := m.crewStatus(r); status {
		case mission.CrewAsking:
			need(0, ui.Orange.Render("? asks"), r.crewID+" asks: "+note)
		case mission.CrewDone:
			done++
		default:
			if w, _ := r.activity(); w {
				working++
			} else {
				waiting = append(waiting, r)
			}
		}
	}
	answering := false
	for _, r := range runs {
		answering = answering || r.role == mission.RoleAnswer
	}
	// Crew answering the question for you have it in hand: they say if
	// they need you.
	if f := dm.M.Flag; f != nil && !answering {
		need(1, ui.Orange.Render("! asks you"), "the orchestrator asks: "+f.Text)
	}
	if d := dm.M.Dispatch; d != nil {
		need(2, ui.Orange.Render("→ next crew"), "next crew is queued: "+d.Text)
	}
	for _, r := range waiting {
		_, since := r.activity()
		need(3, ui.Orange.Render("◐ waiting"), fmt.Sprintf("%s has been waiting for you for %s", r.crewID, shortDuration(since)))
	}
	if c := dm.M.Conclude; c != nil && dm.State != mission.Archived && dm.State != mission.Landed {
		need(4, ui.Green.Render("✓ wrap up"), "looks finished: "+c.Text)
	}
	if len(st.why) > 0 {
		st.group = groupNeedsYou
		return st
	}

	busy := func(rank int, label string) deckStatus {
		return deckStatus{group: groupInProgress, rank: rank, label: label}
	}
	switch {
	case working > 0 && dm.State == mission.Briefing:
		return busy(0, ui.Green.Render("● briefing"))
	case working > 0:
		return busy(0, ui.Green.Render("● working"))
	case len(runs) > 0 && done == len(runs):
		return busy(1, ui.Green.Render("✓ done"))
	case m.launching[slug]:
		return busy(1, ui.Dim.Render("… starting"))
	case orchestrating(dm.Events):
		return busy(2, ui.Blue.Render("◆ deciding"))
	case dm.State == mission.Landed:
		return deckStatus{group: groupPast, label: ui.Green.Render("landed")}
	case dm.State == mission.Archived:
		return deckStatus{group: groupPast, rank: 1, label: ui.Dim.Render("archived")}
	case dm.State == mission.Review:
		return busy(3, ui.Orange.Render("in review"))
	case dm.State == mission.Briefing:
		return deckStatus{group: groupUpNext, label: ui.Orange.Render("to brief")}
	case dm.State == mission.Parked:
		return deckStatus{group: groupUpNext, rank: 2, label: ui.Dim.Render("parked")}
	case !hadWorkCrew(dm.M):
		return deckStatus{group: groupUpNext, rank: 1, label: ui.Blue.Render("ready")}
	}
	// Crew have worked on it, nobody is now, and nothing is pending: it's
	// stalled until you decide what's next.
	st.group = groupNeedsYou
	need(5, ui.Orange.Render("idle"), "nobody's working on it: ⏎ launches crew, p parks it")
	return st
}

// hadWorkCrew reports whether any crew (not just a briefing one) has worked
// on the mission.
func hadWorkCrew(m *mission.Mission) bool {
	for _, c := range m.Crew {
		if c.Role != mission.RoleBriefing {
			return true
		}
	}
	return false
}

// orchestrating reports whether the orchestrator is deciding right now: its
// latest log entry says it started, with no outcome yet (and not so long
// ago that it must have died).
func orchestrating(events []mission.Event) bool {
	for i := len(events) - 1; i >= 0; i-- {
		if e := events[i]; e.Kind == mission.EventOrchestrate {
			return strings.HasPrefix(e.Text, "looking at ") && time.Since(e.Time) < orchestrator.Timeout
		}
	}
	return false
}

// groupCounts is how many missions are in each group.
func (m deckModel) groupCounts() [4]int {
	var n [4]int
	for i := range m.data.Missions {
		n[m.missionStatus(&m.data.Missions[i]).group]++
	}
	return n
}
