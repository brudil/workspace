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
	"strings"
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

// MaxTitle is the longest title a mission can be given: titles name
// missions in the deck, so they're terse, plain-English names.
const MaxTitle = 50

// TitleIsRequest reports whether the title is still the request the
// mission was raised with (or the start of it), not a name for it yet.
func (m *Mission) TitleIsRequest() bool {
	if m.Request == "" {
		return false
	}
	first, _, _ := strings.Cut(strings.TrimSpace(m.Request), "\n")
	return strings.HasPrefix(first, strings.TrimSuffix(m.Title, "…"))
}

// RoleBriefing marks crew that brief a mission rather than work on it.
const RoleBriefing = "briefing"

// RoleAnswer marks crew the user asked to answer, for them, the question
// the orchestrator flagged: they find out what's needed and answer with ws
// mission answer, and don't do the work it leads to.
const RoleAnswer = "answer"

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
	// Role is "" for crew working on the mission, RoleBriefing, or
	// RoleAnswer.
	Role string `toml:"role,omitempty"`

	// Status is what the crew last said about itself: CrewAsking (needs the
	// user), CrewDone (finished its part), or "". Note is the question or
	// the summary. Both clear when the user next talks to the crew.
	Status   string     `toml:"status,omitempty"`
	Note     string     `toml:"note,omitempty"`
	StatusAt *time.Time `toml:"status_at,omitempty"`
	// Activity is what the agent last reported through its own hooks, which
	// ws installs when it launches crew: ActivityWorking once it's sent a
	// message, ActivityWaiting once it has finished its turn,
	// ActivityPermission when it has stopped to ask for one. "" for a
	// harness without hooks: the deck then judges from the screen.
	Activity   string     `toml:"activity,omitempty"`
	ActivityAt *time.Time `toml:"activity_at,omitempty"`
	// LastReply is the start of what the agent last said: its turn's final
	// message. Notice is its last notification ("Claude needs your
	// permission to use Bash").
	LastReply string `toml:"last_reply,omitempty"`
	Notice    string `toml:"notice,omitempty"`
	// ResumeWith is a message to send when this crew is next resumed
	// (ws mission resume-with); it's used once.
	ResumeWith string `toml:"resume_with,omitempty"`
	// Dispatch is what this crew was dispatched to do, when the orchestrator
	// (or the user) dispatched it with ws mission dispatch.
	Dispatch string `toml:"dispatch,omitempty"`
	// OldDispatch is Dispatch as records stored it before handoffs (what
	// crew leave behind) took the name; Load moves it across.
	OldDispatch string `toml:"handoff,omitempty"`
	// HandedOff is when the crew last left a handoff with ws mission done.
	// It's kept at HandoffRef(ID).
	HandedOff *time.Time `toml:"handed_off,omitempty"`
	// Orchestrated identifies the crew event (a run ending, a done or ask) the
	// orchestrator last looked at, so each is handled once. Times alone can't tell events apart:
	// they're stored to the second.
	Orchestrated string `toml:"orchestrated,omitempty"`
}

// Crew statuses.
const (
	CrewAsking = "asking"
	CrewDone   = "done"

	// What crew report through their hooks (Crew.Activity).
	ActivityWorking    = "working"
	ActivityWaiting    = "waiting"
	ActivityPermission = "permission"
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
	// Flag marks the mission as needing the user, with why (ws mission flag):
	// a question for them, with the answers it suggests.
	Flag *Proposal `toml:"flag,omitempty"`
	// Answer is the user's answer to the last flag (ws mission answer, or
	// from the deck), which the orchestrator's next run acts on.
	Answer *Answer `toml:"answer,omitempty"`
	// Conclude is a suggestion that the mission is finished (ws mission
	// conclude); archiving it is the user's call.
	Conclude *Proposal `toml:"conclude,omitempty"`
	// Dispatch is crew the orchestrator wants launched, waiting for the user
	// ([orchestrator] dispatch = "ask", or no deck server to launch on).
	Dispatch *Dispatch `toml:"dispatch,omitempty"`
	// Dispatches counts crew the orchestrator dispatched since the user last
	// stepped in; past [orchestrator] max_dispatches it must flag instead.
	Dispatches int `toml:"dispatches,omitempty"`
	// Artifacts point at what the mission produced or relies on: files (on
	// a default branch, in a capsule, or private to the mission), PRs and
	// issues, and links. Capsules' own PRs are found, not recorded.
	Artifacts []Artifact `toml:"artifacts,omitempty"`
	// Stages is the progress of the brief's stages (stages.go): the plan
	// itself is the brief's.
	Stages []StageRecord `toml:"stages,omitempty"`
}

// Proposal is something raised for the user: why, who by, when.
type Proposal struct {
	Text string    `toml:"text"`
	By   string    `toml:"by,omitempty"`
	At   time.Time `toml:"at"`
	// Options are the answers a flag suggests, so the user can answer
	// with one key (they can always write their own).
	Options []string `toml:"options,omitempty"`
}

// Answer is the user's answer to a flag: what was asked, and what they
// said.
type Answer struct {
	Question string    `toml:"question"`
	Text     string    `toml:"text"`
	At       time.Time `toml:"at"`
	// By is "you", or the crew (RoleAnswer) that answered for you.
	By string `toml:"by,omitempty"`
}

// Dispatch is new crew to launch on a mission, with what it's to do
// from.
type Dispatch struct {
	Text    string    `toml:"text"`
	Capsule string    `toml:"capsule,omitempty"` // repo/name; "" picks one
	Harness string    `toml:"harness,omitempty"`
	By      string    `toml:"by,omitempty"`
	At      time.Time `toml:"at"`
	// Stage is the stage of the brief's plan it's for (from 1); 0 for
	// whichever is current when it's launched.
	Stage int `toml:"stage,omitempty"`
}

// Artifact points at something a mission produced or relies on. Ref says
// where, in one of these forms (the cli package parses and resolves them):
//
//	repo:path            a file on the repo's default branch
//	repo/capsule:path    a file in one of the mission's capsules
//	mission:path         a file in the mission's own files/ folder
//	repo#N               a PR or issue
//	https://…            a link
//
// Where it is now (draft, in review, landed…) isn't stored: it's worked
// out from git and GitHub each time.
type Artifact struct {
	Ref   string `toml:"ref,omitempty"`
	Kind  string `toml:"kind,omitempty"` // proposal, adr, note, doc, pr, link…
	Label string `toml:"label,omitempty"`
	Crew  string `toml:"crew,omitempty"` // who recorded it; "" for the user
	// URL is how artifacts were recorded before Ref: always a link. Loading
	// moves it to Ref.
	URL   string    `toml:"url,omitempty"`
	Added time.Time `toml:"added"`
}

// MissionFilesDir is the folder, inside a mission's own, for its private
// files (mission:path artifacts).
const MissionFilesDir = "files"

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
