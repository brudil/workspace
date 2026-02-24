# Workspace Guide

You are working in a multi-repo workspace managed by `ws`. This file explains how the workspace is organized and how to work within it.

## Layout

Repos live under `repos/`. Each repo is a **bare clone** with linked worktrees.

```
workspace-root/
  repos/
    repo-name/
      .bare/                     # bare git repo (do not touch)
      .ground/                   # default branch worktree — READ-ONLY
      my-feature/                # a capsule (worktree on a branch)
      another-task/              # another capsule
    another-repo/
      .bare/
      .ground/
```

There is no `current` symlink. `ws` infers the active repo and capsule from your working directory.

## Before you edit anything

1. **Check your working directory path.**
2. **If you are in `.ground/`** — STOP. You cannot edit here. It is strictly read-only.
3. **If you are in a capsule** (any directory under `repos/<repo>/` that is not `.bare/` or `.ground/`) — you're good, work here.
4. **If no capsule exists for your task** — create one with `ws lift <repo> <name>`, or ask the user if an existing capsule should be used.

How to tell where you are: check if your cwd contains `/.ground/`. If it does, you are in ground and must not edit.

## Rules

**Ground is read-only.** `.ground/` exists only for reading code and checking state. To make changes, you must be in a capsule. If you aren't in one, create one with `ws lift <repo> <name>` or ask the user which existing capsule to use.

**One capsule, one concern.** Each capsule is an isolated environment for a single task. All your changes must stay within one capsule in one repo. Never modify files across multiple capsules or multiple repos in a single task.

**Repo names and aliases.** Users may refer to repos by display name or alias rather than the canonical directory name. Run `ws status -f llm` to see display names and aliases for each repo.

## Working in a capsule

Once you're inside a capsule, normal development workflows apply:

- **Git works normally.** Commit, push, pull, rebase — all standard git commands work.
- **First push auto-sets upstream.** `push.autoSetupRemote` is configured, so `git push` works without `-u` on the first push.
- **`ws lift` and `ws dock` change your cwd.** These commands automatically `cd` into the new capsule, so you're ready to work immediately after running them.

## Seeing workspace state

Run `ws status -f llm` to get live state for all repos. The output includes:

- Every repo (with display names and aliases) and its capsules
- Notable state only: dirty, ahead/behind, open PRs, boarded status
- Repos the user may refer to by alias or display name

Use this to orient yourself before starting work.

## Key commands

```
ws status [-f llm]                 # live state for all repos and capsules (use llm format)
ws lift <repo> <name> [base]       # create a new capsule (branch + worktree) from base
ws dock <repo> <branch|PR#|PR-URL> # check out an existing branch or PR into a capsule
ws burn [repo] <capsule>           # remove a capsule (alias: ws rm)
ws jump [repo] [capsule]           # navigate to a capsule (alias: ws j)
```

Also available: `ws debrief` (batch cleanup), `ws mc` (TUI), `ws board`/`ws unboard` (IDE workspace management).

## Terminology

**Ground** (`.ground/`): The worktree for the default branch. Read-only — never commit or make changes here. All capsules branch from ground.

**Capsule**: One unit of work — one branch, one worktree, one concern. Created by `lift` (new branch) or `dock` (existing branch/PR). Removed by `burn` or `debrief`.
