# Silo Sync: Include Untracked Non-Ignored Files

**Date:** 2026-03-30
**Status:** Approved

## Problem

Silo sync currently uses `git ls-files -z` to determine which files to sync. This only returns files in the git index (tracked files). New files that haven't been `git add`ed are silently excluded from sync, even though they aren't gitignored. This is a poor DX — users expect all non-ignored files to sync.

The incremental watcher has the same issue: `IsGitTracked()` skips untracked files on change events.

## Solution

**Approach A: `git ls-files --cached --others --exclude-standard`**

Use git's built-in flags to return both tracked and untracked-but-not-ignored files. Replace the tracked-file check in the watcher with an ignored-file check using `git check-ignore`.

## Changes

### `silo.go`

1. **`GitLsFiles` → `GitSyncableFiles`**: Change command from `git ls-files -z` to `git ls-files -z --cached --others --exclude-standard`. Returns tracked files + untracked non-ignored files in one call. `--exclude-standard` respects `.gitignore`, `.git/info/exclude`, and global excludes.

2. **`IsGitTracked` → `IsGitIgnored`**: Replace with `git check-ignore -q <path>`. Exit code 0 = ignored, non-zero = not ignored. Inverts the semantics.

3. **`FullSync`**: Update call from `GitLsFiles` to `GitSyncableFiles`. No other changes — it already iterates a file list and copies.

### `watcher.go`

4. **Line 272**: Flip predicate from `!IsGitTracked(capsuleDir, relPath)` to `IsGitIgnored(capsuleDir, relPath)`. Same skip behavior, opposite check.

### `silo_test.go`

5. **Update `TestGitLsFiles`**: Rename to `TestGitSyncableFiles`. Untracked-but-not-ignored files should now appear in results. Add a gitignored file and verify it's excluded.

6. **Update `TestIsGitTracked`**: Replace with `TestIsGitIgnored`. Verify that gitignored files return true, non-ignored files return false.

7. **Update `TestFullSync`**: The existing assertion that `untracked-src.txt` is NOT copied should flip — it should now be copied. Add a gitignored file and verify it's excluded.

## What doesn't change

- `FullSync` copy/delete logic, `SyncFile`, `RemoveSyncedFile` — file operations are agnostic to how the list is built
- Manifest logic — still tracks synced files for cleanup
- Watcher directory walking, debouncing, git index watching
- `.gitignore` changes are only picked up on manual resync (not reactively)

## Edge cases

- **`git ls-files` index refresh**: `--cached --others` still reads the index, so `git ls-files` will still refresh index stat cache as a side effect. The existing 2-second suppression window in `fullResync` handles this.
- **Duplicate entries**: `git ls-files --cached --others` does not produce duplicates for files that exist on disk — `--others` only lists files not in the index, and `--cached` only lists files in the index. No deduplication needed.
