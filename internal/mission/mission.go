// Package mission stores missions: durable units of work that own capsules,
// a brief, a log, and the crew (agent sessions) run for them.
//
// Missions live under <workspace-root>/.ws/missions/<slug>/:
//
//	mission.toml  title, stored state, capsules, crew
//	brief.md      goal, constraints, acceptance (human-owned)
//	log.jsonl     append-only events
//
// The package knows nothing about git or GitHub. Callers supply capsule facts
// to DeriveState and do cwd → capsule resolution themselves.
package mission

import (
	"slices"
	"time"
)

// Environment variables set for crew launched on a mission. WS_MISSION also
// lets any ws command run by that crew attach work to the mission.
const (
	EnvMission = "WS_MISSION"
	EnvCrew    = "WS_CREW"
	// EnvRole names a special role a process plays for a mission:
	// RoleOrchestrator.
	EnvRole = "WS_ROLE"
)

// RoleOrchestrator is WS_ROLE for the orchestrator, which decides what
// happens next on a mission.
const RoleOrchestrator = "orchestrator"

// State is a mission's lifecycle state. Only Parked and Archived are stored;
// the rest are derived from capsule and PR facts (see DeriveState).
type State string

const (
	Draft    State = "draft"
	Active   State = "active"
	Review   State = "review"
	Landed   State = "landed"
	Parked   State = "parked"
	Archived State = "archived"
	// Briefing: a briefing crew is turning the request into a brief and
	// capsules. `ws mission ready` ends it.
	Briefing State = "briefing"
)

// RoleBriefing marks crew that brief a mission rather than work on it.
const RoleBriefing = "briefing"

// Capsule identifies a capsule owned by a mission. Repo and Name are its
// identity; Branch is remembered so a PR can still be found after the
// capsule's worktree is burned.
type Capsule struct {
	Repo   string `toml:"repo"`
	Name   string `toml:"name"`
	Branch string `toml:"branch,omitempty"`
}

// Crew records one agent session run for a mission.
type Crew struct {
	ID           string     `toml:"id"`
	Harness      string     `toml:"harness"`
	Conversation string     `toml:"conversation,omitempty"`
	Started      time.Time  `toml:"started"`
	Ended        *time.Time `toml:"ended,omitempty"`
	Exit         string     `toml:"exit,omitempty"`
	// PID of the crew process while it runs, so a later deck can tell a
	// crew that is still running from one whose deck died without recording
	// its end.
	PID int `toml:"pid,omitzero"`
	// Capsule (repo/name) the crew worked in, so resuming goes back there.
	Capsule string `toml:"capsule,omitempty"`
	// Role is "" for crew working on the mission, or RoleBriefing.
	Role string `toml:"role,omitempty"`

	// Status is what the crew last said about itself: CrewAsking (needs the
	// user), CrewDone (finished its part), or "". Note is the question or
	// the summary. Both clear when the user next talks to the crew.
	Status   string     `toml:"status,omitempty"`
	Note     string     `toml:"note,omitempty"`
	StatusAt *time.Time `toml:"status_at,omitempty"`
	// ResumeWith is a message to send when this crew is next resumed
	// (ws mission resume-with); it's used once.
	ResumeWith string `toml:"resume_with,omitempty"`
	// Handoff is what this crew was dispatched to do, when the orchestrator
	// (or the user) dispatched it with ws mission dispatch.
	Handoff string `toml:"handoff,omitempty"`
	// Orchestrated identifies the crew event (a run ending, a done or ask) the
	// orchestrator last looked at, so each is handled once. Times alone can't tell events apart:
	// they're stored to the second.
	Orchestrated string `toml:"orchestrated,omitempty"`
}

// Crew statuses.
const (
	CrewAsking = "asking"
	CrewDone   = "done"
)

