# Silo Sync Untracked Files Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make silo sync include all non-gitignored files, not just git-tracked files.

**Architecture:** Replace `git ls-files -z` (tracked only) with `git ls-files -z --cached --others --exclude-standard` (tracked + untracked non-ignored). Replace the `IsGitTracked` check in the watcher with an `IsGitIgnored` check using `git check-ignore -q`.

**Tech Stack:** Go, git CLI

**Spec:** `docs/superpowers/specs/2026-03-30-silo-sync-untracked-files-design.md`

---

## File Map

- Modify: `internal/workspace/silo.go` — change `GitLsFiles` → `GitSyncableFiles`, replace `IsGitTracked` → `IsGitIgnored`
- Modify: `internal/workspace/watcher.go:272` — flip predicate to use `IsGitIgnored`
- Modify: `internal/workspace/silo_test.go` — update all tests for new behavior

---

### Task 1: Replace `GitLsFiles` with `GitSyncableFiles`

**Files:**
- Modify: `internal/workspace/silo_test.go`
- Modify: `internal/workspace/silo.go`

- [ ] **Step 1: Update `TestGitLsFiles` to expect untracked files and rename**

Replace the existing `TestGitLsFiles` test in `internal/workspace/silo_test.go`:

```go
func TestGitSyncableFiles(t *testing.T) {
	dir := initGitRepo(t)

	// Create a tracked file
	os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("hello"), 0644)
	runGit(dir, "add", "tracked.txt")
	runGit(dir, "commit", "-m", "add tracked")

	// Create an untracked file (should now be included)
	os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("world"), 0644)

	// Create a gitignored file (should be excluded)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0644)
	runGit(dir, "add", ".gitignore")
	runGit(dir, "commit", "-m", "add gitignore")
	os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("secret"), 0644)

	files, err := GitSyncableFiles(dir)
	if err != nil {
		t.Fatalf("GitSyncableFiles() error: %v", err)
	}

	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}

	if !fileSet["tracked.txt"] {
		t.Error("expected tracked.txt in results")
	}
	if !fileSet["untracked.txt"] {
		t.Error("expected untracked.txt in results (untracked but not ignored)")
	}
	if !fileSet[".gitignore"] {
		t.Error("expected .gitignore in results")
	}
	if fileSet["ignored.txt"] {
		t.Error("expected ignored.txt to be excluded (gitignored)")
	}
}
```

- [ ] **Step 2: Update `TestGitLsFiles_MultipleFiles` to use new name**

Rename the test and update the function call in `internal/workspace/silo_test.go`:

```go
func TestGitSyncableFiles_MultipleFiles(t *testing.T) {
	dir := initGitRepo(t)

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c"), 0644)
	runGit(dir, "add", ".")
	runGit(dir, "commit", "-m", "add files")

	files, err := GitSyncableFiles(dir)
	if err != nil {
		t.Fatalf("GitSyncableFiles() error: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3: %v", len(files), files)
	}
}
```

- [ ] **Step 3: Update `TestGitLsFiles_EmptyRepo` to use new name**

