package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brudil/workspace/internal/cli"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/testutil"
)

// handoffHarness starts crew that write the first line they're sent to a
// file, so a test can see the opening prompt (with the handoff) arrive.
type handoffHarness struct{ out string }

func (handoffHarness) Name() string     { return "handoff" }
func (handoffHarness) Available() error { return nil }
func (h handoffHarness) Start(b harness.Brief) (harness.Launch, error) {
	return harness.Launch{
		Args:   []string{"/bin/sh", "-c", `printf 'ready> '; read line; printf '%s' "$line" > "$OUT"; sleep 5`},
		Dir:    b.Dir,
		Prompt: "handoff: " + b.Handoff,
	}, nil
}
func (handoffHarness) Resume(string, string) (harness.Launch, error) {
	return harness.Launch{}, harness.ErrNoResume
}

func dispatchFixture(t *testing.T) (*testutil.Workspace, mission.Store, string) {
	t.Helper()
	w, store := orchestrateFixture(t)
	out := filepath.Join(t.TempDir(), "got")
	t.Setenv("OUT", out)
	harness.Register(handoffHarness{})
	t.Setenv(harness.EnvDefault, "handoff")
	t.Cleanup(cli.UseLocalDispatchHost())
	store.EndCrew("cache", "c1", "exit 0")
	t.Setenv(mission.EnvMission, "cache")
	t.Setenv("WS_ROLE", "orchestrator")
	return w, store, out
}

func waitFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return string(data)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s never written", path)
	return ""
}

func TestDispatch_LaunchesFreshCrewWithHandoff(t *testing.T) {
	w, store, out := dispatchFixture(t)
	r := run(t, w, nil, "mission", "dispatch", "Implement docs/proposals/cache.md (commit abc123)")
	if !strings.Contains(r.Stderr, "dispatched c2 on cache in backend/cache-lookup") {
		t.Errorf("stderr: %s", r.Stderr)
	}
	if got := waitFile(t, out); got != "handoff: Implement docs/proposals/cache.md (commit abc123)" {
		t.Errorf("crew got %q", got)
	}
	m, _ := store.Load("cache")
	c := m.Crew[1]
	if c.ID != "c2" || c.Handoff != "Implement docs/proposals/cache.md (commit abc123)" || c.Capsule != "backend/cache-lookup" || c.PID == 0 {
		t.Errorf("crew = %+v", c)
	}
	if m.Dispatches != 1 {
		t.Errorf("dispatches = %d", m.Dispatches)
	}
	show := run(t, w, nil, "mission", "show", "-f", "llm")
	if !strings.Contains(show.Stdout, "Crew c2 was dispatched with: Implement docs/proposals") {
		t.Errorf("show:\n%s", show.Stdout)
	}
}

func TestDispatch_LimitThenFlag(t *testing.T) {
	w, store, _ := dispatchFixture(t)
	os.WriteFile(filepath.Join(w.Root, "ws.local.toml"), []byte("[orchestrator]\nmax_dispatches = 1\n"), 0o644)
	run(t, w, nil, "mission", "dispatch", "first")
	r := testutil.RunCommand(t, w.Root, nil, "mission", "dispatch", "second")
	if r.Err == nil || !strings.Contains(r.Err.Error(), "ws mission flag") {
		t.Fatalf("second dispatch: %v", r.Err)
	}
	// You dispatching (or launching crew in the deck) starts the count again.
	t.Setenv("WS_ROLE", "")
	run(t, w, nil, "mission", "dispatch", "from you")
	if m, _ := store.Load("cache"); m.Dispatches != 0 || len(m.Crew) != 3 {
		t.Errorf("dispatches = %d, crew = %d", m.Dispatches, len(m.Crew))
	}
}

func TestDispatch_AskQueuesForTheDeck(t *testing.T) {
	w, store, _ := dispatchFixture(t)
	os.WriteFile(filepath.Join(w.Root, "ws.local.toml"), []byte("[orchestrator]\ndispatch = \"ask\"\n"), 0o644)
	r := run(t, w, nil, "mission", "dispatch", "--capsule", "backend/cache-lookup", "implement it")
	if !strings.Contains(r.Stderr, "queued") {
		t.Errorf("stderr: %s", r.Stderr)
	}
	m, _ := store.Load("cache")
	if len(m.Crew) != 1 || m.Dispatch == nil || m.Dispatch.Text != "implement it" || m.Dispatch.By != "orchestrator" || m.Dispatch.Capsule != "backend/cache-lookup" {
		t.Errorf("crew %d, dispatch %+v", len(m.Crew), m.Dispatch)
	}
}

func TestDispatch_MistakesAreCorrectable(t *testing.T) {
	w, _, _ := dispatchFixture(t)
	r := testutil.RunCommand(t, w.Root, nil, "mission", "dispatch", "--capsule", "frontend/nope", "x")
	if r.Err == nil || !strings.Contains(r.Err.Error(), "has no capsule frontend/nope (it has: backend/cache-lookup)") || !strings.Contains(r.Err.Error(), "ws lift") {
		t.Errorf("bad capsule: %v", r.Err)
	}
	t.Setenv("WS_ROLE", "")
	t.Setenv(mission.EnvCrew, "c1")
	r = testutil.RunCommand(t, w.Root, nil, "mission", "dispatch", "x")
	if r.Err == nil || !strings.Contains(r.Err.Error(), "crew don't dispatch") {
		t.Errorf("crew dispatching: %v", r.Err)
	}
}
