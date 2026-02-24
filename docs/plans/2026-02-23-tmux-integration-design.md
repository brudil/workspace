# Tmux Integration for Mission Control

## Context

`ws mc` is the primary entry point for daily work, run inside an existing tmux session. Today, pressing `g` (go) creates a new unnamed tmux window every time. This produces duplicate windows and an unreadable status bar. The goal is to make MC tmux-aware: named windows, dedup, live tracking, and lifecycle cleanup — all implicit behind `$TMUX`, with zero impact on non-tmux usage.

## Design

### Window Naming Convention

When MC creates a tmux window, it names it `DisplayName:capsule` — e.g. `Frontend:auth-flow`, `API:fix-caching`.

- Display name comes from `ws.toml` `display_name`, falling back to canonical repo name.
- Ground worktrees use `DisplayName:.ground`.
- Set via `tmux new-window -n "DisplayName:capsule" -c <path>`.
- The naming convention doubles as the lookup key for dedup, live tracking, and cleanup.

### Smart Go (Dedup + Switch)

`doGo()` changes from always creating a new window to:

1. Query: `tmux list-windows -F '#{window_name} #{window_id}'`
2. Look for window named `DisplayName:capsule`
3. Found → `tmux select-window -t <window_id>`
4. Not found → `tmux new-window -n "DisplayName:capsule" -c <path>`

If a user manually renames a window, ws won't find it and creates a new one. That's acceptable — ws owns the names it creates.

### Live Tracking in MC

When `$TMUX` is set, MC queries `tmux list-windows -F '#{window_name}'` at init and on refresh. Parses `DisplayName:capsule` names and sets a `live` flag on matching rows.

- Runs alongside existing async status loading.
- `live` tag renders in the detail panel header alongside `docked`, `boarded`, `dirty`.
- Small `●` indicator in the list view next to live capsules.
- When not in tmux, query is skipped. No `live` field, no indicator, zero cost.
- Refresh (`r`) re-queries tmux windows.

### Lifecycle Sync (Burn Closes Windows)

When a capsule is removed from MC — explicit delete or debrief cleanup — ws closes its tmux window.

- Before removing the worktree, query for the window by name.
- If found, `tmux kill-window -t <window_id>`.
- Then proceed with normal worktree removal.
- Window killed before worktree removed so shells don't end up in deleted directories.
- Only when `$TMUX` is set.

Applies to `doDelete()` and `doRefresh()` (debrief auto-removal).

### Architecture: `internal/tmux` Package

New package `internal/tmux/tmux.go` with:

- `InTmux() bool` — checks `$TMUX` env var
- `ListWindows() map[string]string` — `windowName → windowID`
- `SelectWindow(id string) error` — switch to existing window
- `NewWindow(name, path string) error` — create named window at path
- `KillWindow(id string) error` — close a window

Each function is a thin wrapper around `exec.Command("tmux", ...)`. No state, no caching. MC's `cli/` code is the only consumer. The `workspace/` package has no knowledge of tmux.

`ws burn` from the CLI does not close tmux windows — MC-only behavior for v1.

## Not in v1

- Popup MC (`tmux display-popup`, `ws mc --popup`)
- Auto pane layouts per repo in `ws.repo.toml`
- Status bar integration (`ws prompt --format tmux`)
- `ws shell-init tmux` for keybinding installation
- CLI commands closing tmux windows (MC-only for now)
- `ws status` showing live info (MC-only)
