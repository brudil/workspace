package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/brudil/workspace/internal/mission"
)

func TestCheckMissions_AbsentWithoutMissions(t *testing.T) {
	w := &Workspace{Root: t.TempDir()}
	if got := w.checkMissions(); len(got.Checks) != 0 {
		t.Errorf("checks = %+v, want none", got.Checks)
	}
}

func TestCheckMissions_ExcludeAndUnknownRepo(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	store := mission.Store{Root: root}
	m, err := store.Create("Doc")
	if err != nil {
		t.Fatal(err)
	}
	store.Update(m.Slug, func(m *mission.Mission) error { m.AddCapsule("gone", "x"); return nil })
	// Undo the exclude Create added, as if it had been lost.
	os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), nil, 0o644)

	w := &Workspace{Root: root, RepoNames: []string{"frontend"}}
	got := w.checkMissions()
	if len(got.Checks) != 2 {
		t.Fatalf("checks = %+v", got.Checks)
	}
	exclude, unknown := got.Checks[0], got.Checks[1]
	if exclude.Status != CheckWarn || exclude.Fix == nil {
		t.Errorf("exclude check = %+v", exclude)
	}
	if unknown.Status != CheckWarn || unknown.FixHint != "ws mission detach -m doc gone x" {
		t.Errorf("unknown repo check = %+v", unknown)
	}

	if err := exclude.Fix(); err != nil {
		t.Fatal(err)
	}
	if again := w.checkMissions(); again.Checks[0].Status != CheckOK {
		t.Errorf("exclude still failing after fix: %+v", again.Checks[0])
	}
}
