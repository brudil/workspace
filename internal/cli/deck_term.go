package cli

import (
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/brudil/workspace/internal/termpane"
	uv "github.com/charmbracelet/ultraviolet"
)

// crewTerm is a crew's terminal as the deck uses it. *termpane.Pane is the
// in-process implementation; the deck server's client is the other.
type crewTerm interface {
	RenderScrolled(offset int) string
	ScrollbackLen() int
	AltScreen() bool
	Text() string
	Cursor() (x, y int, visible bool)
	Title() string

	SendKey(uv.KeyPressEvent)
	SendMouse(uv.MouseEvent)
	Paste(string)
	WriteRaw([]byte)
	SetMirror(io.Writer)
	Resize(width, height int)

	Updates() <-chan struct{}
	Done() <-chan struct{}
	Exited() bool
	ExitSummary() string
	Pid() int
	Close(grace time.Duration)
}

var _ crewTerm = (*termpane.Pane)(nil)

// crewSpec is everything needed to start a crew process.
type crewSpec struct {
	Slug, CrewID, Harness string
	Capsule               string // repo/name, for display
	Args                  []string
	Dir                   string
	Env                   []string
	Width, Height         int
}

// hostedCrew is crew already running in a host when the deck attaches.
type hostedCrew struct {
	Spec crewSpec
	Term crewTerm
}

// crewHost runs crew processes for the deck.
type crewHost interface {
	Launch(crewSpec) (crewTerm, error)
	// Running lists crew the host was already running when the deck opened.
	Running() []hostedCrew
	// OwnsLifecycle is true when crew live and die with this deck, so the
	// deck must record their ends and stop them when it quits. A server host
	// keeps crew running after the deck detaches and records ends itself.
	OwnsLifecycle() bool
	// Detach lets go of the host without stopping its crew.
	Detach()
}

// localHost runs crew as children of the deck.
type localHost struct{}

func (localHost) Launch(s crewSpec) (crewTerm, error) {
	cmd := exec.Command(s.Args[0], s.Args[1:]...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	return termpane.Start(cmd, s.Width, s.Height)
}

func (localHost) Running() []hostedCrew { return nil }
func (localHost) OwnsLifecycle() bool   { return true }
func (localHost) Detach()               {}

// crewRegistry holds every crew run the deck knows about. It is shared by
// all copies of the (value-typed) model, so code running after the program
// ends still sees every run.
type crewRegistry struct {
	mu   sync.Mutex
	runs []*deckCrewRun
}

func (g *crewRegistry) add(r *deckCrewRun) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.runs = append(g.runs, r)
}

func (g *crewRegistry) all() []*deckCrewRun {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]*deckCrewRun(nil), g.runs...)
}

// live returns the live runs for a mission ("" for every mission), in
// launch order.
func (g *crewRegistry) live(slug string) []*deckCrewRun {
	var out []*deckCrewRun
	for _, r := range g.all() {
		if r.live() && (slug == "" || r.slug == slug) {
			out = append(out, r)
		}
	}
	return out
}