// Mission is the contents of mission.toml plus its slug.
type Mission struct {
	Slug    string    `toml:"-"`
	Title   string    `toml:"title"`
	Created time.Time `toml:"created"`
	State   State     `toml:"state,omitempty"` // stored override: "", briefing, parked or archived
	// Request is what the user asked for, verbatim, when a briefing crew
	// created the mission from it.
	Request string `toml:"request,omitempty"`
	// From is the mission/crew ("parent/c2") that raised this one, when
	// crew created it from inside another mission.
	From     string    `toml:"from,omitempty"`
	Capsules []Capsule `toml:"capsules,omitempty"`
	Crew     []Crew    `toml:"crew,omitempty"`
	// Flag marks the mission as needing the user, with why (ws mission flag).
	Flag *Proposal `toml:"flag,omitempty"`
	// Conclude is a suggestion that the mission is finished (ws mission
	// conclude); archiving it is the user's call.
	Conclude *Proposal `toml:"conclude,omitempty"`
	// Dispatch is crew the orchestrator wants launched, waiting for the user
	// ([orchestrator] dispatch = "ask", or no deck server to launch on).
	Dispatch *Dispatch `toml:"dispatch,omitempty"`
	// Dispatches counts crew the orchestrator dispatched since the user last
	// stepped in; past [orchestrator] max_dispatches it must flag instead.
	Dispatches int `toml:"dispatches,omitempty"`
	// Artifacts are outputs ws can't infer (capsules' PRs it finds itself):
	// a PR from a branch outside the mission, a design doc, a dashboard.
	Artifacts []Artifact `toml:"artifacts,omitempty"`
}

// Proposal is something raised for the user: why, who by, when.
type Proposal struct {
	Text string    `toml:"text"`
	By   string    `toml:"by,omitempty"`
	At   time.Time `toml:"at"`
}

// Dispatch is new crew to launch on a mission, with the handoff it starts
// from.
type Dispatch struct {
	Text    string    `toml:"text"`
	Capsule string    `toml:"capsule,omitempty"` // repo/name; "" picks one
	Harness string    `toml:"harness,omitempty"`
	By      string    `toml:"by,omitempty"`
	At      time.Time `toml:"at"`
}

// Artifact is a link a mission produced.
type Artifact struct {
	URL   string    `toml:"url"`
	Label string    `toml:"label,omitempty"`
	Crew  string    `toml:"crew,omitempty"`
	Added time.Time `toml:"added"`
}

// HasCapsule reports whether the mission owns repo/name.
func (m *Mission) HasCapsule(repo, name string) bool {
	return slices.ContainsFunc(m.Capsules, func(c Capsule) bool {
		return c.Repo == repo && c.Name == name
	})
}

// AddCapsule adds repo/name if it isn't already owned. It reports whether
// anything changed.
func (m *Mission) AddCapsule(repo, name string) bool {
	return m.AddCapsuleBranch(repo, name, "")
}

// AddCapsuleBranch is AddCapsule that also records the capsule's branch. For
// an already-owned capsule it fills in a missing branch.
func (m *Mission) AddCapsuleBranch(repo, name, branch string) bool {
	for i, c := range m.Capsules {
		if c.Repo == repo && c.Name == name {
			if c.Branch == "" && branch != "" {
				m.Capsules[i].Branch = branch
				return true
			}
			return false
		}
	}
	m.Capsules = append(m.Capsules, Capsule{Repo: repo, Name: name, Branch: branch})
	return true
}

// RemoveCapsule drops repo/name. It reports whether anything changed.
func (m *Mission) RemoveCapsule(repo, name string) bool {
	before := len(m.Capsules)
	m.Capsules = slices.DeleteFunc(m.Capsules, func(c Capsule) bool {
		return c.Repo == repo && c.Name == name
	})
	return len(m.Capsules) != before
}

// PRState is the GitHub PR state of a capsule's branch, as far as callers know.
type PRState int

const (
	PRNone PRState = iota
	PROpen
	PRMerged
)

// CapsuleFacts is what DeriveState needs to know about one capsule.
type CapsuleFacts struct {
	Exists bool
	PR     PRState
}

// DeriveState works out a mission's state. A stored Parked or Archived wins.
// Otherwise only capsules that still exist or have a PR count:
//
//	none count             → draft
//	every one merged       → landed
//	every one has a PR     → review
//	anything else          → active
func DeriveState(m *Mission, facts func(Capsule) CapsuleFacts) State {
	if m.State == Parked || m.State == Archived || m.State == Briefing {
		return m.State
	}
	counted, withPR, merged := 0, 0, 0
	for _, c := range m.Capsules {
		f := facts(c)
		if !f.Exists && f.PR == PRNone {
			continue
		}
		counted++
		if f.PR != PRNone {
			withPR++
		}
		if f.PR == PRMerged {
			merged++
		}
	}
	switch {
	case counted == 0:
		return Draft
	case merged == counted:
		return Landed
	case withPR == counted:
		return Review
	default:
		return Active
	}
}
