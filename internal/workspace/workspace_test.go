package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAlias_CanonicalName(t *testing.T) {
	ws := &Workspace{
		RepoNames: []string{"repo-a", "repo-b"},
		AliasMap:  map[string]string{"ra": "repo-a"},
	}
	got, ok := ws.ResolveAlias("repo-a")
	if !ok || got != "repo-a" {
		t.Errorf("ResolveAlias(%q) = (%q, %v), want (%q, true)", "repo-a", got, ok, "repo-a")
	}
}

func TestResolveAlias_Alias(t *testing.T) {
	ws := &Workspace{
		RepoNames: []string{"repo-a", "repo-b"},
		AliasMap:  map[string]string{"ra": "repo-a"},
	}
	got, ok := ws.ResolveAlias("ra")
	if !ok || got != "repo-a" {
		t.Errorf("ResolveAlias(%q) = (%q, %v), want (%q, true)", "ra", got, ok, "repo-a")
	}
}

func TestResolveAlias_Unknown(t *testing.T) {
	ws := &Workspace{
		RepoNames: []string{"repo-a"},
		AliasMap:  map[string]string{},
	}
	_, ok := ws.ResolveAlias("nope")
	if ok {
		t.Error("ResolveAlias should return false for unknown input")
	}
}

func TestDisplayNameFor_WithDisplayName(t *testing.T) {
	ws := &Workspace{
		DisplayNames: map[string]string{"repo-a": "Repo A"},
	}
	got := ws.DisplayNameFor("repo-a")
	if got != "Repo A" {
		t.Errorf("DisplayNameFor = %q, want %q", got, "Repo A")
	}
}

func TestDisplayNameFor_FallsBackToCanonical(t *testing.T) {
	ws := &Workspace{
		DisplayNames: map[string]string{},
	}
	got := ws.DisplayNameFor("repo-a")
	if got != "repo-a" {
		t.Errorf("DisplayNameFor = %q, want %q", got, "repo-a")
	}
}

func TestFormatRepoName_WithDisplayName(t *testing.T) {
	ws := &Workspace{
		DisplayNames: map[string]string{"repo-a": "Repo A"},
	}
	got := ws.FormatRepoName("repo-a")
	want := "Repo A (repo-a)"
	if got != want {
		t.Errorf("FormatRepoName = %q, want %q", got, want)
	}
}

func TestFormatRepoName_WithoutDisplayName(t *testing.T) {
	ws := &Workspace{
		DisplayNames: map[string]string{},
	}
	got := ws.FormatRepoName("repo-a")
	if got != "repo-a" {
		t.Errorf("FormatRepoName = %q, want %q", got, "repo-a")
	}
}

func TestReposDir(t *testing.T) {
	ws := &Workspace{Root: "/home/user/myws"}
	want := "/home/user/myws/repos"
	if got := ws.ReposDir(); got != want {
		t.Errorf("ReposDir() = %q, want %q", got, want)
	}
}

func TestRepoDir(t *testing.T) {
	ws := &Workspace{Root: "/home/user/myws"}
	want := "/home/user/myws/repos/my-repo"
	if got := ws.RepoDir("my-repo"); got != want {
		t.Errorf("RepoDir() = %q, want %q", got, want)
	}
}

func TestMainWorktree(t *testing.T) {
	ws := &Workspace{Root: "/home/user/myws", DefaultBranch: "main"}
	want := "/home/user/myws/repos/my-repo/.ground"
	if got := ws.MainWorktree("my-repo"); got != want {
		t.Errorf("MainWorktree() = %q, want %q", got, want)
	}
}

func TestBareDir(t *testing.T) {
	ws := &Workspace{Root: "/home/user/myws", DefaultBranch: "main"}
	want := "/home/user/myws/repos/my-repo/.bare"
	if got := ws.BareDir("my-repo"); got != want {
		t.Errorf("BareDir() = %q, want %q", got, want)
	}
}

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		pattern, target string
		want            bool
	}{
		{"fnt", "frontend", true},
		{"fe", "frontend", true},
		{"FE", "frontend", true},       // case insensitive
		{"frontend", "frontend", true}, // exact
		{"xyz", "frontend", false},
		{"fez", "frontend", false}, // z not in order
		{"", "frontend", true},     // empty pattern matches everything
		{"bk", "backend", true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.target, func(t *testing.T) {
			got := FuzzyMatch(tt.pattern, tt.target)
			if got != tt.want {
				t.Errorf("FuzzyMatch(%q, %q) = %v, want %v", tt.pattern, tt.target, got, tt.want)
			}
		})
	}
}

