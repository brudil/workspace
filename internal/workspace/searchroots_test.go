package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// searchWorkspace builds a bare-directory workspace layout (no git needed):
// repos/<repo>/<worktree> for every entry in layout.
func searchWorkspace(t *testing.T, layout map[string][]string) *Workspace {
	t.Helper()
	root := t.TempDir()
	var names []string
	for repo, wts := range layout {
		names = append(names, repo)
		for _, wt := range wts {
			if err := os.MkdirAll(filepath.Join(root, "repos", repo, wt), 0755); err != nil {
				t.Fatal(err)
			}
		}
	}
	// RepoNames must be sorted, as LoadContextFromDir guarantees.
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return &Workspace{
		Root:      root,
		RepoNames: names,
		AliasMap:  map[string]string{"fe": "frontend", "api": "backend"},
	}
}

func rels(roots []SearchRoot) string {
	parts := make([]string, len(roots))
	for i, r := range roots {
		parts[i] = r.RelPath()
	}
	return strings.Join(parts, " ")
}

func TestSearchRoots_DefaultIsGroundEverywhereOutsideWorktrees(t *testing.T) {
	ws := searchWorkspace(t, map[string][]string{
		"frontend": {".bare", ".ground", ".silo", "login-fix"},
		"backend":  {".bare", ".ground", "add-health"},
	})

	roots, err := ws.SearchRoots(ws.Root, nil, ScopeHere)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rels(roots), "repos/backend/.ground repos/frontend/.ground"; got != want {
		t.Errorf("roots = %q, want %q", got, want)
	}
	for _, r := range roots {
		if !filepath.IsAbs(r.Path) {
			t.Errorf("Path %q is not absolute", r.Path)
		}
	}
}

func TestSearchRoots_HereUsesCurrentCapsuleForItsRepoOnly(t *testing.T) {
	ws := searchWorkspace(t, map[string][]string{
		"frontend": {".bare", ".ground", "login-fix"},
		"backend":  {".bare", ".ground", "add-health"},
	})
	cwd := filepath.Join(ws.Root, "repos", "frontend", "login-fix", "src", "deep")

	roots, err := ws.SearchRoots(cwd, nil, ScopeHere)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rels(roots), "repos/backend/.ground repos/frontend/login-fix"; got != want {
		t.Errorf("roots = %q, want %q", got, want)
	}
}

func TestSearchRoots_HereInsideGroundIsGround(t *testing.T) {
	ws := searchWorkspace(t, map[string][]string{
		"frontend": {".ground", "login-fix"},
	})
	cwd := filepath.Join(ws.Root, "repos", "frontend", ".ground")

	roots, err := ws.SearchRoots(cwd, nil, ScopeHere)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rels(roots), "repos/frontend/.ground"; got != want {
		t.Errorf("roots = %q, want %q", got, want)
	}
}

func TestSearchRoots_HereInsideSiloOrBareFallsBackToGround(t *testing.T) {
	ws := searchWorkspace(t, map[string][]string{
		"frontend": {".bare", ".ground", ".silo"},
	})
	for _, wt := range []string{".silo", ".bare"} {
		cwd := filepath.Join(ws.Root, "repos", "frontend", wt)
		roots, err := ws.SearchRoots(cwd, nil, ScopeHere)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := rels(roots), "repos/frontend/.ground"; got != want {
			t.Errorf("from %s: roots = %q, want %q", wt, got, want)
		}
	}
}

func TestSearchRoots_GroundOverridesHere(t *testing.T) {
	ws := searchWorkspace(t, map[string][]string{
		"frontend": {".ground", "login-fix"},
	})
	cwd := filepath.Join(ws.Root, "repos", "frontend", "login-fix")

	roots, err := ws.SearchRoots(cwd, nil, ScopeGround)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rels(roots), "repos/frontend/.ground"; got != want {
		t.Errorf("roots = %q, want %q", got, want)
	}
}

