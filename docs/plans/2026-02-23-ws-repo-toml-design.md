# ws.repo.toml — Per-Repo Capsule Configuration

## Summary

Introduce an optional `ws.repo.toml` file that lives inside each repo's `.ground/` directory. It provides repo-level capsule creation settings: copying files from ground into new worktrees, and a fallback create hook.

## File Format

```toml
# repos/<name>/.ground/ws.repo.toml

[capsule]
copy_from_ground = [".env", "config/local.yaml"]
on_create = "npm install"
```

Always read from `.ground/ws.repo.toml`. The ground version is canonical. The file is optional; absence means no repo-level config.

## Config Parsing

New in `config/` package:

- `RepoFileConfig` struct with `Capsule` containing `CopyFromGround []string` and `OnCreate string`
- `ParseRepoConfig(path string) (*RepoFileConfig, error)` — parses the TOML file at the given path. Returns nil (not error) if file doesn't exist.

Loaded on demand — only when a capsule is being created for a specific repo. Not loaded at startup.

## Capsule Creation Flow

Updated flow in `LiftWorktree` / `DockWorktree` (operations.go):

1. Create worktree (existing behavior)
2. Load `ws.repo.toml` from `.ground/` via `config.ParseRepoConfig()`
3. **Copy files** — for each path in `copy_from_ground`, copy from `.ground/<path>` to `<new-worktree>/<path>`, creating parent dirs as needed. Best-effort: copy what exists, one summary warning listing all skipped files.
4. **Run hook** — if `post_create` is set for this repo (from `ws.toml` or `ws.local.toml`), use that. Otherwise fall back to `ws.repo.toml`'s `on_create`. Only one hook runs.

## Hook Precedence

```
ws.local.toml post_create  >  ws.toml post_create  >  ws.repo.toml on_create
```

Matches the existing merge behavior. `ws.repo.toml` is the lowest-priority fallback.

## Error Handling

- Missing `ws.repo.toml`: no-op, proceed normally
- Missing files in `copy_from_ground`: skip, collect names, print single warning at end
- TOML parse error: return error, fail the operation
- Copy failure (permission, disk full): return error, fail the operation

## Design Decisions

- **Exact paths only** — no glob patterns in `copy_from_ground`
- **Copy before hook** — copied files are in place before any hook runs, so hooks can depend on them
- **One hook runs** — `post_create` from ws.toml/ws.local.toml wins; `on_create` is a fallback, not additive
- **Parsing in config/, loading in workspace/** — keeps TOML parsing centralized, but loads lazily per-repo

## Testing

- Unit tests for `ParseRepoConfig` (valid file, missing file, malformed TOML)
- Unit tests for copy logic (files exist, some missing, nested directories)
- Integration with existing lift/dock test patterns
