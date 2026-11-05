// Package crew runs agents (crew) on missions: where they work, how they
// start, and how their ends are recorded. The deck and `ws mission dispatch`
// both launch crew through it, on a Host: the workspace's deck server, or
// (for tests and WS_DECK_SERVER=0) this process.
package crew

import (
	"io"
	"time"

	"github.com/brudil/workspace/internal/execlog"
	"github.com/brudil/workspace/internal/mission"
	uv "github.com/charmbracelet/ultraviolet"
)

// Term is a crew's terminal. *termpane.Pane is the in-process
// implementation; the deck server's client is the other.
type Term interface {
	RenderScrolled(offset int) string
	ScrollbackLen() int
	AltScreen() bool
	Text() string
	Cursor() (x, y int, visible bool)
	Title() string
	// LastChange is when the screen's text last changed.
	LastChange() time.Time

	// TakeCopied returns what the crew copied to the clipboard since the
	// last call, if it copied anything.
	TakeCopied() (string, bool)

	SendKey(uv.KeyPressEvent)
	SendMouse(uv.MouseEvent)
	Paste(string)
	WriteRaw([]byte)
	SetMirror(io.Writer)
	Resize(width, height int)

	Updates() <-chan struct{}
	// Done is closed once the crew has exited and its end is recorded.
	Done() <-chan struct{}
	Exited() bool
	ExitSummary() string
	Pid() int
	// Close stops the crew: SIGHUP, then SIGKILL after grace.
	Close(grace time.Duration)
}

// Spec is everything needed to start a crew process.
type Spec struct {
	Slug    string   `json:"slug"`
	CrewID  string   `json:"crew"`
	Harness string   `json:"harness"`
	Capsule string   `json:"capsule"` // repo/name it works in, for display
	Role    string   `json:"role,omitempty"`
	Args    []string `json:"args"`
	Dir     string   `json:"dir"`
	Env     []string `json:"env"`
	Width   int      `json:"w"`
	Height  int      `json:"h"`
}

// Key is the crew's identity: slug/crew.
func (s Spec) Key() string { return s.Slug + "/" + s.CrewID }

// Hosted is crew a host is running.
type Hosted struct {
	Spec Spec
	Term Term
}

// Host runs crew. Hosts record crew ends themselves (through Finish), so
// whoever launched crew never has to.
type Host interface {
	Launch(Spec) (Term, error)
	// Running attaches to crew the host is running that skip doesn't name
	// (by Key): crew running when a deck opened, and crew launched since
	// from elsewhere (the orchestrator's dispatches).
	Running(skip func(key string) bool) []Hosted
	// Detachable reports whether crew outlive whoever launched them (the
	// deck server). Otherwise Leave stops them.
	Detachable() bool
	// Leave lets go of the host: a detachable host disconnects and its crew
	// keep running; any other stops its crew and waits for their ends to be
	// recorded.
	Leave()
}

// EndHook runs after a crew's end is recorded. stopped is true when it was
// stopped (by the user, or the host shutting down) rather than ending by
// itself.
type EndHook func(spec Spec, exit string, stopped bool)

// Finish records the end of a crew run: its output, then the end, then the
// hook. Every host ends crew through here.
func Finish(store mission.Store, spec Spec, exit, transcript string, stopped bool, hook EndHook) error {
	result := exit
	if stopped {
		result += " (stopped)"
	}
	execlog.End(store.Root, "crew "+spec.Key(), 0, 0, result)
	errT := store.SaveTranscript(spec.Slug, spec.CrewID, exit, transcript)
	errE := store.EndCrew(spec.Slug, spec.CrewID, exit)
	if hook != nil {
		hook(spec, exit, stopped)
	}
	if errE != nil {
		return errE
	}
	return errT
}
