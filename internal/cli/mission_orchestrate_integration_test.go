package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/testutil"
)

// echoOrchestrator "orchestrates" by printing its role and mission, so tests can see
// it ran with the orchestrator's environment.
type echoOrchestrator struct{}

func (echoOrchestrator) Name() string     { return "echo-orchestrator" }
func (echoOrchestrator) Available() error { return nil }
func (echoOrchestrator) Start(harness.Brief) (harness.Launch, error) {
	return harness.Launch{}, harness.ErrNoResume
}
func (echoOrchestrator) Resume(string, string) (harness.Launch, error) {
	return harness.Launch{}, harness.ErrNoResume
}
func (echoOrchestrator) Orchestrate(prompt, dir, model string) (harness.Launch, error) {
	return harness.Launch{Args: []string{"/bin/sh", "-c", `echo "ran as $WS_ROLE on $WS_MISSION with ` + model + `"`, prompt}, Dir: dir}, nil
}

func orchestrateFixture(t *testing.T) (*testutil.Workspace, mission.Store) {
	t.Helper()
	w := missionWorkspace(t)
	harness.Register(echoOrchestrator{})
	t.Setenv(harness.EnvDefault, "echo-orchestrator")
	store := mission.Store{Root: w.Root}
	run(t, w, nil, "mission", "new", "Cache", "--brief", "## Goal\nCache the lookup.\n## Acceptance\n- tests pass")
	t.Setenv(mission.EnvMission, "cache")
	run(t, w, nil, "lift", "backend", "cache-lookup")
	t.Setenv(mission.EnvMission, "")
	store.StartCrew("cache", "cursor", "conv-1")
	store.SetCrewProcess("cache", "c1", 0, "backend/cache-lookup")
	store.SaveTranscript("cache", "c1", "hangup", "…\nAll tests pass.\nOpened PR.\n")
	return w, store
}

func TestOrchestrate_PromptHasTheContext(t *testing.T) {
	w, _ := orchestrateFixture(t)
	r := run(t, w, nil, "mission", "orchestrate", "-m", "cache", "--crew", "c1", "--trigger", "done", "--print-prompt")
	for _, want := range []string{
		"orchestrator for the mission \"Cache\" (cache)",
		"Crew c1 has just reported that its part is done",
		"Cache the lookup.", "- tests pass",
		"backend/cache-lookup at " + filepath.Join(w.Root, "repos", "backend", "cache-lookup"),
		"ws mission resume-with <crew>", "ws mission conclude", "ws mission new",
		"The end of c1's output", "All tests pass.",
	} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if !strings.Contains(r.Stderr, "would write that to") || !strings.Contains(r.Stderr, "orchestrator-prompt.md and run:") {
		t.Errorf("stderr: %s", r.Stderr)
	}
}

func TestOrchestrate_RunsAsOrchestratorAndLogs(t *testing.T) {
	w, store := orchestrateFixture(t)
	run(t, w, nil, "mission", "orchestrate", "-m", "cache", "--crew", "c1", "--trigger", "done", "--auto")
	events, _ := store.ReadLog("cache", 0)
	var reviews []string
	for _, e := range events {
		if e.Kind == mission.EventOrchestrate {
			reviews = append(reviews, e.Text)
		}
	}
	if len(reviews) != 2 || !strings.Contains(reviews[1], "ran as orchestrator on cache with claude-haiku-5-5-medium") {
		t.Fatalf("review events = %q", reviews)
	}
	// The same event isn't reviewed twice.
	run(t, w, nil, "mission", "orchestrate", "-m", "cache", "--crew", "c1", "--trigger", "done", "--auto")
	events2, _ := store.ReadLog("cache", 0)
	if len(events2) != len(events) {
		t.Errorf("auto review ran twice for one event")
	}
	// A new event (crew reports done) gets a new review.
	store.SetCrewStatus("cache", "c1", mission.CrewDone, "again")
	run(t, w, nil, "mission", "orchestrate", "-m", "cache", "--crew", "c1", "--trigger", "done", "--auto")
	events3, _ := store.ReadLog("cache", 0)
	if len(events3) <= len(events2)+1 {
		t.Errorf("new event wasn't reviewed")
	}
}

func TestOrchestrate_ConfigTurnsAutoOff(t *testing.T) {
	w, store := orchestrateFixture(t)
	os.WriteFile(filepath.Join(w.Root, "ws.local.toml"), []byte("[orchestrator]\nenabled = false\nmodel = \"other\"\n"), 0o644)
	run(t, w, nil, "mission", "orchestrate", "-m", "cache", "--crew", "c1", "--auto")
	for _, e := range mustLog(t, store) {
		if e.Kind == mission.EventOrchestrate {
			t.Fatal("auto review ran with [orchestrator] enabled = false")
		}
	}
	// A manual review still runs, with the configured model.
	run(t, w, nil, "mission", "orchestrate", "-m", "cache")
	found := false
	for _, e := range mustLog(t, store) {
		if e.Kind == mission.EventOrchestrate && strings.Contains(e.Text, "with other") {
			found = true
		}
	}
	if !found {
		t.Error("manual review didn't use the configured model")
	}
}

