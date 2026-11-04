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

// echoReviewer "reviews" by printing its role and mission, so tests can see
// it ran with the reviewer's environment.
type echoReviewer struct{}

func (echoReviewer) Name() string     { return "echo-reviewer" }
func (echoReviewer) Available() error { return nil }
func (echoReviewer) Start(harness.Brief) (harness.Launch, error) {
	return harness.Launch{}, harness.ErrNoResume
}
func (echoReviewer) Resume(string, string) (harness.Launch, error) {
	return harness.Launch{}, harness.ErrNoResume
}
func (echoReviewer) Review(prompt, dir, model string) (harness.Launch, error) {
	return harness.Launch{Args: []string{"/bin/sh", "-c", `echo "reviewed as $WS_ROLE on $WS_MISSION with ` + model + `"`, prompt}, Dir: dir}, nil
}

func reviewFixture(t *testing.T) (*testutil.Workspace, mission.Store) {
	t.Helper()
	w := missionWorkspace(t)
	harness.Register(echoReviewer{})
	t.Setenv(harness.EnvDefault, "echo-reviewer")
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

func TestReview_PromptHasTheContext(t *testing.T) {
	w, _ := reviewFixture(t)
	r := run(t, w, nil, "mission", "review", "-m", "cache", "--crew", "c1", "--trigger", "done", "--print-prompt")
	for _, want := range []string{
		"reviewer for the mission \"Cache\" (cache)",
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
	if !strings.Contains(r.Stderr, "--model claude-haiku-5-5-medium") && !strings.Contains(r.Stderr, "would run") {
		t.Errorf("stderr: %s", r.Stderr)
	}
}

func TestReview_RunsAsReviewerAndLogs(t *testing.T) {
	w, store := reviewFixture(t)
	run(t, w, nil, "mission", "review", "-m", "cache", "--crew", "c1", "--trigger", "done", "--auto")
	events, _ := store.ReadLog("cache", 0)
	var reviews []string
	for _, e := range events {
		if e.Kind == mission.EventReview {
			reviews = append(reviews, e.Text)
		}
	}
	if len(reviews) != 2 || !strings.Contains(reviews[1], "reviewed as reviewer on cache with claude-haiku-5-5-medium") {
		t.Fatalf("review events = %q", reviews)
	}
	// The same event isn't reviewed twice.
	run(t, w, nil, "mission", "review", "-m", "cache", "--crew", "c1", "--trigger", "done", "--auto")
	events2, _ := store.ReadLog("cache", 0)
	if len(events2) != len(events) {
		t.Errorf("auto review ran twice for one event")
	}
	// A new event (crew reports done) gets a new review.
	store.SetCrewStatus("cache", "c1", mission.CrewDone, "again")
	run(t, w, nil, "mission", "review", "-m", "cache", "--crew", "c1", "--trigger", "done", "--auto")
	events3, _ := store.ReadLog("cache", 0)
	if len(events3) <= len(events2)+1 {
		t.Errorf("new event wasn't reviewed")
	}
}

func TestReview_ConfigTurnsAutoOff(t *testing.T) {
	w, store := reviewFixture(t)
	os.WriteFile(filepath.Join(w.Root, "ws.local.toml"), []byte("[review]\nenabled = false\nmodel = \"other\"\n"), 0o644)
	run(t, w, nil, "mission", "review", "-m", "cache", "--crew", "c1", "--auto")
	for _, e := range mustLog(t, store) {
		if e.Kind == mission.EventReview {
			t.Fatal("auto review ran with [review] enabled = false")
		}
	}
	// A manual review still runs, with the configured model.
	run(t, w, nil, "mission", "review", "-m", "cache")
	found := false
	for _, e := range mustLog(t, store) {
		if e.Kind == mission.EventReview && strings.Contains(e.Text, "with other") {
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
	w, store := reviewFixture(t)
	t.Setenv(mission.EnvMission, "cache")
	t.Setenv("WS_ROLE", "reviewer")

	run(t, w, nil, "mission", "flag", "CI is red on the main branch")
	run(t, w, nil, "mission", "resume-with", "c1", "the error path has no test")
	run(t, w, nil, "mission", "conclude", "PR merged, tests pass")

	got, _ := store.Load("cache")
	if got.Flag == nil || got.Flag.Text != "CI is red on the main branch" || got.Flag.By != "reviewer" {
		t.Errorf("flag = %+v", got.Flag)
	}
	if got.Crew[0].ResumeWith != "the error path has no test" {
		t.Errorf("resume-with = %q", got.Crew[0].ResumeWith)
	}
	if got.Conclude == nil || got.Conclude.By != "reviewer" {
		t.Errorf("conclude = %+v", got.Conclude)
	}
	show := run(t, w, nil, "mission", "show", "-f", "llm")
	for _, want := range []string{"Flagged (reviewer): CI is red", "Suggested: resume c1 with: the error path", "Suggested conclusion (reviewer): PR merged"} {
		if !strings.Contains(show.Stdout, want) {
			t.Errorf("show missing %q:\n%s", want, show.Stdout)
		}
	}

	// Mistakes come back as errors the reviewer can correct.
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
