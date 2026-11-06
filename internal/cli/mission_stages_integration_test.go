package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/testutil"
)

const twoStages = "## Goal\nCache it.\n\n## Stages\n1. Write the proposal in `backend/cache-lookup`. Check in with me.\n2. Implement it in `frontend/cache-ui`.\n"

func TestStages_ShowAcceptAndDispatch(t *testing.T) {
	w, store, out := dispatchFixture(t)
	briefFile := filepath.Join(t.TempDir(), "brief.md")
	os.WriteFile(briefFile, []byte(twoStages), 0o644)

	// Written by you: a stage naming a capsule the mission lacks is warned of.
	t.Setenv("WS_ROLE", "")
	r := run(t, w, nil, "mission", "brief", "--set", briefFile)
	if !strings.Contains(r.Stderr, "stage 2 names `frontend/cache-ui`, which isn't one of the mission's capsules (yet): ws lift frontend cache-ui") {
		t.Errorf("no warning:\n%s", r.Stderr)
	}
	show := run(t, w, nil, "mission", "show", "-f", "llm").Stdout
	for _, want := range []string{
		"1. [current] Write the proposal in `backend/cache-lookup`. Check in with me. (check in with the user after it)",
		"2. [to do] Implement it in `frontend/cache-ui`.",
	} {
		if !strings.Contains(show, want) {
			t.Errorf("show missing %q:\n%s", want, show)
		}
	}

	// Crew don't accept their own work.
	t.Setenv(mission.EnvCrew, "c1")
	if r := testutil.RunCommand(t, w.Root, nil, "mission", "accept", "done it"); r.Err == nil || !strings.Contains(r.Err.Error(), "the orchestrator or the user accepts") {
		t.Errorf("crew accepted: %v", r.Err)
	}
	t.Setenv(mission.EnvCrew, "")
	t.Setenv("WS_ROLE", "orchestrator")
	r = run(t, w, nil, "mission", "accept", "the proposal is written")
	if !strings.Contains(r.Stderr, "accepted stage 1 of 2: Write the proposal") || !strings.Contains(r.Stderr, "next: stage 2: Implement it") {
		t.Errorf("accept said:\n%s", r.Stderr)
	}

	// Dispatched crew join the current stage, and are told it.
	if r := testutil.RunCommand(t, w.Root, nil, "mission", "dispatch", "--stage", "3", "x"); r.Err == nil || !strings.Contains(r.Err.Error(), "there's no stage 3") {
		t.Errorf("dispatched for a stage that isn't there: %v", r.Err)
	}
	run(t, w, nil, "mission", "dispatch", "Implement the proposal")
	if got := waitFile(t, out); !strings.Contains(got, "stage 2 of 2: Implement it in `frontend/cache-ui`.") {
		t.Errorf("crew got %q", got)
	}
	m, _ := store.Load("cache")
	states, _ := m.StageProgress(twoStages)
	if st, ok := mission.StageFor(states, "c2"); !ok || st.N != 2 {
		t.Errorf("c2 is on %+v", st)
	}

	// The orchestrator sees the progress, and the stage crew were on.
	prompt := run(t, w, nil, "mission", "orchestrate", "-m", "cache", "--crew", "c2", "--trigger", "done", "--print-prompt").Stdout
	for _, want := range []string{"It was working on stage 2 of 2: Implement it", "## Stage progress", "1. [accepted] Write the proposal", "accepted by orchestrator: the proposal is written", "2. [current] Implement it", "(crew c2)"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("orchestrator prompt missing %q", want)
		}
	}
}
