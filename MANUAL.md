# ws

A multi-repo workspace manager. `ws` gives development teams a single control plane for many repositories, using git worktrees to create lightweight, isolated working environments.

Work happens in **capsules**, not on the main branch. Ground is sacred.

## Table of Contents

- [Getting Started](#getting-started)
- [Concepts](#concepts)
  - [Workspaces](#workspaces)
  - [Ground](#ground)
  - [Capsules](#capsules)
  - [Lifting](#lifting)
  - [Docking](#docking)
  - [Boarding](#boarding)
  - [Debrief](#debrief)
  - [Mission Control](#mission-control)
  - [Repos and Aliases](#repos-and-aliases)
- [Configuration](#configuration)
  - [ws.toml](#wstoml)
  - [ws.local.toml](#wslocaltoml)
  - [ws.repo.toml](#wsrepotoml)
- [Shell Integration](#shell-integration)
  - [The ws Wrapper](#the-ws-wrapper)
  - [Tab Completions](#tab-completions)
  - [Starship](#starship)
  - [Tmux](#tmux)
- [IDE Integration](#ide-integration)
  - [VS Code / Cursor](#vs-code--cursor)
  - [IntelliJ](#intellij)
  - [Opening Your Editor](#opening-your-editor)
- [Silos](#silos)
  - [The Problem](#the-problem)
  - [How It Works](#how-it-works)
  - [Commands](#silo-commands)
  - [Live Watching](#live-watching)
  - [Hooks](#silo-hooks)
  - [ws.repo.toml Silo Config](#wsrepotoml-silo-config)
- [Customisation](#customisation)
  - [Repo Colors](#repo-colors)
  - [Post-Create Hooks](#post-create-hooks)
  - [Fuzzy Matching](#fuzzy-matching)
  - [JSON Output](#json-output)

---

## Getting Started

### Prerequisites

- Git
- [GitHub CLI](https://cli.github.com/) (`gh`) — used for PR integration and authentication.

### Creating a Workspace

A workspace lives in a shared config repo that contains a `ws.toml` file describing your repos. To initialise:

```bash
ws init git@github.com:your-org/workspace-config.git
```

This clones the config repo, then clones every repo listed in `ws.toml` as a bare repository with a `.ground` worktree for the default branch. The directory structure looks like this:

```
my-workspace/
  ws.toml
  ws.local.toml
  repos/
    frontend/
      .bare/
      .ground/
    backend/
      .bare/
      .ground/
```

If you prefer SSH for git operations:

```bash
ws init git@github.com:your-org/workspace-config.git --git ssh
```

### First Capsule

```bash
ws lift frontend my-feature
```

This creates a new branch `my-feature` from the default branch and drops you into the worktree. You're ready to work.

When you're done, push and open a PR. You don't need to clean up — once the PR is merged, `ws debrief` will detect that the capsule has landed and remove it automatically. If the PR is rejected or abandoned, `burn` it manually:

```bash
ws burn frontend my-feature
```

---

## Concepts

### Workspaces

A workspace is a directory containing a `ws.toml` config file and a `repos/` directory. The config file is typically stored in its own git repository, shared across a team, so everyone gets the same repo list, aliases, and display names.

`ws` discovers the workspace by walking up from your current directory looking for `ws.toml`. Every command works from anywhere inside the workspace tree.

### Ground

Each repo has a special worktree called `.ground`. This is the default branch — `main`, `develop`, whatever your team uses. Ground is the source of truth.

You don't work in ground. It exists so that new capsules can branch from a known-good state and so that `ws` can compare capsules against the baseline (ahead/behind counts, merge status).

Ground cannot be removed with `burn`.

### Capsules

A capsule is the unit of work in `ws`: one branch, one worktree, one concern. Capsules are cheap to create and cheap to destroy.

```
repos/frontend/
  .bare/            # the bare git repo
  .ground/          # default branch (sacred)
  my-feature/       # a capsule
  bugfix-login/     # another capsule
```

Each capsule is a full working copy. You can have as many as you need running simultaneously. When a capsule's branch is merged or goes stale, `debrief` cleans it up.

If a branch name contains slashes (e.g. `feature/my-thing`), the capsule directory uses only the last segment (`my-thing`) to avoid nested directories.

### Lifting

**Lifting** creates a new capsule — a fresh branch from ground.

```bash
ws lift <repo> <branch> [base]
```

- `repo` can be the canonical name, an alias, or `.` to infer from your current directory.
- `base` defaults to `origin/<default-branch>`. Pass a different ref to branch from somewhere else.

After lifting, `ws` runs the repo's `after_create` hook (if configured), boards the capsule into your IDE workspace, and `cd`s you into the new worktree.

### Docking

**Docking** checks out an existing branch into a new worktree. This is how you pick up work that already exists — someone else's feature branch, or a PR you want to review.

```bash
ws dock <repo> <branch>
```

You can also dock by PR number or full PR URL:

```bash
ws dock frontend 1234
ws dock https://github.com/your-org/frontend/pull/1234
```

When docking by PR number or URL, `ws` resolves the PR's head branch via the GitHub API and creates a worktree for it.

Like lifting, docking runs `after_create` hooks, boards the capsule, and `cd`s you in.

### Boarding

Boarding controls which capsules are visible in your IDE workspace files. When you `lift` or `dock` a capsule, it's automatically boarded. When you `burn` it, it's automatically unboarded.

You can also manage boarding manually:

```bash
ws board <repo> <capsule>
ws unboard <repo> <capsule>
```

Boarding state is stored in `ws.local.toml` and triggers regeneration of IDE workspace files (VS Code `.code-workspace`, IntelliJ project) so your editor reflects only the capsules you're actively working on.

### Debrief

Over time capsules accumulate. `debrief` scans all capsules across your workspace and cleans up the ones that are done:

```bash
ws debrief
```

A capsule is removed if it's:
- **Landed** — its PR has been merged.
- **Inactive** — no commits for 14 days (configurable with `--days`).

A capsule is skipped if it has uncommitted changes, even if it's landed or inactive.

Capsules that are still active are reported with their age, ahead/behind counts, and open PR status.

You can scope debrief to a single repo:

```bash
ws debrief frontend
```

### Mission Control

`ws mc` opens an interactive terminal dashboard showing every repo and capsule in your workspace.

**Navigation:**
- `j`/`k` or arrow keys to move between rows
- `J`/`K` or shift+arrows to scroll the detail pane
- `/` to filter by text
- `1`–`4` to toggle filters (local only, my PRs, review requested, dirty)
- `?` to toggle help

**Actions:**
- `Enter` — go to the selected capsule (`cd` in your shell, or a tmux window if inside tmux)
- `o` — open in `$EDITOR`
- `b` — toggle boarding for the selected capsule
- `d` — delete (burn) the selected capsule, or dock a ghost PR
- `r` — refresh (debrief and rebuild)
- `:` — command palette

Mission control shows live data: dirty status, ahead/behind counts, open PRs with CI check results. Ghost PRs (open PRs without a local worktree) appear under their repo so you can dock them with a single keypress. Capsules with an open tmux window show a green `●` indicator and a `live` tag in the detail panel.

### Repos and Aliases

Every command that takes a repo argument goes through the same resolution pipeline:

1. **Exact match** against canonical names in `ws.toml`.
2. **Alias match** against aliases defined in `ws.toml` or `ws.local.toml`.
3. **Fuzzy match** — every character in your input must appear in order in the repo name (case-insensitive). If there's exactly one match, it's used. Multiple matches show an interactive picker.
4. **`.`** — infer the repo from your current directory.
5. **No argument** — interactive picker.

The same fuzzy matching applies to capsule arguments (e.g. in `burn` and `jump`).

---

## Configuration

### ws.toml

The shared workspace config. Lives at the root of the workspace and is typically checked into the config repo.

```toml
[workspace]
org = "your-org"              # GitHub organisation
default_branch = "main"       # default branch name for all repos
display_name = "My Workspace" # optional — shown in prompts and mission control

[repos.frontend]
display_name = "Frontend"
aliases = ["fe", "front"]
color = "#FF6B9D"
after_create = "npm install && npm run build"

[repos.backend]
display_name = "API"
aliases = ["api", "be"]
after_create = "make setup"

[repos.infrastructure]
aliases = ["infra"]
```

**Workspace fields:**

| Field | Required | Description |
|---|---|---|
| `org` | yes | GitHub organisation. Used for cloning repos and API calls. |
| `default_branch` | yes | The branch name used for ground (e.g. `main`, `develop`). |
| `display_name` | no | Human-friendly workspace name. Falls back to `org`. |

**Repo fields:**

| Field | Description |
|---|---|
| `display_name` | Human-friendly name shown in UI. Falls back to the canonical name. |
| `aliases` | Short names for the repo (e.g. `fe` for `frontend`). Used in commands and completions. |
| `color` | Terminal colour for this repo. Accepts hex (`#FF6B9D`) or 256-colour codes. |
| `after_create` | Shell command run in the worktree after `lift` or `dock`. Failures are logged but non-fatal. |

### ws.local.toml

Per-machine overrides. Lives alongside `ws.toml` but is gitignored. Created automatically as needed.

```toml
git = "ssh"

[repos.frontend]
after_create = "pnpm install"
aliases = ["f"]

[boarded]
frontend = [".ground", "my-feature"]
backend = [".ground"]
```

**Top-level fields:**

| Field | Description |
|---|---|
| `git` | Clone protocol: `"ssh"` or `"https"` (default). |

**Repo overrides:**

You can override `display_name`, `color`, `after_create`, and `aliases` per repo. Aliases are appended to the shared list; other fields replace the shared value.

**Boarded section:**

Managed automatically by `ws`. You generally don't edit this by hand. Lists which capsules are currently visible in your IDE workspace.

### ws.repo.toml

Some repos need setup work before you can develop in them — installing dependencies, copying local config files, running code generation. These steps need to happen every time someone creates a capsule, and they're specific to the repo, not the workspace.

`ws.repo.toml` lives inside the repo itself, committed alongside the code. Place it at the root of the repo (it'll be read from `.ground/ws.repo.toml`).

```toml
[capsule]
copy_from_ground = [".env", "config/local.yaml"]
after_create = "npm install && npm run codegen"
```

**`copy_from_ground`** — Files that exist in your ground worktree but aren't checked into git (local config, `.env` files, generated certs). When a capsule is created, these are copied from `.ground/` into the new worktree before any hooks run. Missing files are silently skipped.

This solves a common pain point: you set up `.env` once in ground, and every capsule gets a copy automatically. No more "why isn't my app starting" after lifting a new branch.

**`after_create`** — A shell command run in the new worktree after file copying. Use it for dependency installation, build steps, or anything the repo needs to be workable.

This field is a fallback — if `after_create` is also set in `ws.toml` or `ws.local.toml` for this repo, those take precedence. The priority order is:

1. `ws.local.toml` (your machine-specific override)
2. `ws.toml` (workspace-wide config)
3. `ws.repo.toml` (repo's own default)

This lets repos ship sensible defaults while still allowing workspace-level or personal overrides.

| Field | Description |
|---|---|
| `copy_from_ground` | List of file paths to copy from `.ground/` into new capsules. Paths are relative to the repo root. Missing files are skipped. |
| `after_create` | Shell command run after capsule creation. Used as a fallback when no workspace-level hook is set. |

---

## Shell Integration

### The ws Wrapper

`ws` commands like `lift`, `dock`, `jump`, and `mc` need to change your shell's working directory. Since a subprocess can't change its parent's directory, these commands print a `cd` command to stdout, and a shell wrapper function captures and evaluates it.

Add this to your `~/.zshrc`:

```bash
eval "$(workspace shell-init zsh)"
```

This defines a `ws` function that wraps the `workspace` binary. For `jump`, `lift`, `dock`, and `mc`, output is evaluated in your shell. All other commands pass through directly.

Without this wrapper, navigation commands will print a `cd` path instead of actually navigating.

### Tab Completions

The shell wrapper also sets up zsh completions. Repos, capsules, editor names, and subcommands are all completed:

- `ws lift <TAB>` — repo names and aliases
- `ws burn frontend <TAB>` — capsule names in frontend
- `ws jump <TAB>` — repo names and aliases
- `ws open <TAB>` — `cursor`, `code`, `cursor-agent`, `idea`

### Starship

Use `ws prompt` to feed workspace context into [Starship](https://starship.rs/) or any other prompt framework.

**Short format** — returns `Workspace / Repo Display Name`:

```bash
ws prompt --format short
# → My Workspace / Frontend
```

**JSON format** — returns all available context:

```bash
ws prompt --format json
# → {"workspace_display_name":"My Workspace","repo_name":"frontend","repo_display_name":"Frontend","repo_color":"#FF6B9D","capsule_name":"my-feature","is_capsule_boarded":true}
```

**Custom template** — Go template syntax:

```bash
ws prompt --template '{{.RepoDisplayName}} {{.CapsuleName}}'
# → Frontend my-feature
```

Available template fields: `WorkspaceDisplayName`, `RepoName`, `RepoDisplayName`, `RepoColor`, `CapsuleName`, `IsCapsuleBoarded`.

**Example Starship config** (`~/.config/starship.toml`):

```toml
format = '${custom.ws}$all'

[custom.ws]
command = "workspace prompt"
format = "[$output]($style) "
when = true
symbol = " "
style = "italic cyan"
```

Add `${custom.ws}` to your `format` string to position it in your prompt. `when = true` always runs the command — Starship hides the module automatically when the output is empty, which is what `ws prompt` produces outside of a workspace.

### Tmux

Mission control detects tmux automatically. When `$TMUX` is set, several things change:

- **`Enter` (go)** opens the capsule in a **named tmux window** instead of `cd`-ing. The window is named `RepoDisplay:capsule` (e.g. `Frontend:auth-flow`). If a window already exists for that capsule, MC focuses it rather than creating a duplicate. If all panes in the window are busy (running vim, node, etc.), a new pane is split instead.
- **`o` (open)** launches `$EDITOR` in a new pane within the capsule's window, rather than suspending MC.
- **Live tracking** — capsules with an open tmux window show a green `●` in the list and a `live` tag in the detail panel.
- **Lifecycle cleanup** — when a capsule is deleted or debriefed, its tmux window is closed automatically.

Outside of tmux, `Enter` performs a regular `cd` via the shell wrapper and `o` opens the editor in the foreground.

---

## IDE Integration

### VS Code / Cursor

`ws` generates a `workspace.code-workspace` file at the workspace root. This file's `folders` array is kept in sync with your boarded capsules. When you board or unboard a capsule, the file is regenerated and your editor updates its sidebar.

Each folder is named `Display Name (capsule)` — e.g. `Frontend (.ground)` or `API (my-feature)`.

The workspace file must be valid JSON (no comments or trailing commas). `ws` preserves any settings or extensions you've added; it only rewrites the `folders` key.

### IntelliJ

`ws` generates IntelliJ project configuration at the workspace root. Boarded capsules are added as content roots in the project structure.

### Opening Your Editor

```bash
ws open           # opens in Cursor (default)
ws open code      # VS Code
ws open idea      # IntelliJ IDEA
ws open cursor-agent
```

This opens the workspace-level project file, which includes all your boarded capsules.

---

## Silos

### The Problem

Some tools expect a single, stable directory path. Docker compose, for example, needs volume mounts that don't change when you switch what you're working on. With git worktrees, every capsule has its own directory, so you'd have to reconfigure ports, mounts, and container setups every time you want to test a different branch.

### How It Works

A **silo** is a per-repo directory (`.silo/`) that acts as a stable runtime location. You point it at any capsule, and `ws` mirrors the capsule's git-tracked files into `.silo/` in real time. Docker compose always runs from `.silo/` — you switch what's being tested by re-pointing the silo, not by reconfiguring containers.

```
repos/frontend/
  .bare/
  .ground/          # default branch (sacred)
  .silo/            # stable runtime mirror
  my-feature/       # capsule
  bugfix-login/     # capsule
```

Each repo can have at most one silo. `.silo/` is a real git worktree (with a detached HEAD), so path-dependent tools work normally. Non-tracked files in `.silo/` — `node_modules`, build artifacts, `.env` — are left alone and belong to the silo.

Silo state is stored in `ws.local.toml`:

```toml
[silo]
frontend = ".ground"
backend = "add-health"
```

### Silo Commands

**Point a silo at a capsule:**

```bash
ws silo point <repo> <capsule>
```

Creates `.silo/` if it doesn't exist, syncs all git-tracked files from the capsule, and runs hooks. `.ground` is a valid target — use it to run the main branch version.

```bash
ws silo point frontend .ground     # use main branch
ws silo point frontend my-feature  # switch to a capsule
```

`silo point` works whether or not the watcher is running. If the watcher is running, it detects the change and starts watching the new target automatically.

**Start the live watcher:**

```bash
ws silo watch
```

Watches all active silos across the workspace. One command, all repos. See [Live Watching](#live-watching).

**Show silo state:**

```bash
ws silo status
```

Shows each active silo, its target, and whether the watcher is running.

**Remove a silo:**

```bash
ws silo stop <repo>
```

Removes the `.silo/` worktree and clears the silo config.

### Live Watching

`ws silo watch` is a single foreground process that syncs all active silos in real time:

- Uses `fsnotify` to watch capsule directories for changes.
- Debounces rapid edits (200ms) to batch saves.
- Only syncs git-tracked files — untracked files (build output, dependencies) are ignored.
- Watches `ws.local.toml` for target changes — when you `silo point` from another terminal, the watcher picks it up, re-syncs, and starts watching the new capsule.
- Only one watcher can run per workspace (enforced via a lock file at `.silo.lock`).

Press `Ctrl+C` to stop.

### Silo Hooks

When a silo is created or re-pointed, two hooks run in sequence:

1. **`after_create`** — the same hook that runs when creating a capsule (precedence: `ws.local.toml` > `ws.toml` > `ws.repo.toml`). Use it for dependency installation.
2. **`after_switch`** — a silo-specific hook defined in `ws.repo.toml`. Use it for restarting services.

Both hooks run with the working directory set to `.silo/`.

### ws.repo.toml Silo Config

Repos can define silo-specific behaviour in `ws.repo.toml`:

```toml
[silo]
after_switch = "docker compose restart api"
```

| Field | Description |
|---|---|
| `after_switch` | Shell command run in `.silo/` after the sync target changes. Useful for restarting services that don't hot-reload. |

### Integration with Other Commands

Silos integrate with the rest of `ws`:

- **`ws burn`** — warns if the capsule you're burning is an active silo target and offers to repoint to `.ground`.
- **`ws debrief`** — if a debriefed capsule was a silo target, automatically repoints to `.ground`.
- **`ws status`** — shows a silo indicator for repos with an active silo.
- **`ws doctor`** — checks for missing silo targets, orphaned `.silo/` directories, and stale lock files.

---

## Customisation

### Repo Colors

Each repo can have a custom terminal colour, used in mission control and status output. Set it in `ws.toml` or override it per-machine in `ws.local.toml`:

```toml
[repos.frontend]
color = "#FF6B9D"
```

Accepts hex colours or 256-colour codes.

### Post-Create Hooks

A `after_create` hook runs in the new worktree after every `lift` or `dock`. Use it for dependency installation, code generation, or build steps:

```toml
[repos.frontend]
after_create = "npm install && npm run build"

[repos.backend]
after_create = "make setup"
```

Hook failures are logged to stderr but don't block the command. You can override a repo's hook locally in `ws.local.toml`.

### Fuzzy Matching

Repo and capsule arguments use subsequence matching — every character in your input must appear in order in the target, case-insensitive. This works like fzf:

- `fe` matches `frontend`
- `bk` matches `backend`
- `mf` matches `my-feature`

If there's exactly one match, it's used directly. Multiple matches show an interactive picker. Zero matches return an error.

Aliases are included in fuzzy matching, so if `frontend` has alias `fe`, then `f` matches both the canonical name and the alias (deduplicating to one result).

### JSON Output

Several commands support JSON output for scripting and automation:

**Status:**

```bash
ws status --format json
```

Returns the full workspace state — repos, worktrees, dirty status, ahead/behind counts.

**Prompt:**

```bash
ws prompt --format json
```

Returns current workspace context as a JSON object. Useful for building custom integrations.

---

## Other Commands

A few commands that don't fit neatly into the concepts above:

**`ws doctor`** — runs health checks on your workspace. Verifies repos are cloned, worktrees are intact, and required tools are available.

**`ws upgrade`** — pulls the latest `ws.toml` from the config repo and clones any newly added repos. Reports what changed.

**`ws status`** — shows all repos and their worktrees with git status. Use `--format json` for machine-readable output.

**`ws jump`** (alias `j`) — navigate to any capsule. Supports fuzzy matching and interactive pickers:

```bash
ws jump frontend my-feature   # direct
ws jump fe mf                 # fuzzy
ws jump frontend              # pick a capsule
ws jump                       # pick a repo, then a capsule
ws jump ~                     # workspace root
```