func TestFuzzyMatchPositions(t *testing.T) {
	tests := []struct {
		pattern, target string
		want            []int
	}{
		{"fnt", "frontend", []int{0, 3, 4}},
		{"fe", "frontend", []int{0, 5}},
		{"FE", "frontend", []int{0, 5}}, // case insensitive
		{"xyz", "frontend", nil},
		{"", "frontend", nil}, // empty pattern = no positions
		{"go", "Go", []int{0, 1}},
		{"fil", "Filter: Local", []int{0, 1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.target, func(t *testing.T) {
			got := FuzzyMatchPositions(tt.pattern, tt.target)
			if len(got) != len(tt.want) {
				t.Fatalf("FuzzyMatchPositions(%q, %q) = %v, want %v", tt.pattern, tt.target, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("FuzzyMatchPositions(%q, %q)[%d] = %d, want %d", tt.pattern, tt.target, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFuzzyMatchRepos(t *testing.T) {
	ws := &Workspace{
		RepoNames: []string{"frontend", "backend", "infra-core"},
		AliasMap:  map[string]string{"fe": "frontend", "be": "backend"},
	}

	// Matches canonical name
	matches := ws.FuzzyMatchRepos("frt")
	if len(matches) != 1 || matches[0] != "frontend" {
		t.Errorf("FuzzyMatchRepos(frt) = %v, want [frontend]", matches)
	}

	// Matches via alias
	matches = ws.FuzzyMatchRepos("be")
	if len(matches) != 1 || matches[0] != "backend" {
		t.Errorf("FuzzyMatchRepos(be) = %v, want [backend]", matches)
	}

	// No match
	matches = ws.FuzzyMatchRepos("xyz")
	if len(matches) != 0 {
		t.Errorf("FuzzyMatchRepos(xyz) = %v, want []", matches)
	}

	// Multiple matches — "nd" subsequence-matches frontend and backend
	matches = ws.FuzzyMatchRepos("nd")
	if len(matches) != 2 {
		t.Errorf("FuzzyMatchRepos(nd) = %v, want 2 matches", matches)
	}
}

func TestTitle_WithName(t *testing.T) {
	ws := &Workspace{Name: "My Workspace", Org: "my-org"}
	if got := ws.Title(); got != "My Workspace" {
		t.Errorf("Title() = %q, want %q", got, "My Workspace")
	}
}

func TestTitle_FallsBackToOrg(t *testing.T) {
	ws := &Workspace{Org: "my-org"}
	if got := ws.Title(); got != "my-org" {
		t.Errorf("Title() = %q, want %q", got, "my-org")
	}
}

func TestIsBoarded(t *testing.T) {
	ws := &Workspace{
		Boarded: map[string][]string{
			"repo-a": {"main", "feature-x"},
		},
	}
	if !ws.IsBoarded("repo-a", "main") {
		t.Error("expected main to be boarded")
	}
	if !ws.IsBoarded("repo-a", "feature-x") {
		t.Error("expected feature-x to be boarded")
	}
	if ws.IsBoarded("repo-a", "other") {
		t.Error("expected other to not be boarded")
	}
	if ws.IsBoarded("repo-b", "main") {
		t.Error("expected unknown repo to not be boarded")
	}
}

func TestCapsuleName(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{"feature/my-feature", "my-feature"},
		{"my-feature", "my-feature"},
		{"a/b/c", "c"},
		{"trailing/", ""},
		{"/leading", "leading"},
	}
	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got := CapsuleName(tt.branch)
			if got != tt.want {
				t.Errorf("CapsuleName(%q) = %q, want %q", tt.branch, got, tt.want)
			}
		})
	}
}

func TestUniqueCapsuleName_NoCollision(t *testing.T) {
	dir := t.TempDir()
	got := UniqueCapsuleName(dir, "feature/my-feature")
	if got != "my-feature" {
		t.Errorf("UniqueCapsuleName = %q, want %q", got, "my-feature")
	}
}

func TestUniqueCapsuleName_OneCollision(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "my-feature"), 0755)
	got := UniqueCapsuleName(dir, "feature/my-feature")
	if got != "my-feature-2" {
		t.Errorf("UniqueCapsuleName = %q, want %q", got, "my-feature-2")
	}
}

func TestUniqueCapsuleName_TwoCollisions(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "my-feature"), 0755)
	os.Mkdir(filepath.Join(dir, "my-feature-2"), 0755)
	got := UniqueCapsuleName(dir, "feature/my-feature")
	if got != "my-feature-3" {
		t.Errorf("UniqueCapsuleName = %q, want %q", got, "my-feature-3")
	}
}
