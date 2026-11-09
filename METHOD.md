# Orbital Development

You're still working on main.

You know you shouldn't be. You checked out a branch, sure — but you're working in the same directory you were debugging that production issue in yesterday. There's a stash you're afraid to pop. A file you modified but didn't commit because it's for "that other thing." Your working directory is a palimpsest of half-finished intentions.

This isn't a discipline problem. It's a design problem.

## The problem: shared mutable state

Every developer knows that shared mutable state is the root of all evil — in code. But we accept it completely in our working environment.

One directory. Multiple tasks. Constant context switching. The result:

- You stash work to review a PR, then forget what you stashed.
- You start a fix but can't test it cleanly because there's unrelated uncommitted work in the way.
- You `git checkout` between branches and wait for your tooling to re-index, rebuild, reinstall.
- You avoid starting a second task because your working directory is "occupied."

The working directory becomes a bottleneck. Not because of git, not because of your editor — because of a single assumption that went unquestioned: one repo means one working copy.

It doesn't have to.

## The method: one task, one orbit

Every task gets its own complete, isolated copy of the codebase. Not a branch — a full working environment. Its own directory, its own node_modules, its own build state, its own editor window. Completely independent.

This is a capsule. It lifts off from ground (main), orbits for exactly as long as it needs to, and comes back down when it's done. While in orbit, it's self-contained. Nothing on the ground affects it. Nothing it does affects the ground.

You don't "switch" between tasks. You walk to a different desk.

Starting work and finishing work are symmetric operations. One creates a capsule, the other destroys it. There is no accumulation. No archaeology. No "what was I doing in this directory?" The lifecycle is the method.

## The rules

**1. Ground is sacred.**
The main branch exists as a source of truth, not as a place to work. You don't check it out. You don't commit to it. You don't have a working copy of it that you "also use for quick fixes." It is the point of departure and the point of return. Nothing else.

**2. Every task orbits.**
A bug fix, a feature, a PR review, a spike — they all get their own capsule. No exceptions, no "this is too small to bother." The overhead of isolation is near zero. The cost of not isolating is invisible until it isn't.

**3. Capsules are disposable by design.**
A capsule ending is not cleanup. It's completion. The work has already been transmitted — pushed, reviewed, merged. The capsule served its purpose and now it's gone. Holding onto a dead capsule is like keeping an empty envelope because it once held an important letter.

**4. Never stash. Never juggle. Never switch.**
If you're stashing, you have a design problem. If you're juggling uncommitted changes across tasks, you have a design problem. If switching tasks requires any action other than opening a different directory, you have a design problem. The method eliminates these by making them structurally impossible.

## What a day looks like

You pick up a ticket. You lift a capsule — a fresh working copy branched from the latest main. Your editor opens. Your dependencies install. You're working in seconds, in a directory that contains exactly one concern: this task.

Halfway through, a teammate asks you to review their PR. You don't stash. You don't commit half-finished work with a "wip" message. You dock with their capsule. You review their code in a clean environment, run it, test it. When you're done, you undock.

You go back to your original task. It's exactly where you left it. Nothing moved. Nothing changed. You finish the work, push it, open a PR. When it merges, you land. Your machine is exactly as clean as it was this morning.

Three tasks. Zero context switches. No stashes, no work-in-progress commits, no "which branch am I on?" moments. Just isolated, parallel, independent work.

## Why this matters for agents

AI coding agents are the best stress test for your development workflow. An agent can't "remember" that it was in the middle of something. It can't mentally context-switch. It has no concept of "I'll come back to this." It needs a clean, isolated, non-modal environment — every single time.

If your workflow requires stashing, mental bookmarks, or careful branch juggling, an agent will fail at it. Not because the agent is limited, but because the workflow assumes a human holding state in their head.

Orbital development gives agents exactly what they need by default: a fresh, complete, isolated environment for every task. No modes. No hidden state. No assumptions about what happened before. The same property that makes this method better for humans makes it *possible* for agents.

Your workflow shouldn't require a brain to hold it together.

---

*There is a tool that does all of this.*
