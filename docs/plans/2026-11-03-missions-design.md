# Missions: Workspace Owns the Work, Agents Run It

**Date:** 2026-11-03
**Status:** Phases 1–3 implemented (#8, #9, deck crew). Cursor CLI details pending local verification, see `2026-11-04-deck-crew-cursor-checklist.md`.

## Problem

Today the flow is **agent → work**. An agent starts somewhere, runs `ws lift`, and ws ends up with capsules nobody owns. `ws status` shows every capsule in the workspace. With several agents running there's no way to see what belongs to whom, which work is still going, or which capsules were left behind by a session that has ended.

The first instinct was to make ws recognise agent sessions (harness env vars, hooks, walking the process tree). That's fragile, and Cursor CLI, our first target, makes it hard: it doesn't expose its conversation id to shell commands, `CURSOR_AGENT` isn't reliably set, and some of its hooks don't fire.

## Idea

Flip the relationship: **workspace → work → agents.**

A **mission** is a durable unit of work that ws owns. It has a brief, one or more capsules, and a log. Agents are **crew**: ws starts them *for* a mission, and they come and go over the mission's life. ws doesn't need to identify an agent, because it launched the agent and already knows which mission it's for.

The entry point is a new full-screen TUI, **`ws deck`**: the focused agent's terminal on the left, the missions sidebar on the right.

> **The rule:** missions are an additive layer. Nothing that exists today changes its default behaviour or output. If you never run `ws deck` or `ws mission`, ws behaves exactly as it does now.

## Principles

1. **Additive only.** `ws status`, `ws mc`, `lift`, `dock`, `burn`, `debrief` and `FOR_LLMS.md` rules keep working as they do. Mission awareness is either a new command or happens only when `WS_MISSION` is set.
2. **Capsules stay the primitive.** A mission refers to capsules and doesn't replace them. A capsule can belong to no mission, which is today's world.
3. **The brief and log outlive agents.** Handing work to another agent or person goes through mission files, not an agent's memory.
4. **ws starts the agents.** Identity comes from ws provisioning the agent, not from detecting it. Detection stays a possible future fallback.

## Concepts

| Term | Meaning |
|---|---|
| **Mission** | A unit of work: slug, title, state, brief, capsules, crew, log. |
| **Brief** | Markdown: goal, constraints, acceptance criteria, notes. Written by the human, read by agents, and agents may add to it. |
| **Crew** | A record of each agent session run for the mission: harness, conversation id, start and end time, how it ended. |
| **Log** | Append-only events: created, capsule lifted or docked, agent started or ended, note, PR opened, state change. |
| **Deck** | The TUI that runs missions and their crew. |

### Mission state

`draft → active → review → landed → archived`, plus `parked`.

- `draft`: brief only, no capsules yet.
- `active`: has capsules.
- `review`: every capsule has an open PR. Derived from the PR data `collectStatusData` already fetches.
- `landed`: every PR is merged. Derived the same way.
- `archived`: capsules are burned and the files are kept for history.
- `parked`: set manually, for work that's paused.

Only `draft`, `parked` and `archived` are stored. The other states are computed, so they can never drift from git or GitHub.

### Multi-repo missions

A mission can own capsules in several repos, usually with the same branch name (for example `fe/auth-fix` and `api/auth-fix`). This is the main reason to make missions first-class in a multi-repo tool. `FOR_LLMS.md`'s rule "one capsule, one concern" still applies to agents outside a mission. Inside one, the rule becomes "one mission, one concern" (see [Agent guidance](#agent-guidance)).

## Storage

```
<workspace-root>/.ws/missions/
  auth-fix/
    mission.toml     # title, state override, capsules, crew
    brief.md
    log.jsonl
```

```toml
title = "Fix token refresh race"
created = 2026-11-03T10:12:00Z
state = "active"          # only draft | parked | archived are stored, see above

[[capsules]]
repo = "frontend"
name = "auth-fix"

[[capsules]]
repo = "backend"
name = "auth-fix"

[[crew]]
id = "c1"
harness = "cursor"
conversation = "b3f1…"     # harness-native id, used for resume
started = 2026-11-03T10:14:02Z
ended = 2026-11-03T11:40:17Z
exit = "ok"
```

- Kept local for v1. The workspace root is a clone of the config repo, so `.ws/` must go in `.git/info/exclude`. `ws deck` adds it on first run, and `ws doctor` checks it.
- Looking up a capsule's mission means scanning `mission.toml` files. There will be at most tens of missions, so no index is needed.
- If a capsule a mission refers to is burned outside the deck, the mission shows it as missing rather than erroring.

## Entry point: `ws deck`

A new command. It doesn't share a model with `ws mc`, but it reuses `internal/workspace`, the status collection and the styles.

```
┌───────────────────────────────────────────────────────┬──────────────────────────┐
│ cursor · auth-fix · frontend/auth-fix          c1 ●   │ MISSIONS              [n]│
│───────────────────────────────────────────────────────│                          │
│                                                       │ ▸ auth-fix        active │
│   (embedded agent terminal: cursor-agent running      │     fe/auth-fix  ↑2 ●    │
│    in a PTY, full colour, interactive)                │     api/auth-fix PR#88   │
│                                                       │     crew c1 ● live       │
│                                                       │                          │
│                                                       │   billing-export  review │
│                                                       │     api/billing  PR#91 ✓ │
│                                                       │     crew —               │
│                                                       │                          │
│                                                       │   onboarding-copy  draft │
│                                                       │                          │
│                                                       │ ─ loose capsules (3) ─   │
├───────────────────────────────────────────────────────┴──────────────────────────┤
│ ^g prefix · ^g s sidebar · ^g n new · ^g r run · ^g z zoom · ^g ? help           │
└──────────────────────────────────────────────────────────────────────────────────┘
```

### Layout

- **Main pane (left):** the selected mission's live agent terminal. If the mission has no live crew, it shows the brief rendered with glamour, plus a "run agent" prompt.
- **Sidebar (right, about 26 columns, can be hidden):** missions grouped by state. Each mission expands to show its capsules (git and PR badges like status) and crew (live, ended, idle). A collapsed "loose capsules" section at the bottom keeps the rest of the workspace visible without promoting it to a mission.
- **Status line:** prefix-key hints and the focused crew member.

### Input model

- **Focus is either the terminal or the sidebar.** When the terminal has focus, every key goes to the agent, except the **prefix key** (`ctrl+g` by default, configurable). Same idea as tmux's `ctrl+b`.
- With the sidebar focused: `j/k` to move, `enter` to focus that mission's crew, `n` new mission, `r` run agent, `e` edit brief in `$EDITOR`, `l` lift capsule into mission, `a` attach existing capsule, `x` archive.
- `prefix z` **zoom/passthrough:** hides the sidebar and pipes PTY bytes straight to the real terminal with no emulation. This is the fidelity escape hatch (see below).

### Embedded terminal

- `creack/pty` per crew member. The emulator is `charmbracelet/x/vt` (we already depend on `charmbracelet/x/*`). Wrap it behind a small `termpane` package so it can be swapped for `hinshun/vt10x` or a tcell-term style widget.
- **Rendering:** PTY output marks the pane dirty, and the deck renders at most every 16ms (60fps cap). Only the focused pane is emulated *and* rendered. Background crew keep emulating so their state is correct when you switch to them, but they're never drawn.
- **Performance:** a 200×50 grid redrawn at 60fps is cheap in Go. Bubble Tea v1's renderer (what we use now) diffs line by line, and streaming agent output touches only a few lines per frame. Measure first. If it's not enough, the deck moves to Bubble Tea v2's cell renderer, which can live alongside v1 under a different import path.
- **Fidelity is the real risk, not speed:** alt screen, bracketed paste, mouse, kitty keyboard protocol, OSC sequences, wide characters. Two mitigations:
  1. Keys go into the PTY through the emulator's input encoder, which respects the modes the child process has set, rather than us re-encoding Bubble Tea `KeyMsg`s by hand. Check what `x/vt` provides.
  2. Zoom/passthrough mode skips emulation completely, so a rendering bug is never a blocker.

### Process lifetime

- **v1:** crew processes are children of the deck. Quitting the deck with live crew asks first, then sends SIGHUP. Every crew entry stores its harness conversation id, so `r` on a mission with ended crew offers **resume**.
- **Later:** a background `ws deck` server on a unix socket in `.ws/` owns the PTYs, and the TUI becomes a client. That gives detach and reattach (and survives closing the terminal). The `termpane` boundary is designed so this can be added without a rewrite.

## Running an agent (Cursor CLI first)

`r` on a mission, or `ws mission run <slug>` from a shell, which opens the deck focused on it:

1. Choose the working directory: the mission's first capsule. If there are none, prompt to lift one, defaulting to the mission slug as the branch name.
2. Set the environment: `WS_MISSION=<slug>`, `WS_CREW=<crew-id>`, plus everything the deck itself inherited.
3. Get a conversation id. Use `cursor-agent create-chat` if it returns one (to verify), so ws knows the id *before* starting the agent. Fall back to a `sessionStart` hook that writes `conversation_id` into the crew entry.
4. Start `cursor-agent` in the PTY, with an initial prompt built from the brief and the mission context (capsule paths, `ws mission` commands).
5. Resume: `cursor-agent --resume <conversation>` (to verify) with the same `WS_MISSION` and a new `WS_CREW`.

Harness support goes behind a small `harness` interface (`Command(mission) []string`, `Resume(conv)`, `ConversationID()`), so Claude Code and Codex can be added later without touching the deck.

**Spike before building:** in a real `cursor-agent` session, confirm (a) `WS_MISSION` reaches the agent's shell commands, (b) passing an initial prompt and `--resume <id>` both work, (c) whether `create-chat` gives us an id up front.

## Agent-facing CLI: `ws mission`

Agents work through the shell, so the deck needs a small CLI. Every command reads `WS_MISSION` by default and otherwise resolves the mission from the current directory (cwd → capsule → mission).

```
ws mission show [-f llm]        # brief, capsules (+ paths), state, recent log
ws mission note "<text>"        # append a note to the log
ws mission new "<title>"        # human use; opens $EDITOR for the brief
ws mission run <slug>           # open deck on mission, start crew
ws mission list                 # all missions + derived state
ws mission attach <repo> <cap>  # add an existing capsule to a mission
```

### Additive hooks into existing commands

These are the only places existing commands notice missions. Each does nothing unless a mission is in context:

| Command | When `WS_MISSION` is set |
|---|---|
| `ws lift`, `ws dock` | Also add the new capsule to the mission and write a log event. Output is unchanged apart from one `→ mission auth-fix` line. |
| `ws burn` | Log the burn to the mission. |
| `ws status -f llm` | Print a `## Mission` header with the brief summary above the normal output. The normal output is unchanged. |

Without `WS_MISSION`, all of the above behave exactly as they do today, even when run inside a capsule that belongs to a mission. Scoping `ws status` by cwd could be a later opt-in flag.

## Agent guidance

Add a **conditional** section to `FOR_LLMS.md`. It doesn't change any existing rule:

> **If `WS_MISSION` is set**, you are crew on a mission. Run `ws mission show` first. The mission may span several capsules across repos, and you may work in any of them. Don't touch capsules outside the mission. Record significant decisions with `ws mission note`. Create new capsules with `ws lift`, which attaches them to the mission automatically.

## What doesn't change

- `ws`, `ws status` (text/json), `ws mc`, `ws debrief`, `ws jump`, `ws rg`, boarding, silo.
- The `FOR_LLMS.md` rules for agents not on a mission.
- `ws.toml` / `ws.local.toml` schemas. Missions don't live in config.

`ws debrief` doesn't archive missions in v1. The deck offers "archive landed missions" itself, which burns their capsules through the existing burn path.

## Phases

1. **Missions core:** `internal/mission` (load/save/log, derived state, cwd resolution), the `ws mission` CLI, `WS_MISSION` hooks in lift/dock/burn/status-llm, `FOR_LLMS.md` section. Useful on its own with agents started by hand.
2. **Deck shell:** `ws deck` with the sidebar, brief view, and mission create/edit/attach. The main pane only shows the brief at this stage.
3. **Embedded crew:** `termpane` (pty + vt), Cursor harness, run, resume, prefix keys, zoom/passthrough.
4. **Later:** deck server (detach/reattach), more harnesses, starting a mission from a GitHub issue/PR, parallel crew in one mission, possibly syncing missions through the config repo.

## Open questions

1. **Name:** `ws deck`? Other options: `ws bridge`, `ws flight`. `ws mc` is taken and stays as it is.
2. **Implicit missions:** should `ws lift` outside a mission ever create one? This draft says no, so it stays additive, and loose capsules sit in their own sidebar section.
3. **Parallel crew:** allow two live agents in one mission in v1? This draft allows it but warns if two crew members share a capsule.
4. **Brief ownership:** can agents edit `brief.md`, or only add notes? This draft says notes only, and the human owns the brief.
5. **Prefix key:** `ctrl+g` clashes with some editors' cancel key. Make it configurable in `ws.local.toml`.
6. **Bubble Tea v1 vs v2** for the deck: start on v1 and measure, or start the new entry point on v2 because it's new code anyway?

## Risks

- **Terminal emulation fidelity.** Mitigated by the swappable `termpane` and passthrough zoom.
- **Cursor CLI surface** (initial prompt, `--resume`, `create-chat`) is unverified. Mitigated by the spike before phase 3. Phases 1 and 2 don't depend on it.
- **Agents dying when the deck quits** in v1. Mitigated by resume, and properly fixed by the deck server later.
