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

func findCategory(cats []CheckCategory, name string) *CheckCategory {
	for i := range cats {
		if cats[i].Name == name {
			return &cats[i]
		}
	}
	return nil
}
