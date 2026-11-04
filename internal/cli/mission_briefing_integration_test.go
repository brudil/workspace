package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/testutil"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBriefing_HumanPath(t *testing.T) {
	w := missionWorkspace(t)
	store := mission.Store{Root: w.Root}
	m, err := store.CreateBriefing("fe issue 45")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(mission.EnvMission, m.Slug)

	// Not ready without a brief.
	r := testutil.RunCommand(t, w.Root, nil, "mission", "ready")
	if r.Err == nil || !strings.Contains(r.Err.Error(), "write the brief first") {
		t.Fatalf("ready without brief: %v", r.Err)
	}

	run(t, w, nil, "mission", "brief", "--set", writeFile(t, t.TempDir(), "b.md", "## Goal\nFix the race in token refresh."))
	run(t, w, nil, "mission", "title", "Fix", "token", "refresh", "race")
	if got := run(t, w, nil, "mission", "brief"); got.Stdout != "## Goal\nFix the race in token refresh.\n" {
		t.Errorf("brief = %q", got.Stdout)
	}

	// Not ready without a capsule.
	r = testutil.RunCommand(t, w.Root, nil, "mission", "ready")
	if r.Err == nil || !strings.Contains(r.Err.Error(), "create the capsules") {
		t.Fatalf("ready without capsules: %v", r.Err)
	}

	run(t, w, nil, "lift", "fe", "token-race")
	ready := run(t, w, nil, "mission", "ready", "--slug", "token-race")
	if !strings.Contains(ready.Stderr, "Mission token-race is ready: Fix token refresh race · 1 capsule") {
		t.Errorf("ready output:\n%s", ready.Stderr)
	}
	got, err := store.Load("token-race")
	if err != nil || got.State != "" || !got.HasCapsule("frontend", "token-race") {
		t.Errorf("after ready: %+v, %v", got, err)
	}
	if show := run(t, w, nil, "mission", "show", "token-race", "-f", "llm"); !strings.Contains(show.Stdout, "Requested as: fe issue 45") {
		t.Errorf("show:\n%s", show.Stdout)
	}
}

func TestBriefing_CrewCantEditBriefOnceReady(t *testing.T) {
	w := missionWorkspace(t)
	run(t, w, nil, "mission", "new", "Ready one", "--no-edit")
	t.Setenv(mission.EnvMission, "ready-one")
	t.Setenv(mission.EnvCrew, "c1")
	r := testutil.RunCommand(t, w.Root, nil, "mission", "brief", "--set", writeFile(t, t.TempDir(), "b.md", "rewritten"))
	if r.Err == nil || !strings.Contains(r.Err.Error(), "belongs to the user") {
		t.Errorf("crew edited the brief: %v", r.Err)
	}
	r = testutil.RunCommand(t, w.Root, nil, "mission", "title", "Renamed by agent")
	if r.Err == nil {
		t.Error("crew retitled a briefed mission")
	}
}

func TestBriefing_ReadyEndsTheBriefingCrew(t *testing.T) {
	w := missionWorkspace(t)
	store := mission.Store{Root: w.Root}
	m, _ := store.CreateBriefing("api: cache the user lookup")

	// Stand-in for the briefing agent: its own process group, like crew in a pty.
	agent := exec.Command("sleep", "30")
	agent.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- agent.Wait() }()
	t.Cleanup(func() { agent.Process.Kill() })

	crew, _ := store.StartCrewAs(m.Slug, "cursor", "conv-9", mission.RoleBriefing)
	store.SetCrewProcess(m.Slug, crew.ID, agent.Process.Pid, "")
	t.Setenv(mission.EnvMission, m.Slug)
	t.Setenv(mission.EnvCrew, crew.ID)

	run(t, w, nil, "mission", "brief", "--set", writeFile(t, t.TempDir(), "b.md", "## Goal\nCache it."))
	run(t, w, nil, "lift", "backend", "cache-lookup")
	ready := run(t, w, nil, "mission", "ready")
	if !strings.Contains(ready.Stderr, "this briefing session ends now") {
		t.Errorf("ready output:\n%s", ready.Stderr)
	}

	got, _ := store.Load(m.Slug)
	if got.Crew[0].Exit != "briefed" || got.Crew[0].Ended == nil {
		t.Errorf("briefing crew = %+v", got.Crew[0])
	}
	select {
	case <-exited:
	case <-time.After(6 * time.Second):
		t.Fatal("briefing crew wasn't hung up")
	}
}

