package cli

import (
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
)

// recordingHarness is a shell that remembers the brief each launch got, so
// tests can see which harness and model a role used.
type recordingHarness struct{ name string }

var (
	recordedMu     sync.Mutex
	recordedBriefs = map[string][]harness.Brief{}
)

func (h recordingHarness) Name() string     { return h.name }
func (h recordingHarness) Available() error { return nil }
func (h recordingHarness) Start(b harness.Brief) (harness.Launch, error) {
	recordedMu.Lock()
	recordedBriefs[h.name] = append(recordedBriefs[h.name], b)
	recordedMu.Unlock()
	return harness.Launch{Args: []string{"/bin/sh", "-i"}, Dir: b.Dir}, nil
}
func (h recordingHarness) Resume(string, string) (harness.Launch, error) {
	return harness.Launch{}, harness.ErrNoResume
}

func lastBrief(t *testing.T, name string) harness.Brief {
	t.Helper()
	recordedMu.Lock()
	defer recordedMu.Unlock()
	bs := recordedBriefs[name]
	if len(bs) == 0 {
		t.Fatalf("%s never launched", name)
	}
	return bs[len(bs)-1]
}

// [crew] and [briefing] each pick their harness and model: ⏎ launches the
// crew's, n the briefing agent's, and a harness picked by hand (L) runs on
// its own default model.
func TestDeckRoles_CrewAndBriefingUseTheirOwnHarness(t *testing.T) {
	m, slug := shellCrewDeck(t, "work", "spare") // a free capsule for the second crew
	t.Setenv(harness.EnvDefault, "")
	harness.Register(recordingHarness{"crew-agent"})
	harness.Register(recordingHarness{"brief-agent"})
	m.roles = harness.Roles{
		Crew:     harness.Pick{Harness: "crew-agent", Model: "big"},
		Briefing: harness.Pick{Harness: "brief-agent", Model: "small"},
	}

	m = launch(t, m)
	r, ok := liveRun(m, slug)
	if !ok || r.harness != "crew-agent" {
		t.Fatalf("crew run = %+v", r)
	}
	if b := lastBrief(t, "crew-agent"); b.Model != "big" || b.Role != "" {
		t.Errorf("crew brief = %+v", b)
	}

	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // back to the sidebar
	m = newBriefing(t, m, "fe issue 45")
	br, ok := liveRun(m, "fe-issue-45")
	if !ok || br.harness != "brief-agent" || br.role != mission.RoleBriefing {
		t.Fatalf("briefing run = %+v", br)
	}
	if b := lastBrief(t, "brief-agent"); b.Model != "small" || b.Role != mission.RoleBriefing {
		t.Errorf("briefing brief = %+v", b)
	}

	// Picked by hand, brief-agent works the mission on its own default:
	// "small" was configured for briefing, "big" for crew-agent.
	m, _ = prefixKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = selectMission(t, m, slug)
	m, _ = press(t, m, "L")
	names := harness.Names()
	idx := -1
	for i, n := range names {
		if n == "brief-agent" {
			idx = i
		}
	}
	m = choose(t, m, idx)
	if b := lastBrief(t, "brief-agent"); b.Role != "" || b.Model != "" {
		t.Errorf("hand-picked crew brief = %+v", b)
	}
}
