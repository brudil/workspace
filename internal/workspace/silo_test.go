package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(dir, "init")
	runGit(dir, "config", "user.email", "test@test.com")
	runGit(dir, "config", "user.name", "Test")
	return dir
}

func TestGitLsFiles(t *testing.T) {
	dir := initGitRepo(t)

	// Create a tracked file
	os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("hello"), 0644)
	runGit(dir, "add", "tracked.txt")
	runGit(dir, "commit", "-m", "add tracked")

	// Create an untracked file
	os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("world"), 0644)

	files, err := GitLsFiles(dir)
	if err != nil {
		t.Fatalf("GitLsFiles() error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %v", len(files), files)
	}
	if files[0] != "tracked.txt" {
		t.Errorf("files[0] = %q, want %q", files[0], "tracked.txt")
	}
}

func TestGitLsFiles_EmptyRepo(t *testing.T) {
	dir := initGitRepo(t)

	// Make an initial commit so HEAD exists but with no files
	// Actually, ls-files on a repo with no commits and no staged files returns empty
	files, err := GitLsFiles(dir)
	if err != nil {
		t.Fatalf("GitLsFiles() error: %v", err)
	}
	if files != nil {
		t.Errorf("expected nil for empty repo, got %v", files)
	}
}

func TestIsGitTracked(t *testing.T) {
	dir := initGitRepo(t)

	os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("world"), 0644)
	runGit(dir, "add", "tracked.txt")
	runGit(dir, "commit", "-m", "add tracked")

	if !IsGitTracked(dir, "tracked.txt") {
		t.Error("expected tracked.txt to be tracked")
	}
	if IsGitTracked(dir, "untracked.txt") {
		t.Error("expected untracked.txt to not be tracked")
	}
	if IsGitTracked(dir, "nonexistent.txt") {
		t.Error("expected nonexistent.txt to not be tracked")
	}
}

func TestFullSync(t *testing.T) {
	// Set up source repo with tracked files
	srcDir := initGitRepo(t)
	os.WriteFile(filepath.Join(srcDir, "file-a.txt"), []byte("aaa"), 0644)
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "sub", "file-b.txt"), []byte("bbb"), 0644)
	os.WriteFile(filepath.Join(srcDir, "untracked-src.txt"), []byte("not tracked"), 0644)
	runGit(srcDir, "add", "file-a.txt", "sub/file-b.txt")
	runGit(srcDir, "commit", "-m", "add files")

	// Destination is a plain directory (silo is a detached worktree, but for
	// testing we just need a directory — FullSync uses a manifest, not git)
	dstDir := t.TempDir()

	// Add untracked node_modules in dest (should be preserved)
	os.MkdirAll(filepath.Join(dstDir, "node_modules", "pkg"), 0755)
	os.WriteFile(filepath.Join(dstDir, "node_modules", "pkg", "index.js"), []byte("module"), 0644)

	// First sync
	if err := FullSync(srcDir, dstDir); err != nil {
		t.Fatalf("FullSync() error: %v", err)
	}

	// Verify tracked source files were copied
	data, err := os.ReadFile(filepath.Join(dstDir, "file-a.txt"))
	if err != nil {
		t.Fatalf("file-a.txt not found in dest: %v", err)
	}
	if string(data) != "aaa" {
		t.Errorf("file-a.txt content = %q, want %q", string(data), "aaa")
	}

	data, err = os.ReadFile(filepath.Join(dstDir, "sub", "file-b.txt"))
	if err != nil {
		t.Fatalf("sub/file-b.txt not found in dest: %v", err)
	}
	if string(data) != "bbb" {
		t.Errorf("sub/file-b.txt content = %q, want %q", string(data), "bbb")
	}

	// Verify node_modules preserved (untracked)
	if _, err := os.Stat(filepath.Join(dstDir, "node_modules", "pkg", "index.js")); err != nil {
		t.Error("node_modules should be preserved (untracked)")
	}

	// Verify untracked source file was NOT copied
	if _, err := os.Stat(filepath.Join(dstDir, "untracked-src.txt")); !os.IsNotExist(err) {
		t.Error("untracked-src.txt should not have been copied to dest")
	}

	// Verify manifest was written
	manifest := readManifest(dstDir)
	if len(manifest) != 2 {
		t.Fatalf("manifest has %d entries, want 2: %v", len(manifest), manifest)
	}
}