```go
func TestGitSyncableFiles_EmptyRepo(t *testing.T) {
	dir := initGitRepo(t)

	files, err := GitSyncableFiles(dir)
	if err != nil {
		t.Fatalf("GitSyncableFiles() error: %v", err)
	}
	if files != nil {
		t.Errorf("expected nil for empty repo, got %v", files)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `just test`
Expected: FAIL — `GitSyncableFiles` is not defined.

- [ ] **Step 5: Rename `GitLsFiles` to `GitSyncableFiles` and add flags**

In `internal/workspace/silo.go`, replace the `GitLsFiles` function:

```go
// GitSyncableFiles returns all files eligible for silo sync in the given directory.
// This includes both git-tracked files and untracked files that are not gitignored.
// Uses -z for null-separated output to handle filenames with special characters.
func GitSyncableFiles(dir string) ([]string, error) {
	out, err := runGitOutput(dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimRight(out, "\x00")
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\x00"), nil
}
```

- [ ] **Step 6: Update `FullSync` to call `GitSyncableFiles`**

In `internal/workspace/silo.go`, update the `FullSync` function. Change line 43 from:

```go
srcFiles, err := GitLsFiles(srcDir)
```

to:

```go
srcFiles, err := GitSyncableFiles(srcDir)
```

Also update the doc comment on `FullSync` (line 36-41) from:

```go
// FullSync copies all git-tracked files from srcDir to dstDir.
```

to:

```go
// FullSync copies all syncable files (tracked + untracked non-ignored) from srcDir to dstDir.
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `just test`
Expected: PASS — all `GitSyncableFiles` tests pass. The `TestFullSync` test may still reference old behavior (we fix that in Task 3).

- [ ] **Step 8: Commit**

```bash
git add internal/workspace/silo.go internal/workspace/silo_test.go
git commit -m "feat(silo): sync all non-ignored files, not just tracked

Replace GitLsFiles (git ls-files -z) with GitSyncableFiles
(git ls-files -z --cached --others --exclude-standard) so that
untracked but non-ignored files are included in silo sync."
```

---

### Task 2: Replace `IsGitTracked` with `IsGitIgnored`

**Files:**
- Modify: `internal/workspace/silo_test.go`
- Modify: `internal/workspace/silo.go`
- Modify: `internal/workspace/watcher.go:272`

- [ ] **Step 1: Replace `TestIsGitTracked` with `TestIsGitIgnored`**

In `internal/workspace/silo_test.go`, replace the `TestIsGitTracked` function:

```go
func TestIsGitIgnored(t *testing.T) {
	dir := initGitRepo(t)

	// Set up gitignore
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0644)
	runGit(dir, "add", ".gitignore")
	runGit(dir, "commit", "-m", "add gitignore")

	// Create files
	os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("hello"), 0644)
	runGit(dir, "add", "tracked.txt")
	runGit(dir, "commit", "-m", "add tracked")
	os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("world"), 0644)
	os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("secret"), 0644)

	if IsGitIgnored(dir, "ignored.txt") != true {
		t.Error("expected ignored.txt to be ignored")
	}
	if IsGitIgnored(dir, "tracked.txt") != false {
		t.Error("expected tracked.txt to not be ignored")
	}
	if IsGitIgnored(dir, "untracked.txt") != false {
		t.Error("expected untracked.txt to not be ignored")
	}
	if IsGitIgnored(dir, "nonexistent.txt") != false {
		t.Error("expected nonexistent.txt to not be ignored")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test`
Expected: FAIL — `IsGitIgnored` is not defined.

- [ ] **Step 3: Replace `IsGitTracked` with `IsGitIgnored` in `silo.go`**

In `internal/workspace/silo.go`, replace the `IsGitTracked` function:

```go
// IsGitIgnored returns true if the given relative path is ignored by git in dir.
// Uses git check-ignore which respects .gitignore, .git/info/exclude, and global excludes.
func IsGitIgnored(dir, relPath string) bool {
	err := runGit(dir, "check-ignore", "-q", relPath)
	return err == nil
}
```

- [ ] **Step 4: Update the watcher to use `IsGitIgnored`**

In `internal/workspace/watcher.go`, replace lines 272-275:

From:
```go
		if !IsGitTracked(capsuleDir, relPath) {
			sw.verbose("%s: untracked, ignoring", relPath)
			return
		}
```

To:
```go
		if IsGitIgnored(capsuleDir, relPath) {
			sw.verbose("%s: gitignored, skipping", relPath)
			return
		}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `just test`
Expected: PASS — `TestIsGitIgnored` passes, watcher compiles.

- [ ] **Step 6: Commit**

```bash
git add internal/workspace/silo.go internal/workspace/silo_test.go internal/workspace/watcher.go
git commit -m "feat(silo): use IsGitIgnored instead of IsGitTracked in watcher

Replace IsGitTracked (git ls-files) with IsGitIgnored (git check-ignore -q)
so the watcher syncs untracked files that aren't gitignored."
```

---

### Task 3: Update `TestFullSync` for new behavior

**Files:**
- Modify: `internal/workspace/silo_test.go`

- [ ] **Step 1: Update `TestFullSync` to expect untracked files synced**

In `internal/workspace/silo_test.go`, replace the `TestFullSync` function:

```go
func TestFullSync(t *testing.T) {
	// Set up source repo with tracked files
	srcDir := initGitRepo(t)
	os.WriteFile(filepath.Join(srcDir, "file-a.txt"), []byte("aaa"), 0644)
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "sub", "file-b.txt"), []byte("bbb"), 0644)
	os.WriteFile(filepath.Join(srcDir, "untracked-src.txt"), []byte("not tracked"), 0644)

	// Create a gitignored file
	os.WriteFile(filepath.Join(srcDir, ".gitignore"), []byte("ignored.txt\n"), 0644)
	os.WriteFile(filepath.Join(srcDir, "ignored.txt"), []byte("secret"), 0644)
	runGit(srcDir, "add", "file-a.txt", "sub/file-b.txt", ".gitignore")
	runGit(srcDir, "commit", "-m", "add files")

	// Destination is a plain directory (silo is a detached worktree, but for
	// testing we just need a directory — FullSync uses a manifest, not git)
	dstDir := t.TempDir()

	// Add untracked node_modules in dest (should be preserved)
	os.MkdirAll(filepath.Join(dstDir, "node_modules", "pkg"), 0755)
	os.WriteFile(filepath.Join(dstDir, "node_modules", "pkg", "index.js"), []byte("module"), 0644)

	// First sync
	if _, err := FullSync(srcDir, dstDir); err != nil {
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

	// Verify node_modules preserved (untracked in dest)
	if _, err := os.Stat(filepath.Join(dstDir, "node_modules", "pkg", "index.js")); err != nil {
		t.Error("node_modules should be preserved (untracked in dest)")
	}

	// Verify untracked source file WAS copied (new behavior)
	data, err = os.ReadFile(filepath.Join(dstDir, "untracked-src.txt"))
	if err != nil {
		t.Fatalf("untracked-src.txt should have been copied to dest: %v", err)
	}
	if string(data) != "not tracked" {
		t.Errorf("untracked-src.txt content = %q, want %q", string(data), "not tracked")
	}

	// Verify gitignored file was NOT copied
	if _, err := os.Stat(filepath.Join(dstDir, "ignored.txt")); !os.IsNotExist(err) {
		t.Error("ignored.txt should not have been copied to dest (gitignored)")
	}

	// Verify manifest was written (file-a.txt, sub/file-b.txt, untracked-src.txt, .gitignore)
	manifest := readManifest(dstDir)
	if len(manifest) != 4 {
		t.Fatalf("manifest has %d entries, want 4: %v", len(manifest), manifest)
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `just test`
Expected: PASS — all tests pass with the new behavior from Tasks 1 and 2.

- [ ] **Step 3: Commit**

```bash
git add internal/workspace/silo_test.go
git commit -m "test(silo): update FullSync test for untracked file sync behavior

Untracked non-ignored files should now be synced. Gitignored files
should be excluded."
```

---

### Task 4: Final verification

- [ ] **Step 1: Run full test suite**

Run: `just test`
Expected: All tests pass.

- [ ] **Step 2: Build**

Run: `just build`
Expected: Build succeeds with no errors.

- [ ] **Step 3: Verify no references to old function names remain**

Search for `GitLsFiles` and `IsGitTracked` in the codebase. There should be zero results.

Run: `grep -r "GitLsFiles\|IsGitTracked" internal/`
Expected: No output.
