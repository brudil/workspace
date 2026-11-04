package crew

import (
	"strings"
	"time"

	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	uv "github.com/charmbracelet/ultraviolet"
)

// Request is crew to start on a mission.
type Request struct {
	Store mission.Store
	Host  Host
	Root  string // workspace root, for the crew's environment

	Slug    string
	Harness string // harness name, as recorded
	Place   Place
	Role    string // "" or mission.RoleBriefing

	// Resume, when set, continues that crew (same id) instead of recording
	// new crew; a resume-with message it was left becomes the prompt.
	Resume string
	// Handoff, when set, is what the crew was dispatched to do, and By who
	// dispatched it.
	Handoff, By string

	Width, Height int

	// Prepare asks the harness for the command (Start or Resume).
	Prepare func() (harness.Launch, error)
}

// Started is crew that started.
type Started struct {
	Crew mission.Crew
	Term Term
	Dir  string
	// Prompt is the opening prompt still to be typed in (TypePrompt).
	Prompt string
}

// Start launches crew: asks the harness for the command, records the crew
// on the mission, starts it on the host and records its process. If it
// fails after recording the crew, the crew's end is recorded too.
func Start(r Request) (Started, error) {
	l, err := r.Prepare()
	if err != nil {
		return Started{}, err
	}
	var c mission.Crew
	prompt := l.Prompt
	if r.Resume != "" {
		c, err = r.Store.ResumeCrew(r.Slug, r.Resume)
		if err == nil && c.ResumeWith != "" {
			prompt = c.ResumeWith
			r.Store.SetResumeWith(r.Slug, c.ID, "")
		}
	} else {
		c, err = r.Store.StartCrewAs(r.Slug, r.Harness, l.Conversation, r.Role)
	}
	if err != nil {
		return Started{}, err
	}
	if r.Handoff != "" {
		r.Store.SetHandoff(r.Slug, c.ID, r.By, r.Handoff)
	}
	term, err := r.Host.Launch(Spec{
		Slug: r.Slug, CrewID: c.ID, Harness: r.Harness, Capsule: r.Place.Capsule, Role: r.Role,
		Args: l.Args, Dir: l.Dir, Env: Env(r.Root, r.Slug, c.ID),
		Width: r.Width, Height: r.Height,
	})
	if err != nil {
		r.Store.EndCrew(r.Slug, c.ID, "failed to start: "+err.Error())
		return Started{}, err
	}
	r.Store.SetCrewProcess(r.Slug, c.ID, term.Pid(), r.Place.Capsule)
	return Started{Crew: c, Term: term, Dir: l.Dir, Prompt: prompt}, nil
}

// Typing the opening prompt in: wait for the agent's screen to draw and stop
// changing, then paste and submit, as a person would.
var (
	PromptSettle  = 1200 * time.Millisecond
	PromptMaxWait = 20 * time.Second
)

// TypePrompt waits for an agent's screen to draw and settle, then pastes the
// prompt and submits it. It reports false if the agent exited first.
func TypePrompt(t Term, prompt string) bool {
	deadline := time.Now().Add(PromptMaxWait)
	last, changed, drawn := "", time.Now(), false
	for time.Now().Before(deadline) {
		if t.Exited() {
			return false
		}
		if text := t.Text(); text != last {
			last, changed = text, time.Now()
			drawn = drawn || strings.TrimSpace(text) != ""
		}
		if drawn && time.Since(changed) >= PromptSettle {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if t.Exited() {
		return false
	}
	t.Paste(prompt)
	time.Sleep(300 * time.Millisecond)
	t.SendKey(uv.KeyPressEvent{Code: uv.KeyEnter})
	return true
}
