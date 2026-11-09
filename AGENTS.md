# ws CLI

**ws is a developer workspace orchestrator.** It sets up, maintains, and gives visibility into multi-repo development environments built on git worktrees.

## Scope

**In scope:**
- **Workspace definition** — declarative config (ws.toml) for multi-repo workspaces
- **Worktree lifecycle** — clone, create, switch, remove, detect stale worktrees
- **Visibility** — status, PR state, and activity across all repos (CLI + TUI)
- **Editor sync** — keep editor workspace *structure* (which folders are open) in sync with workspace state
- **Light git convenience** — fetch all, show dirty state, detect ahead/behind — read-only or low-risk operations

**Out of scope:**
- Build/CI orchestration (nx, turborepo territory)
- Cross-repo dependency management (package managers)
- Editor settings/preferences/extensions authoring
- Modifying git working trees or making commits

## Stack

Go, cobra, bubbletea/lipgloss/huh, go-gh/v2, BurntSushi/toml

## Architecture

**Core/imperative pattern** — business logic in `workspace`, CLI is a thin frontend.

```
ws/
  cmd/ws/main.go                 Entry point
  internal/
    workspace/                   Core library — no UI deps
      workspace.go               Workspace struct + path helpers
      operations.go              Setup, FetchAll, NewWorktree, RemoveWorktree, UseWorktree
      status.go                  Status, StatusOutline, QueryWorktreeStatus
      git.go                     Git exec wrappers (all output captured, never to stdout)
      detect.go                  DetectRepo — infer repo/worktree from cwd path
    config/                      TOML parsing + ws.toml discovery (walk parents)
    github/                      go-gh PR helpers (PRsForRepo, PRForBranch, PRFromNumber)
    ui/                          Lipgloss styles, huh interactive prompts
    cli/                         Cobra commands — arg parsing + formatting only
      root.go                    Root command, registers all subcommands
      context.go                 LoadContext, ResolveRepo (bridges config → Workspace)
      status.go                  Bubbletea async model — renders instantly, streams git info
      setup.go, fetch.go, ...    One file per command
```

**Key rule:** `workspace/` returns data. `cli/` formats it. Never `fmt.Print` from workspace.

## Commands

```
ws status          Show repos + worktrees + git state (default command)
ws setup           Clone all repos from ws.toml
ws fetch           Fetch all repos in parallel
ws new [repo] <branch>   Create worktree
ws rm [repo] <branch>    Remove worktree (dirty guard)
ws use [repo] [branch]   Set current symlink
ws open [editor]         Open workspace in editor
```

`"."` as repo arg = infer from cwd. Omit repo = auto-detect or interactive picker.

## Config

`ws.toml` at workspace root:

```toml
[workspace]
org = "my-gh-org"
default_branch = "main"

[repos]
my-repo = {}
```

## Build & Test

```sh
cd ws
go build -o ws ./cmd/ws    # build
go test ./... -v            # run tests
```

Dev binary symlinked to `~/go/bin/ws`.

## Workspace layout

```
workspace-root/
  ws.toml
  repos/
    repo-name/
      main/          ← default branch worktree (git clone target)
      feature-x/     ← additional worktrees
      current        ← symlink → active worktree
```

## Patterns

- **Async UI:** status uses bubbletea — `StatusOutline()` for instant structure, `QueryWorktreeStatus()` per worktree via `tea.Cmd`
- **Repo resolution:** explicit arg > `"."` (cwd) > auto-detect > interactive picker (see `context.go:ResolveRepo`)
- **Git ops:** all captured via `CombinedOutput()`, never piped to stdout. Commands decide what to show.
- **Tests:** config and detect packages have unit tests. Git ops tested via integration.