func TestSearchRoots_AllIncludesCapsulesButNeverBareOrSilo(t *testing.T) {
	ws := searchWorkspace(t, map[string][]string{
		"frontend": {".bare", ".ground", ".silo", "zeta", "alpha"},
		"backend":  {".bare", ".ground"},
	})

	roots, err := ws.SearchRoots(ws.Root, nil, ScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	want := "repos/backend/.ground repos/frontend/.ground repos/frontend/alpha repos/frontend/zeta"
	if got := rels(roots); got != want {
		t.Errorf("roots = %q, want %q", got, want)
	}
}

func TestSearchRoots_SelectorsRestrictRepos(t *testing.T) {
	ws := searchWorkspace(t, map[string][]string{
		"frontend": {".ground", "login-fix"},
		"backend":  {".ground"},
		"infra":    {".ground"},
	})

	roots, err := ws.SearchRoots(ws.Root, []RepoSelector{{Repo: "frontend"}}, ScopeHere)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rels(roots), "repos/frontend/.ground"; got != want {
		t.Errorf("roots = %q, want %q", got, want)
	}
}

func TestSearchRoots_ExplicitWorktreeWinsOverMode(t *testing.T) {
	ws := searchWorkspace(t, map[string][]string{
		"frontend": {".ground", ".silo", "login-fix", "other"},
		"backend":  {".ground", "add-health"},
	})

	sel := []RepoSelector{
		{Repo: "frontend", Worktree: "login-fix"},
		{Repo: "frontend", Worktree: ".silo"},
		{Repo: "backend"},
	}
	roots, err := ws.SearchRoots(ws.Root, sel, ScopeGround)
	if err != nil {
		t.Fatal(err)
	}
	want := "repos/backend/.ground repos/frontend/.silo repos/frontend/login-fix"
	if got := rels(roots); got != want {
		t.Errorf("roots = %q, want %q", got, want)
	}
}

func TestSearchRoots_ExplicitMissingWorktreeIsError(t *testing.T) {
	ws := searchWorkspace(t, map[string][]string{
		"frontend": {".ground"},
	})

	_, err := ws.SearchRoots(ws.Root, []RepoSelector{{Repo: "frontend", Worktree: "nope"}}, ScopeHere)
	if err == nil || !strings.Contains(err.Error(), "frontend/nope") {
		t.Errorf("err = %v, want missing worktree error", err)
	}
}

func TestSearchRoots_UnknownRepoIsError(t *testing.T) {
	ws := searchWorkspace(t, map[string][]string{"frontend": {".ground"}})

	_, err := ws.SearchRoots(ws.Root, []RepoSelector{{Repo: "nope"}}, ScopeHere)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("err = %v, want unknown repo error", err)
	}
}

func TestSearchRoots_ImplicitRepoWithoutGroundIsSkipped(t *testing.T) {
	ws := searchWorkspace(t, map[string][]string{
		"frontend": {".ground"},
		"backend":  {".bare"}, // cloned but no ground yet
	})

	roots, err := ws.SearchRoots(ws.Root, nil, ScopeHere)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rels(roots), "repos/frontend/.ground"; got != want {
		t.Errorf("roots = %q, want %q", got, want)
	}

	_, err = ws.SearchRoots(ws.Root, []RepoSelector{{Repo: "backend"}}, ScopeGround)
	if err == nil {
		t.Error("explicitly selecting a repo without .ground should error")
	}
}

func TestSearchRoots_DuplicateSelectorsCollapse(t *testing.T) {
	ws := searchWorkspace(t, map[string][]string{"frontend": {".ground", "a"}})

	sel := []RepoSelector{{Repo: "frontend"}, {Repo: "frontend"}, {Repo: "frontend", Worktree: ".ground"}}
	roots, err := ws.SearchRoots(ws.Root, sel, ScopeGround)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rels(roots), "repos/frontend/.ground"; got != want {
		t.Errorf("roots = %q, want %q", got, want)
	}
}