func mustLog(t *testing.T, store mission.Store) []mission.Event {
	t.Helper()
	events, err := store.ReadLog("cache", 0)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestProposals_FlagResumeWithConclude(t *testing.T) {
	w, store := orchestrateFixture(t)
	t.Setenv(mission.EnvMission, "cache")
	t.Setenv("WS_ROLE", "orchestrator")

	run(t, w, nil, "mission", "flag", "CI is red on the main branch")
	run(t, w, nil, "mission", "resume-with", "c1", "the error path has no test")
	run(t, w, nil, "mission", "conclude", "PR merged, tests pass")

	got, _ := store.Load("cache")
	if got.Flag == nil || got.Flag.Text != "CI is red on the main branch" || got.Flag.By != "orchestrator" {
		t.Errorf("flag = %+v", got.Flag)
	}
	if got.Crew[0].ResumeWith != "the error path has no test" {
		t.Errorf("resume-with = %q", got.Crew[0].ResumeWith)
	}
	if got.Conclude == nil || got.Conclude.By != "orchestrator" {
		t.Errorf("conclude = %+v", got.Conclude)
	}
	show := run(t, w, nil, "mission", "show", "-f", "llm")
	for _, want := range []string{"Flagged (orchestrator): CI is red", "Suggested: resume c1 with: the error path", "Suggested conclusion (orchestrator): PR merged"} {
		if !strings.Contains(show.Stdout, want) {
			t.Errorf("show missing %q:\n%s", want, show.Stdout)
		}
	}

	// Mistakes come back as errors the orchestrator can correct.
	for args, want := range map[string]string{
		"resume-with c9 x": "has no crew c9 (it has: c1)",
	} {
		r := testutil.RunCommand(t, w.Root, nil, append([]string{"mission"}, strings.Fields(args)...)...)
		if r.Err == nil || !strings.Contains(r.Err.Error(), want) {
			t.Errorf("%s: err = %v", args, r.Err)
		}
	}
	store.StartCrew("cache", "shell", "")
	r := testutil.RunCommand(t, w.Root, nil, "mission", "resume-with", "c2", "go on")
	if r.Err == nil || !strings.Contains(r.Err.Error(), "recorded no conversation") {
		t.Errorf("resume-with crew without conversation: %v", r.Err)
	}
	run(t, w, nil, "mission", "flag", "--clear")
	if got, _ := store.Load("cache"); got.Flag != nil {
		t.Error("flag not cleared")
	}
}

// fileOrchestrator stands in for an agent that follows the one line it's
// given: it reports how many lines that was, then reads the file it names.
type fileOrchestrator struct{ echoOrchestrator }

func (fileOrchestrator) Name() string { return "file-orchestrator" }
func (fileOrchestrator) Orchestrate(prompt, dir, model string) (harness.Launch, error) {
	script := `printf 'lines=%s\n' "$(printf '%s' "$0" | wc -l | tr -d ' ')"; for w in $0; do case "$w" in /*orchestrator-prompt.md) head -n 1 "$w";; esac; done`
	return harness.Launch{Args: []string{"/bin/sh", "-c", script, prompt}, Dir: dir}, nil
}

func TestOrchestrate_PromptGoesInAFileNotTheCommandLine(t *testing.T) {
	w, store := orchestrateFixture(t)
	harness.Register(fileOrchestrator{})
	t.Setenv(harness.EnvDefault, "file-orchestrator")
	run(t, w, nil, "mission", "orchestrate", "-m", "cache", "--crew", "c1", "--trigger", "done")

	data, err := os.ReadFile(filepath.Join(store.Dir(), "cache", "orchestrator-prompt.md"))
	if err != nil || !strings.Contains(string(data), "Cache the lookup.") || !strings.Contains(string(data), "All tests pass.") {
		t.Fatalf("prompt file = %q, %v", data, err)
	}
	var summary string
	for _, e := range mustLog(t, store) {
		if e.Kind == mission.EventOrchestrate {
			summary = e.Text
		}
	}
	// One line on the command line, and the agent found the prompt from it.
	if !strings.Contains(summary, "lines=0") || !strings.Contains(summary, `You are the orchestrator for the mission "Cache"`) {
		t.Errorf("summary = %q", summary)
	}
}

func TestMissionErrors_NoUsageText(t *testing.T) {
	w, _ := orchestrateFixture(t)
	t.Setenv(mission.EnvMission, "cache")
	r := testutil.RunCommand(t, w.Root, nil, "mission", "resume-with", "c9", "x")
	if r.Err == nil || strings.Contains(r.Stderr+r.Stdout, "Usage:") {
		t.Errorf("a failed mission command printed usage:\n%s", r.Stderr)
	}
	// A mistyped flag still gets it.
	r = testutil.RunCommand(t, w.Root, nil, "mission", "resume-with", "--nope")
	if r.Err == nil || !strings.Contains(r.Stderr+r.Stdout, "Usage:") {
		t.Errorf("a mistyped flag didn't print usage:\n%s", r.Stderr)
	}
}
