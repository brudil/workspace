package cli_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brudil/workspace/internal/cli"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/testutil"
	"github.com/brudil/workspace/internal/web"
)

// Review is only of a mission's own capsules, compared with origin's
// default branch; the workspace the web UI serves says each capsule's
// commit, so an open review notices new ones.
func TestWebCapsuleAndWorkspace(t *testing.T) {
	t.Setenv(mission.EnvMission, "")
	t.Setenv(cli.ShellPWD, "")
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{Org: "test-org", DefaultBranch: "main", Repos: []testutil.RepoOpts{{Name: "frontend"}, {Name: "backend"}}})
	runOK := func(args ...string) {
		if r := testutil.RunCommand(t, w.Root, nil, args...); r.Err != nil {
			t.Fatalf("ws %v: %v\n%s", args, r.Err, r.Stderr)
		}
	}
	runOK("mission", "new", "Cache", "--brief", "## Goal\nCache it.")
	t.Setenv(mission.EnvMission, "cache")
	runOK("lift", "backend", "cache-lookup")
	t.Setenv(mission.EnvMission, "")
	find := cli.WebCapsule(w.Root)
	dir, base, err := find("cache", "backend", "cache-lookup")
	if err != nil || dir != filepath.Join(w.Root, "repos", "backend", "cache-lookup") || base != "origin/main" {
		t.Fatalf("capsule = %q %q %v", dir, base, err)
	}
	for _, c := range [][3]string{{"cache", "backend", "other"}, {"nope", "backend", "cache-lookup"}, {"cache", "frontend", "cache-lookup"}} {
		if _, _, err := find(c[0], c[1], c[2]); !errors.Is(err, web.ErrNotFound) {
			t.Errorf("%v: %v", c, err)
		}
	}

	ws, err := cli.WebWorkspace(w.Root)()
	if err != nil || len(ws.Missions) != 1 || len(ws.Missions[0].Capsules) != 1 {
		t.Fatalf("workspace = %+v, %v", ws, err)
	}
	if c := ws.Missions[0].Capsules[0]; !c.Exists || len(c.Head) != 40 || c.Branch != "cache-lookup" {
		t.Errorf("capsule = %+v", c)
	}

	os.RemoveAll(dir)
	if _, _, err := find("cache", "backend", "cache-lookup"); err == nil || errors.Is(err, web.ErrNotFound) {
		t.Errorf("a missing capsule folder: %v", err)
	}
}

// The mission page has what the deck's mission view has; artifacts are
// readable by their reference, and only the mission's own.
func TestWebMissionAndArtifact(t *testing.T) {
	t.Setenv(mission.EnvMission, "")
	t.Setenv(cli.ShellPWD, "")
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{Org: "test-org", DefaultBranch: "main", Repos: []testutil.RepoOpts{{Name: "backend"}}})
	runOK := func(args ...string) {
		if r := testutil.RunCommand(t, w.Root, nil, args...); r.Err != nil {
			t.Fatalf("ws %v: %v\n%s", args, r.Err, r.Stderr)
		}
	}
	runOK("mission", "new", "Cache", "--brief", "## Goal\nCache it.\n\n## Stages\n1. Propose it in `backend/cache-lookup`. Check in with me.\n2. Build it.\n")
	t.Setenv(mission.EnvMission, "cache")
	runOK("lift", "backend", "cache-lookup")
	t.Setenv(mission.EnvMission, "")
	store := mission.Store{Root: w.Root}
	os.MkdirAll(store.FilesDir("cache"), 0o755)
	os.WriteFile(filepath.Join(store.FilesDir("cache"), "notes.md"), []byte("# Notes\n\n```mermaid\ngraph TD; A-->B\n```\n"), 0o644)
	store.AddArtifact("cache", mission.Artifact{Ref: "mission:notes.md", Kind: "note", Label: "Spike notes"})
	store.AddArtifact("cache", mission.Artifact{Ref: "https://example.com/design", Kind: "link"})
	store.Update("cache", func(m *mission.Mission) error {
		m.Flag = &mission.Proposal{Text: "Ship it?", By: "orchestrator", Options: []string{"Yes", "No"}}
		return nil
	})

	d, err := cli.WebMission(w.Root)("cache", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Brief, "Cache it.") || d.Flag == nil || d.Flag.Options[1] != "No" || d.GitHub {
		t.Errorf("detail = %+v", d)
	}
	if len(d.Stages) != 2 || d.Stages[0].State != "current" || !d.Stages[0].CheckIn || d.Stages[1].State != "todo" {
		t.Errorf("stages = %+v", d.Stages)
	}
	if len(d.Capsules) != 1 || !d.Capsules[0].Exists || len(d.Capsules[0].Head) != 40 {
		t.Errorf("capsules = %+v", d.Capsules)
	}
	arts := map[string]web.Artifact{}
	for _, a := range d.Artifacts {
		arts[a.Ref] = a
	}
	if a := arts["mission:notes.md"]; !a.Readable || a.State != "private" || a.Label != "Spike notes" {
		t.Errorf("notes = %+v", a)
	}
	if a := arts["https://example.com/design"]; a.Readable || a.State != "link" || a.URL != "https://example.com/design" {
		t.Errorf("link = %+v", a)
	}
	words := []string{}
	for _, e := range d.Events {
		words = append(words, e.Text)
	}
	if all := strings.Join(words, " | "); !strings.Contains(all, "capsule backend/cache-lookup added") {
		t.Errorf("events = %s", all)
	}
	if _, err := cli.WebMission(w.Root)("nope", false); !errors.Is(err, web.ErrNotFound) {
		t.Errorf("no mission: %v", err)
	}

	read := cli.WebArtifact(w.Root)
	a, err := read("cache", "mission:notes.md")
	if err != nil || !a.Markdown || !strings.Contains(a.Text, "```mermaid") {
		t.Fatalf("read = %+v, %v", a, err)
	}
	if _, err := read("cache", "https://example.com/design"); err == nil || errors.Is(err, web.ErrNotFound) {
		t.Errorf("a link isn't a file: %v", err)
	}
	// Only references the mission recorded: not any path.
	for _, ref := range []string{"mission:../../../ws.toml", "mission:other.md", "backend:README.md"} {
		if _, err := read("cache", ref); !errors.Is(err, web.ErrNotFound) {
			t.Errorf("%s: %v", ref, err)
		}
	}

	// A change to the mission's log shows in the workspace, so open pages
	// hear of it.
	before, _ := cli.WebWorkspace(w.Root)()
	time.Sleep(20 * time.Millisecond)
	store.AppendLog("cache", mission.Event{Kind: mission.EventNote, Text: "hi"})
	after, _ := cli.WebWorkspace(w.Root)()
	if !after.Missions[0].Updated.After(before.Missions[0].Updated) {
		t.Errorf("updated %v → %v", before.Missions[0].Updated, after.Missions[0].Updated)
	}
}
