package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func setupJumpWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	content := `[workspace]
org = "test-org"
default_branch = "main"

[repos.frontend]
aliases = ["fe"]

[repos.backend]
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)

	// Create repo dirs with worktrees
	for _, repo := range []string{"frontend", "backend"} {
		os.MkdirAll(filepath.Join(root, "repos", repo, "main"), 0755)
	}

	// Add a feature worktree to frontend
	os.MkdirAll(filepath.Join(root, "repos", "frontend", "feature-x"), 0755)

	return root
}

func TestResolveJumpPath_TildeReturnsRoot(t *testing.T) {
	root := setupJumpWorkspace(t)
	ctx, err := LoadContextFromDir(root)
	if err != nil {
		t.Fatal(err)
	}

	got, err := resolveJumpPath(ctx, "~", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != root {
		t.Errorf("got %q, want %q", got, root)
	}
}

func TestResolveJumpPath_RepoAndWorktree(t *testing.T) {
	root := setupJumpWorkspace(t)
	ctx, err := LoadContextFromDir(root)
	if err != nil {
		t.Fatal(err)
	}

	got, err := resolveJumpPath(ctx, "frontend", "feature-x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, "repos", "frontend", "feature-x")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveJumpPath_AliasResolution(t *testing.T) {
	root := setupJumpWorkspace(t)
	ctx, err := LoadContextFromDir(root)
	if err != nil {
		t.Fatal(err)
	}

	got, err := resolveJumpPath(ctx, "fe", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, "repos", "frontend", "main")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveJumpPath_NonexistentWorktree(t *testing.T) {
	root := setupJumpWorkspace(t)
	ctx, err := LoadContextFromDir(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = resolveJumpPath(ctx, "frontend", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent worktree")
	}
}

func TestResolveJumpPath_UnknownRepo(t *testing.T) {
	root := setupJumpWorkspace(t)
	ctx, err := LoadContextFromDir(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = resolveJumpPath(ctx, "unknown-repo", "main")
	if err == nil {
		t.Error("expected error for unknown repo")
	}
}

func TestResolveJumpPath_FuzzyRepoMatch(t *testing.T) {
	root := setupJumpWorkspace(t)
	ctx, err := LoadContextFromDir(root)
	if err != nil {
		t.Fatal(err)
	}

	// "fnt" should fuzzy match "frontend"
	got, err := resolveJumpPath(ctx, "fnt", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, "repos", "frontend", "main")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveJumpPath_FuzzyWorktreeMatch(t *testing.T) {
	root := setupJumpWorkspace(t)
	ctx, err := LoadContextFromDir(root)
	if err != nil {
		t.Fatal(err)
	}

	// "fx" should fuzzy match "feature-x"
	got, err := resolveJumpPath(ctx, "frontend", "fx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, "repos", "frontend", "feature-x")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveJumpPath_FuzzyNoMatch(t *testing.T) {
	root := setupJumpWorkspace(t)
	ctx, err := LoadContextFromDir(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = resolveJumpPath(ctx, "xyz", "main")
	if err == nil {
		t.Error("expected error for no fuzzy match")
	}
}