func TestFullSync_CleansUpOldFiles(t *testing.T) {
	// Source repo v1: has file-a.txt and file-b.txt
	srcDir := initGitRepo(t)
	os.WriteFile(filepath.Join(srcDir, "file-a.txt"), []byte("aaa"), 0644)
	os.WriteFile(filepath.Join(srcDir, "file-b.txt"), []byte("bbb"), 0644)
	runGit(srcDir, "add", ".")
	runGit(srcDir, "commit", "-m", "v1")

	dstDir := t.TempDir()

	// Sync v1
	if err := FullSync(srcDir, dstDir); err != nil {
		t.Fatalf("FullSync v1 error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "file-a.txt")); err != nil {
		t.Fatal("file-a.txt should exist after v1 sync")
	}
	if _, err := os.Stat(filepath.Join(dstDir, "file-b.txt")); err != nil {
		t.Fatal("file-b.txt should exist after v1 sync")
	}

	// Source repo v2: remove file-b.txt, add file-c.txt
	os.Remove(filepath.Join(srcDir, "file-b.txt"))
	os.WriteFile(filepath.Join(srcDir, "file-c.txt"), []byte("ccc"), 0644)
	runGit(srcDir, "add", ".")
	runGit(srcDir, "commit", "-m", "v2")

	// Sync v2
	if err := FullSync(srcDir, dstDir); err != nil {
		t.Fatalf("FullSync v2 error: %v", err)
	}

	// file-a.txt still there
	if _, err := os.Stat(filepath.Join(dstDir, "file-a.txt")); err != nil {
		t.Error("file-a.txt should still exist after v2 sync")
	}
	// file-b.txt removed (was in manifest from v1 but not in v2 source)
	if _, err := os.Stat(filepath.Join(dstDir, "file-b.txt")); !os.IsNotExist(err) {
		t.Error("file-b.txt should have been removed after v2 sync")
	}
	// file-c.txt added
	if _, err := os.Stat(filepath.Join(dstDir, "file-c.txt")); err != nil {
		t.Error("file-c.txt should exist after v2 sync")
	}
}

func TestSyncFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source file in a subdirectory
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "sub", "file.txt"), []byte("content"), 0644)

	if err := SyncFile(srcDir, dstDir, "sub/file.txt"); err != nil {
		t.Fatalf("SyncFile() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dstDir, "sub", "file.txt"))
	if err != nil {
		t.Fatalf("file not found in dest: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("content = %q, want %q", string(data), "content")
	}
}

func TestSyncFile_Overwrite(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("new"), 0644)
	os.WriteFile(filepath.Join(dstDir, "file.txt"), []byte("old"), 0644)

	if err := SyncFile(srcDir, dstDir, "file.txt"); err != nil {
		t.Fatalf("SyncFile() error: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dstDir, "file.txt"))
	if string(data) != "new" {
		t.Errorf("content = %q, want %q", string(data), "new")
	}
}

func TestRemoveSyncedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	os.WriteFile(path, []byte("data"), 0644)

	if err := RemoveSyncedFile(dir, "file.txt"); err != nil {
		t.Fatalf("RemoveSyncedFile() error: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should have been removed")
	}
}

// Ensure initGitRepo doesn't collide with initTestRepo from git_test.go
// (they're in the same package, but have different names so it's fine)

func TestGitLsFiles_MultipleFiles(t *testing.T) {
	dir := initGitRepo(t)

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c"), 0644)
	runGit(dir, "add", ".")
	runGit(dir, "commit", "-m", "add files")

	files, err := GitLsFiles(dir)
	if err != nil {
		t.Fatalf("GitLsFiles() error: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3: %v", len(files), files)
	}
}

// Verify that .silo is excluded from ListWorktrees (same as .ground/.bare)
func TestListWorktrees_SkipsSilo(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".silo"), 0755)
	os.MkdirAll(filepath.Join(dir, "feature-x"), 0755)

	names, err := ListWorktrees(dir)
	if err != nil {
		t.Fatalf("ListWorktrees() error: %v", err)
	}
	if len(names) != 1 || names[0] != "feature-x" {
		t.Errorf("ListWorktrees() = %v, want [feature-x]", names)
	}
}

// Ensure that git commands in tests don't fail (verify git is available)
func init() {
	if _, err := exec.LookPath("git"); err != nil {
		panic("git is required for silo tests")
	}
}
