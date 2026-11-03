package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/cli"
	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/testutil"
)

func missionWorkspace(t *testing.T) *testutil.Workspace {
	t.Helper()
	t.Setenv(mission.EnvMission, "")
	t.Setenv(mission.EnvCrew, "")
	t.Setenv(cli.ShellPWD, "")
	return testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos: []testutil.RepoOpts{
			{Name: "frontend", Aliases: []string{"fe"}},
			{Name: "backend", Branches: []string{"existing"}},
		},
	})
}

func run(t *testing.T, w *testutil.Workspace, gh github.Client, args ...string) testutil.Result {
	t.Helper()
	r := testutil.RunCommand(t, w.Root, gh, args...)
	if r.Err != nil {
		t.Fatalf("ws %s: %v\nstderr: %s", strings.Join(args, " "), r.Err, r.Stderr)
	}
	return r
}

func loadMission(t *testing.T, w *testutil.Workspace, slug string) *mission.Mission {
	t.Helper()
	m, err := mission.Store{Root: w.Root}.Load(slug)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func logKinds(t *testing.T, w *testutil.Workspace, slug string) []string {
	t.Helper()
	events, err := mission.Store{Root: w.Root}.ReadLog(slug, 0)
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]string, len(events))
	for i, e := range events {
		kinds[i] = e.Kind
	}
	return kinds
}

func TestMission_NewListShow(t *testing.T) {
	w := missionWorkspace(t)

	run(t, w, nil, "mission", "new", "Fix token refresh", "--brief", "## Goal\nStop the race.")

	list := run(t, w, nil, "mission", "list", "-f", "llm")
	if !strings.Contains(list.Stdout, "fix-token-refresh [draft] Fix token refresh") {
		t.Errorf("list llm = %q", list.Stdout)
	}

	show := run(t, w, nil, "mission", "show", "fix-token-refresh", "-f", "llm")
	for _, want := range []string{"Mission: fix-token-refresh", "State: draft", "Stop the race.", "Capsules: none yet", "created"} {
		if !strings.Contains(show.Stdout, want) {
			t.Errorf("show llm missing %q:\n%s", want, show.Stdout)
		}
	}
}

func TestMission_ShowUnknown(t *testing.T) {
	w := missionWorkspace(t)
	r := testutil.RunCommand(t, w.Root, nil, "mission", "show", "nope")
	if r.Err == nil || !strings.Contains(r.Err.Error(), `no mission "nope"`) {
		t.Errorf("err = %v", r.Err)
	}
}

func TestMission_ShowNeedsAMission(t *testing.T) {
	w := missionWorkspace(t)
	r := testutil.RunCommand(t, w.Root, nil, "mission", "show")
	if r.Err == nil || !strings.Contains(r.Err.Error(), "no mission given") {
		t.Errorf("err = %v", r.Err)
	}
}

func TestLift_WithWSMissionAttachesCapsule(t *testing.T) {
	w := missionWorkspace(t)
	run(t, w, nil, "mission", "new", "Auth", "--no-edit")
	t.Setenv(mission.EnvMission, "auth")
	t.Setenv(mission.EnvCrew, "c1")

	r := run(t, w, nil, "lift", "fe", "auth-fix")
	if !strings.Contains(r.Stderr, "→ mission auth") {
		t.Errorf("lift stderr missing mission line:\n%s", r.Stderr)
	}

	m := loadMission(t, w, "auth")
	if len(m.Capsules) != 1 || m.Capsules[0] != (mission.Capsule{Repo: "frontend", Name: "auth-fix", Branch: "auth-fix"}) {
		t.Errorf("capsules = %+v", m.Capsules)
	}
	events, _ := mission.Store{Root: w.Root}.ReadLog("auth", 0)
	last := events[len(events)-1]
	if last.Kind != mission.EventCapsuleAdded || last.Crew != "c1" {
		t.Errorf("last event = %+v", last)
	}
}

func TestDock_WithWSMissionAttachesCapsule(t *testing.T) {
	w := missionWorkspace(t)
	run(t, w, nil, "mission", "new", "Docked", "--no-edit")
	t.Setenv(mission.EnvMission, "docked")

	run(t, w, nil, "dock", "backend", "existing")
	if m := loadMission(t, w, "docked"); !m.HasCapsule("backend", "existing") {
		t.Errorf("capsules = %+v", m.Capsules)
	}
}

