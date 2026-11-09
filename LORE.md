# Lore

Ground is main. It's sacred — the source of truth, the point of departure, the point of return. You don't work on ground. You don't commit to ground. Ground is clean.

Work happens in space.

A **capsule** is a unit of work. Small, self-contained, single-occupant, built for one mission. It's a worktree, a branch, a build environment — everything needed for exactly one task. When the mission ends, the capsule is gone.

## Lifecycle

You **lift** a capsule — a fresh, isolated environment branched from ground. You're in space. Nothing on ground affects you. Nothing you do affects ground.

You work. When you're ready, you push and open a PR — requesting reentry. Anyone can **dock** with your capsule. They pull it down, inspect it, run it. Docking is how review happens. When they're done, they **undock**.

If ground gives the GO, you **land**. The work merges into ground. Mission complete.

If it's a NO-GO, you **burn**. The capsule reenters and burns up. Mission failed.

Either way, the capsule is gone. Ground is clean.

## Commands

```
lift     create a capsule, start work
dock     check out an existing capsule
undock   disconnect from a docked capsule
land     merge into ground
burn     abandon a capsule
mc       mission control — overview of all capsules
```

## States

A capsule has four meaningful states:

| State         | Mine? | Local? | PR? | Meaning                              |
|---------------|-------|--------|-----|--------------------------------------|
| **active**    | yes   | yes    | no  | In flight. Development in progress.  |
| **up**        | yes   | yes    | yes | PR open. Requesting reentry.         |
| **available** | no    | no     | yes | Someone else's capsule. Visible, not local. |
| **docked**    | no    | yes    | yes | Connected. Reviewing their work.     |
