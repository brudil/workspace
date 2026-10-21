# Outer Repos — In the Workspace, Out of the Way

**Date:** 2026-10-21
**Status:** Proposed

## Problem

A team's `ws.toml` lists the repos the team owns, and that works well. But teams also rely on repos they don't own: platform repos, shared infra, design systems, other teams' services. You read these far more often than you change them. Today they live outside the workspace. They get cloned somewhere else by hand and kept up to date by hand, and none of the `ws` tooling can reach them: `ws rg` can't search them, `ws jump` can't go to them, and the rare `ws lift` for a platform PR doesn't work.

Adding them to `[repos]` as normal entries doesn't work either. Every command that covers all repos would then include them. `ws` status gets longer, `ws rg` returns platform matches for every search, fetches get slower, and `init` clones repos that some people will never open.

## Idea

Add a second **tier** of repo. An **outer** repo is defined in `ws.toml` like any other repo, but commands only include it when you ask for it.

> **The rule:** naming an outer repo always works. Commands that cover every repo by default leave outer repos out.

```toml
[repos.frontend]
display_name = "Frontend"

[repos.platform-infra]
tier = "outer"
aliases = ["infra"]

[repos.design-system]
tier = "outer"
aliases = ["ds"]
```

`tier` defaults to `"core"`, so existing configs don't change.

## Behaviour

### Naming a repo explicitly: always works

| Path | Outer behaviour |
|---|---|
| Exact name or alias (`ws jump infra`, `ws lift ds fix-tokens`) | Resolves normally. |
| cwd detection (standing in `repos/platform-infra/...`) | Resolves normally. |
| `ws rg --repo infra` | Included. |
| Shell completions | Outer names and aliases are offered too, listed after core. Completions are how people find them. |
| Fuzzy match | Searches core repos first and only falls back to outer repos when no core repo matches. This keeps `ws jump f` from turning ambiguous because of a platform repo. |
| Interactive picker (no argument) | Core only. |

### Commands that cover all repos: core only by default

| Command | Default | Opt in |
|---|---|---|
| `ws` (status), `ws status --json/--llm` | Core repos, **plus any outer repo with live capsules** | `--outer` |
| `ws mc` | Same as status | `--outer`, or a palette toggle |
| `ws rg` | Core, plus the outer repo you're standing in (keeps `ScopeHere` behaving the same) | `--outer`, or `--repo <outer>` |
| `ws debrief` | Core, plus outer repos with capsules. Cleaning up is safe and stops capsules from piling up unnoticed. | — |
| Fetch (status refresh, etc.) | Core only | `--outer` |
| `ws doctor` | Core checks as today. An uncloned outer repo is info, not an error. | — |
| IDE files | Already driven by boarding, so outer repos appear only once a capsule is boarded. No change. | — |

**Active outer repos.** An outer repo with at least one capsule beyond `.ground` is one you're working in right now. Status and debrief should show it, or you would lose track of your own branches. Once those capsules are debriefed, it drops out of view again.

### Cloning: lazy by default

`ws init`, `ws setup` and `ws upgrade` skip outer repos. The first command that needs an outer repo's files clones it, with the same progress output `init` uses, and then carries on. That means `jump`, `lift`, `dock` and `rg --repo` all clone on first use. `ws setup --outer` clones all outer repos up front for anyone who wants them local.

This matters most for platform repos, which tend to be large. Each person only pays for the ones they actually touch.

### Personal overrides via `ws.local.toml`

The team config decides the default. Each person can change it for themselves:

```toml
# ws.local.toml — I'm on the platform rotation this quarter
[repos.platform-infra]
tier = "core"

# ...and I never touch mobile
[repos.mobile]
tier = "outer"
```

`config.Merge` already applies per-repo overrides, so this needs one more field there. Demoting a core repo to outer covers the related request "hide the repos I don't work on" with no extra design.

## Implementation sketch

Small. The work is mostly at the one place repos are listed, not spread across the commands.

1. **Config:** add `Tier string` to `RepoConfig` and accept `""`, `"core"` or `"outer"`. Validate in `Load` the same way `git` is validated. Add the override to `Merge`.
2. **Workspace:** `RepoNames` stays as the list commands use when they cover all repos, but now holds **core only**. Add `OuterRepoNames` and `AllRepoNames()`. Every existing `range w.RepoNames` then excludes outer repos without being touched, and that is the right default.
3. **Resolution:** `ResolveAlias` checks all repos. `FuzzyMatchRepos` tries core first, then outer. `ResolveRepo`'s `c.Config.Repos[repo]` check already covers outer repos.
4. **Active outer:** add `w.ActiveOuterRepos()`, which returns outer repos whose repo dir has any worktree other than `.ground`. Status and debrief use `RepoNames + ActiveOuterRepos()`.
5. **`--outer` flag** on status, mc, rg and setup, mapped to `AllRepoNames()`.
6. **Lazy clone:** add `ensureCloned(repo)` in `cli`, called from `ResolveRepo` (and from rg's selector resolution) when an outer repo's bare dir is missing. It reuses `cloneRepos` and `runAfterCreateHooks`.
7. **Display:** in status and mc, show an active outer repo with a dim `outer` marker so it's obvious why it appeared.
8. **Docs:** add `tier` to MANUAL.md's config reference, plus a short "Outer repos" section.

Tests follow the existing patterns: config parse/merge tests, `SearchRoots` tests for the rg defaults, status golden tests covering an active and an inactive outer repo, and a resolution test showing the fuzzy fallback.

## Decisions

- **Read-only enforcement is out of scope.** `ws lift` on an outer repo behaves normally. A `lift = false` knob can be a separate change if a team wants a hard stop.
- **The flag is a fixed `--outer`, not `--<tier>` or `--tier <name>`.** Tiers are a closed set of two values defined by `ws`, not by users, so there is exactly one thing to opt into. `--outer` is also the exact word in the config, which makes it easy to find. If user-defined groups arrive later, they get their own `--group <name>` and `--outer` stays as is.

## Alternatives considered

- **Boolean `outer = true`.** Simpler, but `tier` leaves room for a third class later without a schema change. The cost is the same either way.
- **Free-form groups/tags (`groups = ["platform"]`, `--group platform`).** More flexible, but it adds questions about which groups are hidden and how they interact. Most of the value is in the visible/hidden split. Groups can be added later on top of tiers.
- **A separate `[outer.*]` table.** Keeps outer repos visually apart in `ws.toml`, but duplicates the schema and makes promoting a repo in `ws.local.toml` awkward. One table with a field is easier.
- **A separate workspace for platform repos.** This is what happens today. It loses `rg`, `jump` and capsules across the boundary.

## Open questions

1. **Fetch for active outer repos.** Should status refresh fetch an active outer repo? Probably yes, since you have a branch there, but it's worth checking against how big platform repos are.
2. **`ws upgrade`.** When a repo moves from core to outer, should upgrade offer to remove its clone? Proposal: no. Report the tier change in the diff summary and leave files on disk alone.