func TestLift_WithoutWSMissionIsUnchanged(t *testing.T) {
	w := missionWorkspace(t)
	r := run(t, w, nil, "lift", "fe", "plain")
	if strings.Contains(r.Stderr, "mission") {
		t.Errorf("lift mentioned missions without WS_MISSION:\n%s", r.Stderr)
	}
	if _, err := os.Stat(filepath.Join(w.Root, ".ws")); !os.IsNotExist(err) {
		t.Errorf(".ws created without any mission: %v", err)
	}
}

func TestLift_WithMissingWSMissionWarnsButSucceeds(t *testing.T) {
	w := missionWorkspace(t)
	t.Setenv(mission.EnvMission, "ghost")
	r := run(t, w, nil, "lift", "fe", "x")
	if !strings.Contains(r.Stderr, "WS_MISSION=ghost does not exist") {
		t.Errorf("no warning:\n%s", r.Stderr)
	}
	if _, err := os.Stat(filepath.Join(w.Root, "repos", "frontend", "x")); err != nil {
		t.Errorf("capsule not created: %v", err)
	}
}

func TestMission_NoteResolution(t *testing.T) {
	w := missionWorkspace(t)
	run(t, w, nil, "mission", "new", "One", "--no-edit")
	run(t, w, nil, "mission", "new", "Two", "--no-edit")

	// Explicit flag.
	run(t, w, nil, "mission", "note", "-m", "one", "flag", "note")
	// $WS_MISSION.
	t.Setenv(mission.EnvMission, "two")
	run(t, w, nil, "mission", "note", "env note")
	// cwd: standing in a capsule owned by "one".
	t.Setenv(mission.EnvMission, "")
	run(t, w, nil, "lift", "fe", "cap")
	run(t, w, nil, "mission", "attach", "-m", "one", "fe", "cap")
	t.Setenv(cli.ShellPWD, filepath.Join(w.Root, "repos", "frontend", "cap"))
	run(t, w, nil, "mission", "note", "cwd note")

	notes := func(slug string) []string {
		events, _ := mission.Store{Root: w.Root}.ReadLog(slug, 0)
		var out []string
		for _, e := range events {
			if e.Kind == mission.EventNote {
				out = append(out, e.Text)
			}
		}
		return out
	}
	if got := notes("one"); strings.Join(got, "|") != "flag note|cwd note" {
		t.Errorf("one notes = %q", got)
	}
	if got := notes("two"); strings.Join(got, "|") != "env note" {
		t.Errorf("two notes = %q", got)
	}
}

func TestMission_AttachRules(t *testing.T) {
	w := missionWorkspace(t)
	run(t, w, nil, "mission", "new", "A", "--no-edit")
	run(t, w, nil, "mission", "new", "B", "--no-edit")
	run(t, w, nil, "lift", "fe", "shared")
	run(t, w, nil, "mission", "attach", "-m", "a", "frontend", "shared")

	// Owned elsewhere: refused without --move.
	r := testutil.RunCommand(t, w.Root, nil, "mission", "attach", "-m", "b", "fe", "shared")
	if r.Err == nil || !strings.Contains(r.Err.Error(), "already belongs to mission a") {
		t.Fatalf("err = %v", r.Err)
	}
	run(t, w, nil, "mission", "attach", "-m", "b", "fe", "shared", "--move")
	if loadMission(t, w, "a").HasCapsule("frontend", "shared") || !loadMission(t, w, "b").HasCapsule("frontend", "shared") {
		t.Error("--move didn't move the capsule")
	}

	// Ground can't join.
	r = testutil.RunCommand(t, w.Root, nil, "mission", "attach", "-m", "a", "fe", ".ground")
	if r.Err == nil {
		t.Error("attaching .ground should fail")
	}

	// Detach.
	run(t, w, nil, "mission", "detach", "-m", "b", "fe", "shared")
	if loadMission(t, w, "b").HasCapsule("frontend", "shared") {
		t.Error("detach didn't remove the capsule")
	}
}

