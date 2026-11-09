package web

import "time"

// What the API serves. web/src/api.ts mirrors these.

// Workspace is GET /api/workspace: the workspace and its missions.
type Workspace struct {
	Name     string    `json:"name"`
	Root     string    `json:"root"`
	Missions []Mission `json:"missions"`
}

// Mission is one mission, as the deck's sidebar knows it.
type Mission struct {
	Slug     string    `json:"slug"`
	Title    string    `json:"title"`
	State    string    `json:"state"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`        // when its record, brief or log last changed
	Flag     string    `json:"flag,omitempty"` // the question it's flagged with
	Capsules []Capsule `json:"capsules"`
	Crew     []Crew    `json:"crew"`
}

// Capsule is one of a mission's capsules.
type Capsule struct {
	Repo   string `json:"repo"`
	Name   string `json:"name"`
	Branch string `json:"branch,omitempty"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	// Head is the capsule's commit: when it moves, a review open on it has
	// new commits to show.
	Head string `json:"head,omitempty"`
}

// Crew is one agent session run on a mission.
type Crew struct {
	ID        string    `json:"id"`
	Harness   string    `json:"harness"`
	Role      string    `json:"role,omitempty"`
	Capsule   string    `json:"capsule,omitempty"`
	Started   time.Time `json:"started"`
	Ended     bool      `json:"ended"`
	Activity  string    `json:"activity,omitempty"` // working, waiting, permission
	Status    string    `json:"status,omitempty"`   // done, …
	LastReply string    `json:"lastReply,omitempty"`
}

// MissionDetail is GET /api/missions/{slug}: everything the deck's mission
// view shows. Capsules' PRs are only there with ?github=1 (GitHub is slow,
// so the page asks after it has drawn).
type MissionDetail struct {
	Slug      string          `json:"slug"`
	Title     string          `json:"title"`
	State     string          `json:"state"`
	Created   time.Time       `json:"created"`
	Request   string          `json:"request,omitempty"` // what was asked for, when a briefing agent made it
	From      string          `json:"from,omitempty"`    // the mission/crew that raised it
	Brief     string          `json:"brief"`
	Flag      *Proposal       `json:"flag,omitempty"`     // a question for you
	Dispatch  *Proposal       `json:"dispatch,omitempty"` // crew waiting for you to launch
	Conclude  *Proposal       `json:"conclude,omitempty"` // a suggestion it's finished
	Stages    []Stage         `json:"stages"`
	Capsules  []CapsuleDetail `json:"capsules"`
	Crew      []CrewDetail    `json:"crew"`
	Artifacts []Artifact      `json:"artifacts"`
	Events    []Event         `json:"events"` // the log, oldest first
	GitHub    bool            `json:"github"` // PRs were looked up
}

// Proposal is something raised for you, by whom.
type Proposal struct {
	Text    string   `json:"text"`
	By      string   `json:"by,omitempty"`
	Capsule string   `json:"capsule,omitempty"`
	Options []string `json:"options,omitempty"`
}

// Stage is one of the brief's stages and where it's got to.
type Stage struct {
	N       int      `json:"n"`
	Title   string   `json:"title"` // a few words
	Text    string   `json:"text"`  // the whole stage, on one line
	State   string   `json:"state"` // accepted, current, todo
	Crew    []string `json:"crew,omitempty"`
	CheckIn bool     `json:"checkIn,omitempty"`
	Why     string   `json:"why,omitempty"` // why it was accepted
}

// CapsuleDetail is a capsule with its git status and PR.
type CapsuleDetail struct {
	Capsule
	Dirty  bool `json:"dirty,omitempty"`
	Ahead  int  `json:"ahead,omitempty"`
	Behind int  `json:"behind,omitempty"`
	PR     *PR  `json:"pr,omitempty"`
}

// PR is a capsule's pull request.
type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Merged bool   `json:"merged,omitempty"`
	Checks string `json:"checks,omitempty"`
}

// CrewDetail is crew with how it went.
type CrewDetail struct {
	Crew
	EndedAt  *time.Time `json:"endedAt,omitempty"`
	Exit     string     `json:"exit,omitempty"`
	Note     string     `json:"note,omitempty"`     // with Status: what it said
	Dispatch string     `json:"dispatch,omitempty"` // what it was dispatched for
	Handoff  string     `json:"handoff,omitempty"`  // its handoff's artifact reference
}

// Artifact is something the mission made or relies on, and where it is now.
type Artifact struct {
	Ref   string    `json:"ref"`
	Kind  string    `json:"kind"`
	Label string    `json:"label,omitempty"`
	State string    `json:"state"` // draft, in review, landed, private, missing; open, merged; link
	URL   string    `json:"url,omitempty"`
	Path  string    `json:"path,omitempty"`
	By    string    `json:"by,omitempty"` // the crew that recorded it; "" for you
	Added time.Time `json:"added"`
	// Readable: it's a file here, so GET .../artifact shows it.
	Readable bool `json:"readable"`
}

// Event is a line of the mission's log, as the deck words it.
type Event struct {
	Time time.Time `json:"time"`
	Kind string    `json:"kind"`
	Text string    `json:"text"`
}

// ArtifactContent is GET /api/missions/{slug}/artifact?ref=: a readable
// artifact's text.
type ArtifactContent struct {
	Artifact
	Text     string `json:"text"`
	Markdown bool   `json:"markdown"`
}
