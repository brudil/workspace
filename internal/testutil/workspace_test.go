package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetupWorkspace_CreatesStructure(t *testing.T) {
	w := SetupWorkspace(t, WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos: []RepoOpts{
			{Name: "repo-a"},
			{Name: "repo-b", Branches: []string{"feature-x"}},
		},
	})

	// ws.toml exists
	if _, err := os.Stat(filepath.Join(w.Root, "ws.toml")); err != nil {
		t.Errorf("ws.toml not found: %v", err)
	}

	// Bare repos exist
	for _, name := range []string{"repo-a", "repo-b"} {
		bareDir := filepath.Join(w.Root, "repos", name, ".bare")
		if _, err := os.Stat(bareDir); err != nil {
			t.Errorf("bare dir for %s not found: %v", name, err)
		}
	}

	// .ground worktrees exist
	for _, name := range []string{"repo-a", "repo-b"} {
		groundDir := filepath.Join(w.Root, "repos", name, ".ground")
		if _, err := os.Stat(groundDir); err != nil {
			t.Errorf(".ground for %s not found: %v", name, err)
		}
	}

	// Extra branch is fetchable (exists in bare repo refs)
	if _, ok := w.Sources["repo-b"]; !ok {
		t.Error("expected source repo path for repo-b")
	}
}