func TestBurn_LogsOnOwningMissionAndKeepsCapsule(t *testing.T) {
	w := missionWorkspace(t)
	run(t, w, nil, "mission", "new", "Burny", "--no-edit")
	t.Setenv(mission.EnvMission, "burny")
	run(t, w, nil, "lift", "fe", "temp")
	t.Setenv(mission.EnvMission, "")

	run(t, w, nil, "burn", "fe", "temp")

	kinds := logKinds(t, w, "burny")
	if kinds[len(kinds)-1] != mission.EventCapsuleBurned {
		t.Errorf("log kinds = %v", kinds)
	}
	if !loadMission(t, w, "burny").HasCapsule("frontend", "temp") {
		t.Error("burned capsule dropped from mission")
	}
	show := run(t, w, nil, "mission", "show", "burny", "-f", "llm")
	if !strings.Contains(show.Stdout, "frontend/temp [burned]") || !strings.Contains(show.Stdout, "State: draft") {
		t.Errorf("show = %s", show.Stdout)
	}
}

func TestMission_StateFromPRs(t *testing.T) {
	w := missionWorkspace(t)
	run(t, w, nil, "mission", "new", "Shipping", "--no-edit")
	t.Setenv(mission.EnvMission, "shipping")
	run(t, w, nil, "lift", "fe", "ship")
	t.Setenv(mission.EnvMission, "")

	open := &testutil.StubClient{PRsForRepoFn: func(org, repo string) ([]github.PR, error) {
		if repo == "frontend" {
			return []github.PR{{Number: 7, HeadRefName: "ship", State: "OPEN"}}, nil
		}
		return nil, nil
	}}
	show := run(t, w, open, "mission", "show", "shipping", "-f", "llm")
	if !strings.Contains(show.Stdout, "State: review") || !strings.Contains(show.Stdout, "PR #7 open") {
		t.Errorf("open PR show = %s", show.Stdout)
	}

	merged := &testutil.StubClient{MergedPRsForRepoFn: func(org, repo string) ([]github.PR, error) {
		if repo == "frontend" {
			return []github.PR{{Number: 7, HeadRefName: "ship", State: "MERGED"}}, nil
		}
		return nil, nil
	}}
	run(t, w, nil, "burn", "fe", "ship")
	show = run(t, w, merged, "mission", "show", "shipping", "-f", "llm")
	if !strings.Contains(show.Stdout, "State: landed") {
		t.Errorf("merged show = %s", show.Stdout)
	}
}

func TestMission_ParkArchiveReopen(t *testing.T) {
	w := missionWorkspace(t)
	run(t, w, nil, "mission", "new", "Later", "--no-edit")

	run(t, w, nil, "mission", "park", "later")
	if l := run(t, w, nil, "mission", "list", "-f", "llm"); !strings.Contains(l.Stdout, "later [parked]") {
		t.Errorf("list = %s", l.Stdout)
	}

	run(t, w, nil, "mission", "archive", "later")
	if l := run(t, w, nil, "mission", "list", "-f", "llm"); strings.Contains(l.Stdout, "later") {
		t.Errorf("archived mission listed by default: %s", l.Stdout)
	}
	if l := run(t, w, nil, "mission", "list", "-f", "llm", "--all"); !strings.Contains(l.Stdout, "later [archived]") {
		t.Errorf("list --all = %s", l.Stdout)
	}

	run(t, w, nil, "mission", "reopen", "later")
	if l := run(t, w, nil, "mission", "list", "-f", "llm"); !strings.Contains(l.Stdout, "later [draft]") {
		t.Errorf("reopened list = %s", l.Stdout)
	}
}

func TestStatusLLM_MissionHeaderOnlyWithWSMission(t *testing.T) {
	w := missionWorkspace(t)
	run(t, w, nil, "mission", "new", "Header", "--no-edit")

	plain := run(t, w, nil, "status", "-f", "llm")
	if strings.Contains(plain.Stdout, "Mission:") || !strings.HasPrefix(plain.Stdout, "Workspace:") {
		t.Errorf("status without WS_MISSION changed:\n%s", plain.Stdout)
	}

	t.Setenv(mission.EnvMission, "header")
	withMission := run(t, w, nil, "status", "-f", "llm")
	if !strings.HasPrefix(withMission.Stdout, "Mission: header — Header [draft]\n") {
		t.Errorf("status header:\n%s", withMission.Stdout)
	}
	if !strings.HasSuffix(withMission.Stdout, plain.Stdout) {
		t.Errorf("status body changed under WS_MISSION:\n%s", withMission.Stdout)
	}
}
