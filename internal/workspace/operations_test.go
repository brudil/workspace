package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureReposDir(t *testing.T) {
	root := t.TempDir()
	ws := &Workspace{Root: root}

	if err := ws.EnsureReposDir(); err != nil {
		t.Fatalf("EnsureReposDir() error: %v", err)
	}

	info, err := os.Stat(filepath.Join(root, "repos"))
	if err != nil {
		t.Fatalf("repos dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("repos is not a directory")
	}
}

func TestEnsureReposDir_AlreadyExists(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "repos"), 0755)
	ws := &Workspace{Root: root}

	if err := ws.EnsureReposDir(); err != nil {
		t.Fatalf("EnsureReposDir() should not error when dir exists: %v", err)
	}
}

func TestCheckRemoveWorktree_DefaultBranch(t *testing.T) {
	ws := &Workspace{
		Root:          t.TempDir(),
		DefaultBranch: "main",
	}

	_, err := ws.CheckRemoveWorktree("repo-a", "main")
	if err == nil {
		t.Error("expected error when removing default branch")
	}
}

func TestCheckRemoveWorktree_Ground(t *testing.T) {
	ws := &Workspace{
		Root:          t.TempDir(),
		DefaultBranch: "main",
	}

	_, err := ws.CheckRemoveWorktree("repo-a", ".ground")
	if err == nil {
		t.Error("expected error when removing .ground")
	}
}

func TestBoard(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "repos", "my-repo")
	os.MkdirAll(filepath.Join(repoDir, ".ground"), 0755)
	os.MkdirAll(filepath.Join(repoDir, "feature-x"), 0755)

	ws := &Workspace{
		Root:    root,
		Boarded: map[string][]string{},
	}

	if err := ws.Board("my-repo", ".ground"); err != nil {
		t.Fatalf("Board() error: %v", err)
	}
	if !ws.IsBoarded("my-repo", ".ground") {
		t.Error(".ground should be boarded")
	}

	if err := ws.Board("my-repo", "feature-x"); err != nil {
		t.Fatalf("Board() error: %v", err)
	}
	if len(ws.Boarded["my-repo"]) != 2 {
		t.Errorf("boarded count = %d, want 2", len(ws.Boarded["my-repo"]))
	}
}

func TestBoard_AlreadyBoarded(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "repos", "my-repo", ".ground"), 0755)

	ws := &Workspace{
		Root:    root,
		Boarded: map[string][]string{"my-repo": {".ground"}},
	}

	if err := ws.Board("my-repo", ".ground"); err != nil {
		t.Fatalf("Board() should not error for already-boarded: %v", err)
	}
	if len(ws.Boarded["my-repo"]) != 1 {
		t.Errorf("should not duplicate: count = %d", len(ws.Boarded["my-repo"]))
	}
}

func TestBoard_NonexistentCapsule(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "repos", "my-repo"), 0755)

	ws := &Workspace{
		Root:    root,
		Boarded: map[string][]string{},
	}

	if err := ws.Board("my-repo", "nonexistent"); err == nil {
		t.Error("expected error for nonexistent capsule")
	}
}

func TestUnboard(t *testing.T) {
	root := t.TempDir()
	ws := &Workspace{
		Root:    root,
		Boarded: map[string][]string{"my-repo": {".ground", "feature-x"}},
	}

	if err := ws.Unboard("my-repo", "feature-x"); err != nil {
		t.Fatalf("Unboard() error: %v", err)
	}
	if ws.IsBoarded("my-repo", "feature-x") {
		t.Error("feature-x should not be boarded")
	}
	if !ws.IsBoarded("my-repo", ".ground") {
		t.Error(".ground should still be boarded")
	}
}

func TestUnboard_NotBoarded(t *testing.T) {
	ws := &Workspace{
		Boarded: map[string][]string{"my-repo": {".ground"}},
	}

	if err := ws.Unboard("my-repo", "other"); err == nil {
		t.Error("expected error for unboarding non-boarded capsule")
	}
}

func TestCopyFromGround_AllFilesExist(t *testing.T) {
	tmp := t.TempDir()
	groundDir := filepath.Join(tmp, ".ground")
	capsuleDir := filepath.Join(tmp, "my-feature")
	os.MkdirAll(groundDir, 0755)
	os.MkdirAll(capsuleDir, 0755)

	os.WriteFile(filepath.Join(groundDir, ".env"), []byte("SECRET=abc"), 0644)

	skipped, err := CopyFromGround(groundDir, capsuleDir, []string{".env"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want empty", skipped)
	}

	got, err := os.ReadFile(filepath.Join(capsuleDir, ".env"))
	if err != nil {
		t.Fatalf("reading copied file: %v", err)
	}
	if string(got) != "SECRET=abc" {
		t.Errorf("content = %q, want %q", string(got), "SECRET=abc")
	}
}

func TestCopyFromGround_NestedDirs(t *testing.T) {
	tmp := t.TempDir()
	groundDir := filepath.Join(tmp, ".ground")
	capsuleDir := filepath.Join(tmp, "my-feature")
	os.MkdirAll(filepath.Join(groundDir, "config"), 0755)
	os.MkdirAll(capsuleDir, 0755)

	os.WriteFile(filepath.Join(groundDir, "config", "local.yaml"), []byte("key: val"), 0644)

	skipped, err := CopyFromGround(groundDir, capsuleDir, []string{"config/local.yaml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want empty", skipped)
	}

	got, err := os.ReadFile(filepath.Join(capsuleDir, "config", "local.yaml"))
	if err != nil {
		t.Fatalf("reading copied file: %v", err)
	}
	if string(got) != "key: val" {
		t.Errorf("content = %q, want %q", string(got), "key: val")
	}
}

func TestCopyFromGround_SomeMissing(t *testing.T) {
	tmp := t.TempDir()
	groundDir := filepath.Join(tmp, ".ground")
	capsuleDir := filepath.Join(tmp, "my-feature")
	os.MkdirAll(groundDir, 0755)
	os.MkdirAll(capsuleDir, 0755)

	os.WriteFile(filepath.Join(groundDir, ".env"), []byte("SECRET=abc"), 0644)

	skipped, err := CopyFromGround(groundDir, capsuleDir, []string{".env", "missing.txt", "also-missing.conf"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped count = %d, want 2", len(skipped))
	}
	if skipped[0] != "missing.txt" || skipped[1] != "also-missing.conf" {
		t.Errorf("skipped = %v, want [missing.txt also-missing.conf]", skipped)
	}

	// .env should still have been copied
	if _, err := os.Stat(filepath.Join(capsuleDir, ".env")); err != nil {
		t.Error("expected .env to be copied despite other missing files")
	}
}

func TestCopyFromGround_EmptyList(t *testing.T) {
	tmp := t.TempDir()
	groundDir := filepath.Join(tmp, ".ground")
	capsuleDir := filepath.Join(tmp, "my-feature")
	os.MkdirAll(groundDir, 0755)
	os.MkdirAll(capsuleDir, 0755)

	skipped, err := CopyFromGround(groundDir, capsuleDir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want empty", skipped)
	}
}