func TestSignals_AskDoneArtifact(t *testing.T) {
	w := missionWorkspace(t)
	store := mission.Store{Root: w.Root}
	run(t, w, nil, "mission", "new", "Cache", "--no-edit")
	store.StartCrew("cache", "cursor", "conv")
	t.Setenv(mission.EnvMission, "cache")

	// Crew only.
	if r := testutil.RunCommand(t, w.Root, nil, "mission", "ask", "which?"); r.Err == nil || !strings.Contains(r.Err.Error(), "WS_CREW") {
		t.Errorf("ask without WS_CREW: %v", r.Err)
	}
	t.Setenv(mission.EnvCrew, "c1")
	run(t, w, nil, "mission", "ask", "Per-org", "or", "global?")
	got, _ := store.Load("cache")
	if got.Crew[0].Status != mission.CrewAsking || got.Crew[0].Note != "Per-org or global?" {
		t.Errorf("after ask: %+v", got.Crew[0])
	}
	run(t, w, nil, "mission", "done", "Added the per-org cache")
	run(t, w, nil, "mission", "artifact", "add", "https://example.com/design", "Design", "doc")
	show := run(t, w, nil, "mission", "show", "-f", "llm")
	for _, want := range []string{"Crew c1 done: Added the per-org cache", "https://example.com/design — Design doc"} {
		if !strings.Contains(show.Stdout, want) {
			t.Errorf("show missing %q:\n%s", want, show.Stdout)
		}
	}
}

func TestSignals_BriefingCrewIsToldToUseReady(t *testing.T) {
	w := missionWorkspace(t)
	store := mission.Store{Root: w.Root}
	m, _ := store.CreateBriefing("something")
	store.StartCrewAs(m.Slug, "cursor", "", mission.RoleBriefing)
	t.Setenv(mission.EnvMission, m.Slug)
	t.Setenv(mission.EnvCrew, "c1")
	if r := testutil.RunCommand(t, w.Root, nil, "mission", "done", "briefed it"); r.Err == nil || !strings.Contains(r.Err.Error(), "ws mission ready") {
		t.Errorf("briefing crew done: %v", r.Err)
	}
}

func TestMissionNew_PlainTextWaitsForBriefing(t *testing.T) {
	w := missionWorkspace(t)
	r := run(t, w, nil, "mission", "new", "fe", "issue", "45")
	if strings.TrimSpace(r.Stdout) != "fe-issue-45" || !strings.Contains(r.Stderr, "waiting to be briefed") {
		t.Errorf("stdout %q stderr %q", r.Stdout, r.Stderr)
	}
	m, _ := mission.Store{Root: w.Root}.Load("fe-issue-45")
	if m.State != mission.Briefing || m.Request != "fe issue 45" || m.From != "" {
		t.Errorf("mission = %+v", m)
	}
}

func TestMissionNew_FromCrewRecordsOrigin(t *testing.T) {
	w := missionWorkspace(t)
	store := mission.Store{Root: w.Root}
	run(t, w, nil, "mission", "new", "Parent", "--no-edit")
	store.StartCrew("parent", "cursor", "")
	t.Setenv(mission.EnvMission, "parent")
	t.Setenv(mission.EnvCrew, "c1")

	run(t, w, nil, "mission", "new", "the token refresh test is flaky")
	child, err := store.Load("the-token-refresh-test-is-flaky")
	if err != nil || child.State != mission.Briefing || child.From != "parent/c1" {
		t.Fatalf("child = %+v, %v", child, err)
	}
	events, _ := store.ReadLog("parent", 0)
	last := events[len(events)-1]
	if last.Kind != mission.EventNote || last.Crew != "c1" || !strings.Contains(last.Text, "raised mission the-token-refresh-test-is-flaky") {
		t.Errorf("parent log = %+v", last)
	}
	if show := run(t, w, nil, "mission", "show", "the-token-refresh-test-is-flaky", "-f", "llm"); !strings.Contains(show.Stdout, "Raised from: parent/c1") {
		t.Errorf("show:\n%s", show.Stdout)
	}
	// It didn't attach the new mission to the parent's capsules or vice versa.
	if p, _ := store.Load("parent"); p.State == mission.Briefing {
		t.Error("parent changed state")
	}
}

func TestMissionNew_BriefFlagStillMakesItReady(t *testing.T) {
	w := missionWorkspace(t)
	run(t, w, nil, "mission", "new", "Ready", "one", "--brief", "## Goal\nNow.")
	m, _ := mission.Store{Root: w.Root}.Load("ready-one")
	if m.State != "" {
		t.Errorf("state = %q", m.State)
	}
}
