package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDoctor_AllHealthy(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "repos", "repo-a")
	bareDir := filepath.Join(repoDir, ".bare")
	os.MkdirAll(bareDir, 0755)
	os.MkdirAll(filepath.Join(repoDir, "main"), 0755)
	ws := &Workspace{
		Root:          root,
		DefaultBranch: "main",
		RepoNames:     []string{"repo-a"},
	}

	orig := CheckGitHubAuthFunc
	CheckGitHubAuthFunc = func() CheckResult {
		return CheckResult{Name: "gh auth", Status: CheckOK}
	}
	t.Cleanup(func() { CheckGitHubAuthFunc = orig })

	categories := ws.Doctor()
	for _, cat := range categories {
		for _, check := range cat.Checks {
			if check.Status != CheckOK {
				t.Errorf("category %q check %q: got status %d, want CheckOK", cat.Name, check.Name, check.Status)
			}
		}
	}
}

func TestDoctor_RepoNotCloned(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "repos"), 0755)

	ws := &Workspace{
		Root:          root,
		DefaultBranch: "main",
		RepoNames:     []string{"missing-repo"},
	}

	categories := ws.Doctor()
	reposCat := findCategory(categories, "Repos")
	if reposCat == nil {
		t.Fatal("missing Repos category")
	}
	if len(reposCat.Checks) != 1 || reposCat.Checks[0].Status != CheckFail {
		t.Errorf("expected CheckFail for missing repo, got %+v", reposCat.Checks)
	}
}

func TestCheckResult_HasFixFields(t *testing.T) {
	called := false
	r := CheckResult{
		Name:    "test",
		Status:  CheckFail,
		Detail:  "broken",
		Fix:     func() error { called = true; return nil },
		FixHint: "try this",
	}

	if r.Fix == nil {
		t.Fatal("Fix should not be nil")
	}
	if err := r.Fix(); err != nil {
		t.Fatalf("Fix returned error: %v", err)
	}
	if !called {
		t.Fatal("Fix was not called")
	}
	if r.FixHint != "try this" {
		t.Errorf("FixHint = %q, want %q", r.FixHint, "try this")
	}
}

func TestCheckRepos_MissingRepo_HasFix(t *testing.T) {
	root := t.TempDir()
	reposDir := filepath.Join(root, "repos")
	os.MkdirAll(filepath.Join(reposDir, "exists", ".bare"), 0755)
	os.MkdirAll(filepath.Join(reposDir, "missing"), 0755)

	w := &Workspace{
		Root:      root,
		RepoNames: []string{"exists", "missing"},
	}

	cat := w.checkRepos()

	for _, check := range cat.Checks {
		if check.Name == "exists" {
			if check.Status != CheckOK {
				t.Errorf("exists: status = %d, want CheckOK", check.Status)
			}
			if check.Fix != nil {
				t.Error("exists: Fix should be nil for healthy repo")
			}
		}
		if check.Name == "missing" {
			if check.Status != CheckFail {
				t.Errorf("missing: status = %d, want CheckFail", check.Status)
			}
			if check.Fix == nil {
				t.Error("missing: Fix should not be nil for missing repo")
			}
		}
	}
}

func TestCheckTools_MissingGh_HasHint(t *testing.T) {
	w := &Workspace{}

	// Temporarily break PATH to ensure gh isn't found
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", "")
	defer os.Setenv("PATH", origPath)

	cat := w.checkTools()

	if len(cat.Checks) < 1 {
		t.Fatalf("expected at least 1 check, got %d", len(cat.Checks))
	}
	check := cat.Checks[0]
	if check.Status != CheckFail {
		t.Errorf("status = %d, want CheckFail", check.Status)
	}
	if check.FixHint == "" {
		t.Error("expected FixHint for missing gh tool")
	}
}

func findCategory(cats []CheckCategory, name string) *CheckCategory {
	for i := range cats {
		if cats[i].Name == name {
			return &cats[i]
		}
	}
	return nil
}
