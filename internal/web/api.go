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
