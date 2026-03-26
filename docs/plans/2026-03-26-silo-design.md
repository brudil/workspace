# Silo Design

## Problem

Tools like docker compose expect a single, stable directory path. Git worktrees give each capsule its own directory, so switching what you're testing means reconfiguring volume mounts, ports, and container setups. This makes it painful to test changes across capsules in multi-repo workspaces.

## Solution

A **silo** is a per-repo directory (`.silo/`) that acts as a stable runtime location for tools like docker compose. It mirrors the contents of whichever capsule you point it at, syncing git-tracked files in real time. Docker compose always runs from `.silo/` — you switch what's being tested by re-pointing the silo, not by reconfiguring containers.

Each repo can have at most one silo. A single workspace-level watcher process syncs all active silos.

## Directory Structure

```
repos/
  frontend/
    .bare/
    .ground/          # read-only default branch
    .silo/            # runtime mirror, never edited directly
    my-feature/       # capsule
  backend/
    .bare/
    .ground/
    .silo/
    add-health/
```

`.silo/` is a real git worktree checked out to the default branch, serving as a neutral filesystem base. Git-tracked files are overwritten by the sync engine; non-tracked files (node_modules, build artifacts) are left alone and belong to the silo.

## State

Silo targets are stored in `ws.local.toml` alongside boarded capsules:

```toml
[boarded]
frontend = ["my-feature"]

[silo]
frontend = ".ground"
backend = "add-health"
```

Each `[silo]` key maps a repo name to the capsule name it mirrors. `.ground` is a valid target. Removing a key means no active silo for that repo.

## Commands

### `ws silo point <repo> <capsule>`

Sets a repo's silo target. Works whether or not the watcher is running.

1. Resolve repo (aliases, `.` for cwd — same as other commands)
2. Resolve capsule (`.ground` is valid, fuzzy matching)
3. Create `.silo/` worktree if it doesn't exist (checked out to default branch)
4. Write target to `ws.local.toml [silo]`
5. Full sync — copy all git-tracked files from capsule to `.silo/`
6. Run `after_create` hook (precedence: `ws.local.toml` > `ws.toml` > `ws.repo.toml`)
7. Run `after_switch` hook from `ws.repo.toml [silo]`

### `ws silo watch`

Starts the workspace-level watcher. Foreground process, one per workspace.

1. Acquire lock file (`.silo.lock` at workspace root) — exit with error if another watcher is running
2. Read `ws.local.toml [silo]` to discover active targets
3. Start fsnotify watcher on each active capsule directory
4. Watch `ws.local.toml` for changes to `[silo]` section
5. On file change in capsule: debounce (200ms), sync git-tracked files to `.silo/`
6. On target change in `ws.local.toml`: re-sync, adjust watchers, run hooks
7. Log sync activity to stdout
8. Clean up lock file on Ctrl+C / SIGTERM

### `ws silo stop <repo>`

Removes a repo's silo.

1. Remove repo from `ws.local.toml [silo]`
2. Remove `.silo/` worktree (`git worktree remove`)
3. Watcher (if running) detects the change and stops watching

### `ws silo status`

Shows current silo state:

```
frontend    .ground      (watching)
backend     add-health   (synced, not watching)
```

## Sync Engine

### Full sync (on point or target switch)

1. Run `git ls-files` in the source capsule to get tracked file list
2. Copy all tracked files to `.silo/`, preserving directory structure
3. Delete files in `.silo/` that are git-tracked in the silo's worktree but absent from the capsule's tracked set (handles branch differences)
4. Leave non-tracked files in `.silo/` untouched

### Incremental sync (watcher running)

1. fsnotify fires for a changed file in the capsule
2. Debounce 200ms to batch rapid saves
3. Check if changed file is git-tracked (`git ls-files <path>`)
4. If tracked: copy to `.silo/`
5. If a tracked file is deleted: delete from `.silo/`

### Watcher setup

- Recursively watch capsule directories
- Filter events through git-tracked check
- Ignore `.git/` subdirectory events
- One watcher per active silo, all managed by the single `ws silo watch` process

## Lock File

`ws silo watch` creates `.silo.lock` at workspace root containing its PID.

- On start: check if lock exists and PID is alive. If alive, exit with error. If stale, remove and proceed.
- On shutdown: remove lock file.

## Hook Execution

### On silo creation (first `ws silo point` for a repo)

1. Create `.silo/` worktree
2. Full sync
3. `after_create` hook (precedence chain: `ws.local.toml` > `ws.toml` > `ws.repo.toml`)

### On target switch

1. Full sync from new capsule
2. `after_create` hook
3. `after_switch` hook (from `ws.repo.toml [silo]`)

All hooks run with cwd set to `.silo/`.

### ws.repo.toml silo config

```toml
[silo]
after_switch = "docker compose restart api"
```

## Integration with Existing Systems

### Status

`ws status` shows silo indicator per repo:

```
frontend    .ground
            my-feature        (silo -> my-feature)
backend     .ground           (silo -> .ground)
            add-health
```

`ws status -f llm` includes silo state.

### Doctor

New checks:
- Silo target points at non-existent capsule — warn, suggest repoint to `.ground`
- `.silo/` exists but no `[silo]` entry — warn, orphaned silo
- Stale lock file (PID dead) — auto-fix: remove

### IDE Workspace

`.silo/` is excluded from IDE workspaces. `ListWorktrees` skips `.silo/` alongside `.bare/` and `.ground/`.

### Debrief

`ws debrief` skips `.silo/` when scanning for stale capsules. If a burned capsule was a silo target, debrief offers to repoint to `.ground`.

### Burn

`ws burn` warns if the target capsule is an active silo target. Optionally auto-repoints to `.ground` and re-syncs.

## Edge Cases

### Capsule burned while silo points at it

Watcher detects directory disappearing, stops watching. Config becomes stale. `ws silo status` shows error. `burn` warns and can auto-repoint to `.ground`.

### Dirty silo files

`.silo/` is a mirror. Edits to tracked files in `.silo/` are overwritten on next sync. This is expected and documented.

### Large repos / fsnotify limits

macOS default watch descriptor limit is 8192. If exceeded, log a warning and fall back to periodic polling (1-2s). `ws doctor` can check/report on watch limits.

### Watcher crash

Stale lock file handled by PID check. Silos remain in last-synced state. Re-running `ws silo watch` picks up where it left off.
