# Workspace Guide

You are working in a multi-repo workspace managed by `ws`. This file explains how the workspace is organized and how to work within it.

## Layout

Repos live under `repos/`. Each repo is a **bare clone** with linked worktrees. The workspace config is in `ws.toml` at the root.

```
workspace-root/
  ws.toml                        # workspace config (repos, org, default branch)
  repos/
    repo-name/
      .bare/                     # bare git repo (do not touch)
      .ground/                   # default branch worktree — read-only, never commit here
      my-feature/                # a capsule (worktree on a branch)
      another-task/              # another capsule
    another-repo/
      .bare/
      .ground/
```

To find the code for a repo, navigate into one of its capsules: `repos/<repo>/<capsule>/`. The `.ground/` worktree tracks the default branch and serves as the source of truth.

There is no `current` symlink. `ws` infers the active repo and capsule from your working directory.

## Terminology

**Ground** (`.ground/`): The worktree for the default branch. Sacred — never commit or make changes here. All capsules branch from ground.

**Capsule**: One unit of work — one branch, one worktree, one concern. Created by `lift` (new branch) or `dock` (existing branch/PR). Removed by `burn` or `debrief`.

## Rules

**One capsule, one concern.** Each capsule is an isolated environment for a single task. All your changes must stay within one capsule in one repo. Never modify files across multiple capsules or multiple repos in a single task.

**Ground is read-only.** Do not make commits or changes in `.ground/`. All work happens in capsules.

**Repo names and aliases.** Users may refer to repos by display name or alias rather than the canonical directory name. Check `ws.toml` under `[repos]` to resolve which repo they mean — look at `display_name` and `aliases` fields.

## Seeing workspace state

Run `ws status -f json` to get live state for all repos. The output includes:

- Every repo and its capsules
- Branch name, dirty state, ahead/behind counts
- Open pull requests linked to each capsule
Use this to orient yourself before starting work.

## Commands

```
ws status [-f json]                # live state for all repos and capsules
ws lift <repo> <name> [base]       # create a new capsule (branch + worktree) from base
ws dock <repo> <branch|PR#|PR-URL> # check out an existing branch or PR into a capsule
ws burn [repo] <capsule>           # remove a capsule (alias: ws rm)
ws debrief [repo] [--days N]       # batch cleanup of landed/inactive capsules
ws jump [repo] [capsule]           # navigate to a capsule (alias: ws j)
ws mc                              # full-screen mission control TUI
ws board <repo> <capsule>          # add capsule to IDE workspace
ws unboard <repo> <capsule>        # remove capsule from IDE workspace
```
