package harness

import (
	"errors"
	"fmt"
	"os"
)

// DefaultHarness is what crew run when nothing configures another.
const DefaultHarness = "cursor"

// Pick is a harness, and the model to run on it ("" for the agent's own
// default), chosen for one role.
type Pick struct {
	Harness string
	Model   string
}

// ModelFor is the model to run on the named harness: the pick's model if
// that's the pick's harness, else the agent's own default. A model belongs
// to the harness it was configured for: Cursor and Claude Code name models
// differently, so picking another harness in the deck drops it.
func (p Pick) ModelFor(harnessName string) string {
	if harnessName == p.Harness {
		return p.Model
	}
	return ""
}

// Roles is what's configured for each role agents play on a mission, from
// [crew], [briefing] and [orchestrator] in ws.toml / ws.local.toml. Empty
// fields fall back (see the For methods), so the zero value works.
type Roles struct {
	Crew, Briefing, Orchestrator Pick
}

// ForCrew picks the harness for crew doing the work: $WS_DECK_HARNESS, else
// [crew] harness, else cursor. [crew] model goes with [crew] harness.
func (r Roles) ForCrew() Pick {
	p := r.Crew
	if env := os.Getenv(EnvDefault); env != "" && env != p.Harness {
		p = Pick{Harness: env}
	}
	if p.Harness == "" {
		p.Harness = DefaultHarness
	}
	return p
}

// ForBriefing picks the harness for briefing agents: [briefing], else the
// crew's harness. A [briefing] model with no harness applies to the crew's.
func (r Roles) ForBriefing() Pick {
	if r.Briefing.Harness != "" {
		return r.Briefing
	}
	p := r.ForCrew()
	if r.Briefing.Model != "" {
		p.Model = r.Briefing.Model
	}
	return p
}

// ForOrchestrator picks the harness for the headless orchestrator, which
// must be able to run one: [orchestrator] harness, else the crew's harness
// if it can, else the first of cursor, claude and pi installed here. The model
// is [orchestrator] model, else the harness's own orchestrator default.
func (r Roles) ForOrchestrator() (Pick, Orchestrator, error) {
	if name := r.Orchestrator.Harness; name != "" {
		o, err := orchestratorNamed(name)
		if err != nil {
			return Pick{}, nil, err
		}
		return r.orchestratorPick(o), o, nil
	}
	if o, err := orchestratorNamed(r.ForCrew().Harness); err == nil {
		return r.orchestratorPick(o), o, nil
	}
	for _, name := range []string{"cursor", "claude", "pi"} {
		if o, err := orchestratorNamed(name); err == nil && o.Available() == nil {
			return r.orchestratorPick(o), o, nil
		}
	}
	return Pick{}, nil, errors.New("no harness here can run the headless orchestrator: install the Cursor CLI, Claude Code or pi, or set [orchestrator] harness")
}

func (r Roles) orchestratorPick(o Orchestrator) Pick {
	p := Pick{Harness: o.Name(), Model: r.Orchestrator.Model}
	if p.Model == "" {
		p.Model = o.OrchestratorModel()
	}
	return p
}

func orchestratorNamed(name string) (Orchestrator, error) {
	h, err := Get(name)
	if err != nil {
		return nil, err
	}
	o, ok := h.(Orchestrator)
	if !ok {
		return nil, fmt.Errorf("harness %q can't run the headless orchestrator", name)
	}
	return o, nil
}
