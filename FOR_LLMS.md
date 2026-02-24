# Workspace Guide

You are working in a multi-repo workspace managed by `ws`. This file explains how the workspace is organized and how to work within it.

## Layout

Repos live under `repos/`. Each repo is a **bare clone** with linked worktrees.

```
workspace-root/
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

**Repo names and aliases.** Users may refer to repos by display name or alias rather than the canonical directory name. Run `ws status -f llm` to see display names and aliases for each repo.

## Seeing workspace state

Run `ws status -f llm` to get live state for all repos. The output includes:

- Every repo (with display names and aliases) and its capsules
- Notable state only: dirty, ahead/behind, open PRs, boarded status
- Repos the user may refer to by alias or display name
Use this to orient yourself before starting work.

## Commands

```
ws status [-f llm]                 # live state for all repos and capsules (use llm format)
ws lift <repo> <name> [base]       # create a new capsule (branch + worktree) from base
ws dock <repo> <branch|PR#|PR-URL> # check out an existing branch or PR into a capsule
ws burn [repo] <capsule>           # remove a capsule (alias: ws rm)
ws debrief [repo] [--days N]       # batch cleanup of landed/inactive capsules
ws jump [repo] [capsule]           # navigate to a capsule (alias: ws j)
ws mc                              # full-screen mission control TUI
ws board <repo> <capsule>          # add capsule to IDE workspace
ws unboard <repo> <capsule>        # remove capsule from IDE workspace
```
