package cli_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

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
