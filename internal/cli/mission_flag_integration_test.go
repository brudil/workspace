package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/cli"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/testutil"
)

// The orchestrator's flag is a question with answers to pick; answering it
// clears the flag and runs the orchestrator again with the answer.
func TestFlag_AnsweringRunsTheOrchestratorWithTheAnswer(t *testing.T) {
	w, store := orchestrateFixture(t)
	runs, restore := cli.CaptureOrchestratorRuns()
	defer restore()

	t.Setenv("WS_ROLE", "orchestrator")
	run(t, w, nil, "mission", "flag", "-m", "cache", "Commit and open PRs?", "--option", "Commit both and open PRs", "--option", "Leave them uncommitted")
	t.Setenv("WS_ROLE", "")
	if m, _ := store.Load("cache"); m.Flag == nil || len(m.Flag.Options) != 2 {
		t.Fatalf("flag = %+v", m.Flag)
	}

	t.Setenv(mission.EnvCrew, "c1")
	if r := testutil.RunCommand(t, w.Root, nil, "mission", "answer", "-m", "cache", "yes"); r.Err == nil || !strings.Contains(r.Err.Error(), "only the user answers") {
		t.Errorf("crew answering: %v", r.Err)
	}
	t.Setenv(mission.EnvCrew, "")

	r := run(t, w, nil, "mission", "answer", "-m", "cache", "Commit both and open PRs")
	if !strings.Contains(r.Stderr, "the orchestrator is acting on it") || strings.Join(*runs, ",") != "cache//answer" {
		t.Errorf("stderr %q, runs %v", r.Stderr, *runs)
	}
	m, _ := store.Load("cache")
	if m.Flag != nil || m.Answer == nil || m.Answer.Question != "Commit and open PRs?" || m.Answer.Text != "Commit both and open PRs" {
		t.Errorf("after answering: flag %+v answer %+v", m.Flag, m.Answer)
	}

	prompt := run(t, w, nil, "mission", "orchestrate", "-m", "cache", "--trigger", "answer", "--print-prompt").Stdout
	for _, want := range []string{
		"You asked: Commit and open PRs?", "They answered: Commit both and open PRs", "Act on their answer now",
		`--option "<answer>"`, "don't flag the same question",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}

	if r := testutil.RunCommand(t, w.Root, nil, "mission", "answer", "-m", "cache", "again"); r.Err == nil || !strings.Contains(r.Err.Error(), "isn't flagged") {
		t.Errorf("answering twice: %v", r.Err)
	}
}

func TestFlag_AnswerWithTheOrchestratorOff(t *testing.T) {
	w, store := orchestrateFixture(t)
	runs, restore := cli.CaptureOrchestratorRuns()
	defer restore()
	os.WriteFile(filepath.Join(w.Root, "ws.local.toml"), []byte("[orchestrator]\nenabled = false\n"), 0o644)
	store.SetFlag("cache", "Which way?", "orchestrator")
	r := run(t, w, nil, "mission", "answer", "-m", "cache", "Left")
	if !strings.Contains(r.Stderr, "the orchestrator is off") || len(*runs) != 0 {
		t.Errorf("stderr %q, runs %v", r.Stderr, *runs)
	}
}
