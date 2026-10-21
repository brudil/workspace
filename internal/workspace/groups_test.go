package workspace

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// groupWorkspace has two visible repos and two in the hidden platform group:
// infra (ground only) and deploy (with a capsule).
func groupWorkspace(t *testing.T) *Workspace {
	t.Helper()
	ws := searchWorkspace(t, map[string][]string{
		"frontend": {".bare", ".ground"},
		"backend":  {".bare", ".ground"},
		"infra":    {".bare", ".ground"},
		"deploy":   {".bare", ".ground", "bump-chart"},
	})
	ws.RepoGroups = map[string]string{
		"frontend": "product",
		"infra":    "platform",
		"deploy":   "platform",
	}
	ws.HiddenGroups = map[string]bool{"platform": true}
	return ws
}

func TestDefaultRepoNames_SkipsHiddenGroups(t *testing.T) {
	ws := groupWorkspace(t)
	if got, want := ws.DefaultRepoNames(), []string{"backend", "frontend"}; !slices.Equal(got, want) {
		t.Errorf("DefaultRepoNames = %v, want %v", got, want)
	}
}

func TestListedRepoNames_AddsHiddenReposWithCapsules(t *testing.T) {
	ws := groupWorkspace(t)
	if got, want := ws.ListedRepoNames(), []string{"backend", "deploy", "frontend"}; !slices.Equal(got, want) {
		t.Errorf("ListedRepoNames = %v, want %v", got, want)
	}
}

func TestSelectGroups_NarrowsToGroupsHiddenOrNot(t *testing.T) {
	ws := groupWorkspace(t)
	if err := ws.SelectGroups([]string{"platform", "product"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"deploy", "frontend", "infra"}
	if got := ws.DefaultRepoNames(); !slices.Equal(got, want) {
		t.Errorf("DefaultRepoNames = %v, want %v", got, want)
	}
	if got := ws.ListedRepoNames(); !slices.Equal(got, want) {
		t.Errorf("ListedRepoNames = %v, want %v", got, want)
	}
}

func TestSelectGroups_UnknownGroup(t *testing.T) {
	ws := groupWorkspace(t)
	err := ws.SelectGroups([]string{"nope"})
	if err == nil || !strings.Contains(err.Error(), "platform, product") {
		t.Fatalf("err = %v, want unknown group listing known groups", err)
	}
}

func TestFuzzyMatchRepos_HiddenOnlyWhenNoVisibleMatch(t *testing.T) {
	ws := groupWorkspace(t)
	// "e" matches frontend and backend, so hidden deploy is never considered.
	if got := ws.FuzzyMatchRepos("e"); slices.Contains(got, "deploy") {
		t.Errorf("FuzzyMatchRepos(e) = %v, want hidden deploy left out", got)
	}
	if got, want := ws.FuzzyMatchRepos("inf"), []string{"infra"}; !slices.Equal(got, want) {
		t.Errorf("FuzzyMatchRepos(inf) = %v, want %v", got, want)
	}
}

func TestResolveAlias_FindsHiddenRepos(t *testing.T) {
	ws := groupWorkspace(t)
	if got, ok := ws.ResolveAlias("infra"); !ok || got != "infra" {
		t.Errorf("ResolveAlias(infra) = %q, %v; want infra, true", got, ok)
	}
}

func TestSearchRoots_SkipsHiddenUnlessStandingInIt(t *testing.T) {
	ws := groupWorkspace(t)

	roots, err := ws.SearchRoots(ws.Root, nil, ScopeHere)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rels(roots), "repos/backend/.ground repos/frontend/.ground"; got != want {
		t.Errorf("roots = %q, want %q", got, want)
	}

	cwd := filepath.Join(ws.Root, "repos", "infra", ".ground")
	roots, err = ws.SearchRoots(cwd, nil, ScopeHere)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rels(roots), "repos/backend/.ground repos/frontend/.ground repos/infra/.ground"; got != want {
		t.Errorf("roots = %q, want %q", got, want)
	}
}

func TestSearchRoots_ExplicitHiddenRepo(t *testing.T) {
	ws := groupWorkspace(t)
	roots, err := ws.SearchRoots(ws.Root, []RepoSelector{{Repo: "infra"}}, ScopeHere)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rels(roots), "repos/infra/.ground"; got != want {
		t.Errorf("roots = %q, want %q", got, want)
	}
}
