# Design: `ws doctor --fix`

GitHub issue: #1 — doctor identifies issues but no recourse to fix.

## Principle

**Safe actions are automated; destructive actions are hints.**

`--fix` never deletes data. It only performs constructive operations (cloning repos, creating worktrees). Anything destructive is surfaced as a hint telling the user what to run manually.

## Data Model

Add two fields to `CheckResult`:

```go
type CheckResult struct {
    Name    string
    Status  CheckStatus
    Detail  string
    Fix     func() error  // nil if not fixable
    FixHint string        // manual action suggestion when Fix is nil
}
```

- `Fix` — closure that repairs the issue. Only set for safe, constructive operations.
- `FixHint` — human-readable suggestion for issues that can't be auto-fixed.

## Check Behavior

| Check | Without `--fix` | With `--fix` |
|-------|----------------|-------------|
| Repo not cloned | `✗ d — not cloned (expected ...)` | Calls `SetupRepo(name)` → `✓ d → cloned` |
| Orphaned worktree dir | `⚠ name — orphaned` | Hint: `rm -rf repos/x/name, then re-run ws doctor --fix` |
| `gh` not on PATH | `✗ gh — not found` | Hint: `install with: brew install gh` |

## CLI Changes

- Add `--fix` bool flag to the `doctor` command.
- When `--fix` is set and a check has `Fix != nil`: call the fix, update status/detail.
- When `--fix` is set and a check has `FixHint != ""`: print the hint below the check line.
- Without `--fix`, behavior is unchanged.

## Example Output

```
❯ ws doctor --fix
Repos
  ✓ a
  ✓ b
  ✓ c
  ✓ d → cloned
  ✓ e

Worktrees
  ⚠ d — orphaned directory
    hint: rm -rf repos/d/my-capsule, then re-run ws doctor --fix

Tools
  ✗ gh — not found on PATH
    hint: install with: brew install gh
```

## Files to Change

- `internal/workspace/doctor.go` — add `Fix`/`FixHint` to `CheckResult`, attach fix closures in `checkRepos()`, add hints in `checkOrphanedWorktrees()` and `checkTools()`.
- `internal/cli/doctor.go` — add `--fix` flag, execute fixes and print hints in `printDoctorResults`.
- `internal/cli/integration_test.go` — add test for `doctor --fix` cloning a missing repo.
