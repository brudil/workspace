package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/cli"
	"github.com/brudil/workspace/internal/testutil"
)

// groupsWorkspace has frontend (product), backend (ungrouped), and two repos
// in the hidden platform group: infra (cloned) and charts (not cloned).
func groupsWorkspace(t *testing.T) *testutil.Workspace {
	t.Helper()
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		HiddenGroups:  []string{"platform"},
		Repos: []testutil.RepoOpts{
			{Name: "frontend", Aliases: []string{"fe"}, Group: "product"},
			{Name: "backend"},
			{Name: "infra", Group: "platform"},
			{Name: "charts", Group: "platform", NotCloned: true},
		},
	})
	t.Setenv(cli.ShellPWD, w.Root)
	return w
}

func statusRepoNames(t *testing.T, w *testutil.Workspace, args ...string) map[string]map[string]any {
	t.Helper()
	res := testutil.RunCommand(t, w.Root, nil, append([]string{"status", "--format", "json"}, args...)...)
	if res.Err != nil {
		t.Fatalf("status: %v\nstderr: %s", res.Err, res.Stderr)
	}
	var out struct {
		Repos []map[string]any `json:"repos"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		t.Fatalf("parsing status json: %v\n%s", err, res.Stdout)
	}
	repos := make(map[string]map[string]any)
	for _, r := range out.Repos {
		repos[r["name"].(string)] = r
	}
	return repos
}

func TestGroups_StatusHidesHiddenGroups(t *testing.T) {
	w := groupsWorkspace(t)

	repos := statusRepoNames(t, w)
	if _, ok := repos["infra"]; ok {
		t.Error("status shows infra, want hidden group left out")
	}
	if got := repos["frontend"]["group"]; got != "product" {
		t.Errorf("frontend group = %v, want product", got)
	}
	if _, ok := repos["backend"]; !ok {
		t.Error("status missing ungrouped backend")
	}
}

func TestGroups_StatusShowsHiddenRepoWithCapsule(t *testing.T) {
	w := groupsWorkspace(t)
	bare := filepath.Join(w.Root, "repos", "infra", ".bare")
	testutil.GitCmd(t, bare, "worktree", "add", "-b", "fix", filepath.Join(w.Root, "repos", "infra", "fix"), "main")

	repos := statusRepoNames(t, w)
	infra, ok := repos["infra"]
	if !ok {
		t.Fatal("status hides infra, want it shown while it has a capsule")
	}
	if infra["hidden"] != true {
		t.Errorf("infra hidden = %v, want true", infra["hidden"])
	}
}

func TestGroups_StatusGroupFlagSelectsGroup(t *testing.T) {
	w := groupsWorkspace(t)

	repos := statusRepoNames(t, w, "--group", "platform")
	if _, ok := repos["infra"]; !ok {
		t.Error("status --group platform missing infra")
	}
	if _, ok := repos["frontend"]; ok {
		t.Error("status --group platform shows frontend, want only platform repos")
	}
}

func TestGroups_UnknownGroupFlag(t *testing.T) {
	w := groupsWorkspace(t)
	res := testutil.RunCommand(t, w.Root, nil, "status", "--format", "json", "--group", "nope")
	if res.Err == nil || !strings.Contains(res.Err.Error(), "unknown group") {
		t.Fatalf("err = %v, want unknown group", res.Err)
	}
}

func TestGroups_RgSkipsHiddenByDefault(t *testing.T) {
	w := groupsWorkspace(t)
	res := testutil.RunCommand(t, w.Root, nil, "rg", "--roots")
	if res.Err != nil {
		t.Fatalf("rg --roots: %v\nstderr: %s", res.Err, res.Stderr)
	}
	if strings.Contains(res.Stdout, "infra") {
		t.Errorf("rg --roots includes hidden infra:\n%s", res.Stdout)
	}
}

func TestGroups_RgGroupClonesOnFirstUse(t *testing.T) {
	w := groupsWorkspace(t)
	w.RedirectClones(t)

	res := testutil.RunCommand(t, w.Root, nil, "rg", "--group", "platform", "--roots")
	if res.Err != nil {
		t.Fatalf("rg --group platform --roots: %v\nstderr: %s", res.Err, res.Stderr)
	}
	want := filepath.Join(w.Root, "repos", "charts", ".ground") + "\n" +
		filepath.Join(w.Root, "repos", "infra", ".ground") + "\n"
	if res.Stdout != want {
		t.Errorf("stdout = %q, want %q", res.Stdout, want)
	}
	if !strings.Contains(res.Stderr, "not cloned yet") {
		t.Errorf("stderr = %q, want a note that charts is being cloned", res.Stderr)
	}
}

func TestGroups_SetupSkipsHiddenGroups(t *testing.T) {
	w := groupsWorkspace(t)
	w.RedirectClones(t)

	res := testutil.RunCommand(t, w.Root, nil, "setup")
	if res.Err != nil {
		t.Fatalf("setup: %v\nstderr: %s", res.Err, res.Stderr)
	}
	if _, err := os.Stat(filepath.Join(w.Root, "repos", "charts")); !os.IsNotExist(err) {
		t.Errorf("setup cloned hidden charts (stat err = %v)", err)
	}
}

func TestGroups_ShowHideAndReset(t *testing.T) {
	w := groupsWorkspace(t)
	w.RedirectClones(t)

	res := testutil.RunCommand(t, w.Root, nil, "group", "show", "platform")
	if res.Err != nil {
		t.Fatalf("group show: %v\nstderr: %s", res.Err, res.Stderr)
	}
	if _, err := os.Stat(filepath.Join(w.Root, "repos", "charts", ".ground")); err != nil {
		t.Errorf("group show did not clone charts: %v", err)
	}
	if _, ok := statusRepoNames(t, w)["infra"]; !ok {
		t.Error("status hides infra after group show")
	}

	if res := testutil.RunCommand(t, w.Root, nil, "group", "hide", "product"); res.Err != nil {
		t.Fatalf("group hide: %v", res.Err)
	}
	if _, ok := statusRepoNames(t, w)["frontend"]; ok {
		t.Error("status shows frontend after group hide product")
	}

	if res := testutil.RunCommand(t, w.Root, nil, "group", "reset", "platform"); res.Err != nil {
		t.Fatalf("group reset: %v", res.Err)
	}
	if _, ok := statusRepoNames(t, w)["infra"]; ok {
		t.Error("status shows infra after reset, want team default (hidden)")
	}

	res = testutil.RunCommand(t, w.Root, nil, "group")
	if res.Err != nil {
		t.Fatalf("group: %v", res.Err)
	}
	for _, want := range []string{"platform", "product", "charts, infra"} {
		if !strings.Contains(res.Stderr, want) {
			t.Errorf("group list missing %q:\n%s", want, res.Stderr)
		}
	}
}
func TestGroups_FailedFirstUseCloneLeavesNoDir(t *testing.T) {
	w := groupsWorkspace(t)
	// No RedirectClones: point git at a source that doesn't exist.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+filepath.Join(t.TempDir(), "missing")+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/test-org/charts.git")

	res := testutil.RunCommand(t, w.Root, nil, "rg", "--repo", "charts", "--roots")
	if res.Err == nil {
		t.Fatal("rg --repo charts succeeded, want clone failure")
	}
	if _, err := os.Stat(filepath.Join(w.Root, "repos", "charts")); !os.IsNotExist(err) {
		t.Errorf("repos/charts left behind after failed clone (stat err = %v)", err)
	}
}
