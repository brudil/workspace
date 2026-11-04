// Package harness knows how to launch and resume each kind of agent (crew)
// that the deck can run on a mission.
//
// A harness only builds commands. Running them in a terminal pane, setting
// WS_MISSION/WS_CREW, and recording crew are the deck's job.
package harness

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// EnvDefault picks the harness the deck launches by default.
const EnvDefault = "WS_DECK_HARNESS"

// Brief is what a harness needs to know about the mission it starts on.
type Brief struct {
	Slug  string
	Title string
	Brief string // brief.md contents
	Dir   string // working directory (a mission capsule)
}

// Launch is a ready-to-run agent command.
type Launch struct {
	Args []string // argv, Args[0] is the program
	Dir  string
	// Conversation is the agent's own session id when known before the
	// agent starts, so the crew record can be resumed later.
	Conversation string
}

// Harness launches one kind of agent.
type Harness interface {
	Name() string
	// Available reports why the harness can't run here, or nil.
	Available() error
	// Start builds the command for new crew on a mission.
	Start(b Brief) (Launch, error)
	// Resume builds the command to continue an earlier conversation. It
	// returns ErrNoResume if the harness can't.
	Resume(dir, conversation string) (Launch, error)
}

// ErrNoResume is returned by harnesses that can't resume a conversation.
var ErrNoResume = errors.New("this harness can't resume conversations")

var registry = map[string]Harness{}

func register(h Harness) { registry[h.Name()] = h }

// Get returns a harness by name.
func Get(name string) (Harness, error) {
	if h, ok := registry[name]; ok {
		return h, nil
	}
	return nil, fmt.Errorf("unknown harness %q (have %s)", name, strings.Join(Names(), ", "))
}

// Names lists the registered harnesses.
func Names() []string {
	var names []string
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Default is $WS_DECK_HARNESS, or cursor.
func Default() string {
	if n := os.Getenv(EnvDefault); n != "" {
		return n
	}
	return "cursor"
}

// InitialPrompt is the first message given to new crew. It points the agent at
// the mission rather than pasting everything, so the brief stays the single
// source of truth, but includes the brief too so the agent can start at once.
func InitialPrompt(b Brief) string {
	var s strings.Builder
	fmt.Fprintf(&s, "You are crew on the mission %q (%s), managed by the ws workspace tool.\n\n", b.Title, b.Slug)
	s.WriteString("Before starting, run `ws mission show -f llm` to see the mission's capsules (worktrees, possibly across repos) and log. ")
	s.WriteString("Work only in this mission's capsules; create new ones with `ws lift <repo> <name>` and they join the mission automatically. ")
	s.WriteString("Record significant decisions and progress with `ws mission note \"...\"`. Don't edit the brief.\n")
	if brief := strings.TrimSpace(b.Brief); brief != "" {
		s.WriteString("\nThe brief:\n\n")
		s.WriteString(brief)
		s.WriteString("\n")
	}
	return s.String()
}
