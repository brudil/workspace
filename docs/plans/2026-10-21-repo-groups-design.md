# Repo Groups — In the Workspace, Out of the Way

**Date:** 2026-10-21
**Status:** Approved — implemented

## Problem

A team's `ws.toml` lists the repos the team owns, and that works well. But teams also rely on repos they don't own: platform repos, shared infra, design systems, other teams' services. You read these far more often than you change them. Today they live outside the workspace. They get cloned somewhere else by hand and kept up to date by hand, and none of the `ws` tooling can reach them: `ws rg` can't search them, `ws jump` can't go to them, and the rare `ws lift` for a platform PR doesn't work.

Adding them to `[repos]` as normal entries doesn't work either. Every command that covers all repos would then include them. `ws` status gets longer, `ws rg` returns platform matches for every search, fetches get slower, and `init` clones repos that some people will never open.

## Idea

Each repo can belong to one **group**, and a group can be **hidden**. Hidden repos are defined in `ws.toml` like any other repo, but commands only include them when you ask for them. Groups also work as a selector for any slice of the workspace (`product`, `tooling`, `platform`).

> **The rule:** naming a hidden repo always works. Commands that cover every repo by default leave hidden groups out.

```toml
[groups.platform]
hidden = true

[repos.frontend]
group = "product"

[repos.platform-infra]
group = "platform"
aliases = ["infra"]
```

Ungrouped repos are always visible, and a group with no setting is visible, so existing configs don't change.

## Behaviour

### Naming a repo explicitly: always works

| Path | Hidden repo behaviour |
|---|---|
| Exact name or alias (`ws jump infra`, `ws lift infra fix`) | Resolves normally. |
| cwd detection (standing in `repos/platform-infra/...`) | Resolves normally. |
| `ws rg --repo infra` | Included. |
| Shell completions | Hidden names and aliases are offered too. |
| Fuzzy match | Only considers hidden repos when no visible repo matches, so a hidden repo never makes `ws jump f` ambiguous. |
| Interactive picker (no argument) | Visible repos only. |

### Commands that cover every repo

| Command | Default | `--group <name>` |
|---|---|---|
| `ws` / `ws status` (all formats) | Visible repos, **plus hidden repos with a capsule** | Exactly those groups |
| `ws mc` | Same as status | Same |
| `ws debrief` | Same as status | Same |
| mc fetch-all | Same as status | Same |
| `ws rg` | Visible repos, plus the hidden repo you're standing in | Adds the groups' repos to `--repo` |
| `ws setup`, `ws init` | Visible repos | `setup` only: clones exactly those groups |
| `ws upgrade` | Clones newly added visible repos and notes hidden ones | — |
| `ws doctor` | "Not cloned" checks cover visible repos only | — |
| IDE files | Already driven by boarding. No change. | — |

`--group` is repeatable and comma-separated, and selects groups whether they're hidden or not. An unknown group name is an error listing the known groups.

**Hidden repos with capsules.** A capsule in a hidden repo means you're working there right now. Status, mc and debrief show the repo, marked `· platform (hidden)`, so you don't lose track of the branch. Once it's debriefed, the repo drops out of view again. JSON status has `group` and `hidden` fields, and the LLM format shows `group: platform, hidden`.

### Cloning: on first use

`init`, `setup` and `upgrade` skip hidden repos. `jump`, `lift`, `dock` and `rg` clone a repo that has no directory yet, with progress on stderr so stdout stays clean for the shell wrapper and ripgrep output. `ws group show <name>` clones the group's missing repos, and `ws setup --group <name>` clones a group up front.

### Personal choice: `ws group` and `ws.local.toml`

The team config sets the default. Each person can change it for themselves:

```bash
ws group                  # list groups, shown/hidden, and their repos
ws group show platform    # writes [groups.platform] hidden = false, clones its repos
ws group hide product     # writes [groups.product] hidden = true
ws group reset platform   # removes the override; team default applies
```

`ws.local.toml` can also override a repo's `group`, so a person can hide one repo by moving it into a group of their own (`[repos.mobile] group = "mine"`) and hiding that group.

## Implementation

- **Config:** `RepoConfig.Group`; `Config.Groups` / `LocalConfig.Groups` of `GroupConfig{Hidden *bool}`. The pointer lets a local `hidden = false` override a team `true`. `Merge` applies local group visibility and repo group. `Load` rejects group names that are empty or contain commas or spaces (they couldn't be passed to `--group`).
- **Workspace:** `RepoNames` still lists every repo, so name resolution and existing callers are unchanged. New: `RepoGroups`, `HiddenGroups`, `SelectGroups` (backs `--group`), `DefaultRepoNames()` (visible, or the selection), and `ListedRepoNames()` (default plus hidden repos with capsules). Each command that covers every repo now uses one of these two.
- **CLI:** `addGroupFlag` / `applyGroupFlag` helpers; `ensureCloned` for clone-on-first-use; `ws group` with `show`, `hide`, `reset`. The root command also takes `--group`, so `ws --group product` works like status.
- **Tests:** config parse/merge/save, workspace selection and fuzzy fallback, `SearchRoots` scoping, and CLI integration tests. Clone-on-first-use is tested without the network: the fixture redirects GitHub URLs to local source repos with `GIT_CONFIG_*` `insteadOf`.

## Decisions

- **Groups, not tiers.** An earlier draft had a fixed `tier = "core" | "outer"`. Groups give the same visible/hidden split, and also let you select internal slices like `product` and `tooling`.
- **One group per repo.** With several tags you have to decide what happens when a repo is in one visible group and one hidden group. A single `group` keeps visibility obvious. It can become a list later without breaking configs.
- **Visibility is per group, not per repo.** A personal override applies to a whole group. Hiding one repo is done by moving it into a group you hide.
- **`--group` restricts, it doesn't add.** `ws --group platform` is status for platform only. On `rg`, it combines with `--repo` because both are explicit selections.
- **No read-only enforcement** in this change. `lift` on a hidden repo behaves normally. A `lift = false` knob can come later if a team wants a hard stop.
- **No "show everything" flag.** `ws group show` and `--group` cover the real cases, and `--all` is already taken on `rg`.

## Open questions

1. **Changing a repo's group in `ws upgrade`.** When a repo moves into a hidden group, upgrade leaves its clone alone and doesn't report the change. Reporting group changes in the diff summary would be a small follow-up.
2. **Multiple groups per repo**, if teams find themselves wanting both `platform` and `tooling` on one repo.
