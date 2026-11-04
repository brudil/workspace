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
)

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
}

// Mission is the contents of mission.toml plus its slug.
type Mission struct {
	Slug    string    `toml:"-"`
	Title   string    `toml:"title"`
	Created time.Time `toml:"created"`
	State   State     `toml:"state,omitempty"` // stored override: "", briefing, parked or archived
	// Request is what the user asked for, verbatim, when a briefing crew
	// created the mission from it.
	Request  string    `toml:"request,omitempty"`
	Capsules []Capsule `toml:"capsules,omitempty"`
	Crew     []Crew    `toml:"crew,omitempty"`
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
