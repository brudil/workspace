# Missions: the Workspace Owns the Work, Agents Do It

**Date:** 2026-11-03. What and why rewritten 2026-11-05.
**Status:** Phases 1–6 built (see [Built so far](#built-so-far)), and the first part of phase 8; phases 7–9 are next and close the gap between the build and the idea. Cursor CLI verified locally on 2026-11-05, except the orchestrator end to end (`2026-11-04-deck-crew-cursor-checklist.md`). Claude Code verified end to end in every role on 2026-11-05 (claude 2.1.318).

## Why

**Work has no home above the agent.** ws has capsules, where code changes happen, and agents, who change it. Nothing represents the piece of work itself. A feature that spans two repos, a design doc, a PR and three agent sessions exists only as scattered capsules and in the context of whichever session is open. When that session ends, so does the only thing that knew how the parts fit. `ws status` lists capsules nobody owns, and nothing says which work they belong to, whether it's still going, or what's left.

**Every agent starts by rediscovering.** Several agents can work on the same piece of work, but each starts cold. It reads the code again, finds the issue again, works out which capsules matter, and learns again what the last agent already tried. Nothing carries what one agent learned to the next.

**Agents are the wrong unit to organise around.** The first instinct was to make ws recognise agent sessions (harness env vars, hooks, the process tree) and hang work off them. That's fragile: Cursor doesn't expose its conversation id to shell commands, `CURSOR_AGENT` isn't reliably set, and hooks don't always fire. Worse, it puts the most disposable thing at the centre. Sessions end, crash, run out of context and change vendor; the work carries on.

## What a mission is

> **Work outlives agents.** A mission is the lasting record of one piece of work: what it's for, where it happens, and what has happened and been produced, so any agent, or person, can pick it up cold.

This flips the relationship from agent → work to **workspace → mission → agents**. A mission has three layers:

| Layer | What it holds | In ws |
|---|---|---|
| **Intent** | What the work is for, and what done looks like | The brief: goal, context, constraints, acceptance, and optionally stages and check-ins |
| **Place** | Where the work happens | Capsules, possibly across several repos |
| **Memory** | What happened, and what it produced | The log (who did what, when) and artifacts (ADRs, docs, PRs, handoffs: what one agent leaves for the next) |

Agents act on a mission. Each is one contributor to the work, not its container, and they're interchangeable. ws starts them *for* a mission, so it never has to identify them. They play three roles:

- **Briefing agent:** turns a request ("fe issue 45", a sentence) into intent and place. It writes the brief and lifts the capsules, then hands over.
- **Crew:** do the work, one part at a time. They read the mission when they start and add to its memory as they go.
- **Orchestrator:** a headless agent that runs between crew. It reads the intent and memory and decides what's next: dispatch new crew, resume one, flag the human, or suggest concluding.

Which agent CLI plays each role (Cursor, Claude Code, a shell) is configuration (`[crew]`, `[briefing]`, `[orchestrator]`), and changing it changes nothing about the mission.

### From brief to automation

The brief sits at the top of the mission, and the goal is for as much as possible to follow from it automatically. A human states the intent once, or a briefing agent drafts it from a request. The orchestrator then moves the work through it stage by stage. It comes back to the human only at check-ins the brief names, for decisions only they can make, or after a few dispatches in a row without them.

The log and artifacts are what make that possible: they're how the orchestrator, and each new crew, know where the work stands without relying on anyone's memory.

### Log and artifacts: the connective tissue

- **Log:** an append-only record of what did what, when. It covers crew starting and ending, capsules added, notes, asks, done reports, dispatches and the orchestrator's decisions. Agents read it to catch up; the orchestrator reads it to decide.
- **Artifacts:** what the mission produces and passes along. That means proposals, ADRs, notes, docs, PRs, and the handoff each crew leaves. The log says *that* something happened; artifacts are *what was made*, in a form the next agent can read. An artifact can be a file on a repo's default branch, a file in one of the mission's capsules, a private file in the mission's own folder, a PR or issue, or a link. The mission only points at it.

The entry point is **`ws deck`**, a full-screen view of missions and the agents working on them. Agents also reach their mission through `ws mission` from their shell.

> **The rule:** missions are an additive layer. Nothing that exists today changes its default behaviour or output. If you never run `ws deck` or `ws mission`, ws behaves exactly as it does now.

## Principles

1. **Additive only.** `ws status`, `ws mc`, `lift`, `dock`, `burn`, `debrief` and the `FOR_LLMS.md` rules keep working as they do. Mission awareness is either a new command or happens only when `WS_MISSION` is set.
2. **Capsules stay the primitive.** A mission refers to capsules and doesn't replace them. A capsule can belong to no mission, which is today's world.
3. **The mission is the memory.** Handing work to another agent or person goes through the mission's brief, log and artifacts, never through an agent's context. If the next agent would need to know something, it belongs in the mission.
4. **Agents are interchangeable.** ws starts them, so identity comes from provisioning rather than detection. Any harness can play any role it supports, and nothing in a mission depends on which agent did which part.
5. **Automate from intent, check in where the brief says.** The orchestrator moves work forward from the brief. It acts only through `ws` commands, which check what it asks for, and it hands back to the human at named check-ins and limits.

## Concepts

| Term | Meaning |
|---|---|
| **Mission** | A unit of work: slug, title, state, brief, capsules, crew, log, artifacts. |
| **Brief** | Markdown: goal, context, constraints, acceptance, and optionally `## Stages` with check-ins. The human owns it; a briefing agent may draft it before the mission is ready. Crew add to the log, not the brief. |
| **Capsule** | A worktree, unchanged. A mission owns some; others belong to no mission. |
| **Crew** | A record of each agent session run on the mission: role, harness, conversation id, capsule, handoff, start and end, and how it ended. |
| **Briefing agent** | Crew in the briefing role: turns a request into a brief and capsules, then runs `ws mission ready`. |
| **Orchestrator** | A headless agent run when crew report done or end by themselves. It decides what's next and acts only through `ws mission` commands. |
| **Log** | Append-only events: what did what, when. |
| **Artifact** | A pointer to something the mission produced or relies on: a file (on a default branch, in a capsule, or private to the mission), a PR or issue, or a link. It has a kind, such as proposal, ADR, note or handoff. |
| **Harness** | An agent CLI ws can launch in a role: `cursor`, `claude`, `shell`. |
| **Deck** | The TUI for running missions and their crew. |

### Mission state

`briefing → draft → active → review → landed → archived`, plus `parked`.

- `briefing`: a briefing agent is turning the request into a brief and capsules. `ws mission ready` ends it.
- `draft`: nothing live yet.
- `active`: has capsules.
- `review`: every capsule has an open PR. Derived from the PR data `collectStatusData` already fetches.
- `landed`: every PR is merged. Derived the same way.
- `archived`: capsules are burned and the files are kept for history.
- `parked`: set manually, for work that's paused.

`briefing`, `parked` and `archived` are set explicitly. The others are worked out from git and GitHub each time, so they can never drift.

### Multi-repo missions

A mission can own capsules in several repos, usually with the same branch name (for example `fe/auth-fix` and `api/auth-fix`). This is the main reason to make missions first-class in a multi-repo tool. `FOR_LLMS.md`'s rule "one capsule, one concern" still applies to agents outside a mission. Inside one, the rule becomes "one mission, one concern" (see [Agent guidance](#agent-guidance)).

> **From here to [What doesn't change](#what-doesnt-change)** is the original design for phases 1–4. MANUAL.md ("Missions") describes what was built, and where the two differ, the manual is right. For example, crew run in a deck server rather than as children of the deck, the deck runs on Bubble Tea v2, crew are typed into or given their prompt depending on the harness, and Claude Code is a harness alongside Cursor.

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

## Built so far

1. **Missions core** (#8): `internal/mission` (load and save, log, derived state, finding the mission from cwd), the `ws mission` CLI, `WS_MISSION` hooks in lift, dock, burn and `status -f llm`, and the `FOR_LLMS.md` section.
2. **Deck shell** (#9): `ws deck` with the missions sidebar, the mission view, and creating, editing and attaching missions.
3. **Embedded crew** (#10, #11): `termpane` (a PTY plus a terminal emulator), prefix keys, several crew per mission, scrollback, raw zoom, and a deck server so crew outlive the deck.
4. **Briefing, attention and the orchestrator** (#12–#16):
   - briefing agents that turn a request into a ready mission
   - crew shown as working or waiting, with ask and done signals
   - artifacts as links
   - missions raised by crew
   - the orchestrator with dispatch, resume-with, flag and conclude
   - the Overview, which groups missions by what they need from you
5. **Harness roles:** Cursor and Claude Code can each play every role (crew, briefing, orchestrator), chosen in `[crew]`, `[briefing]` and `[orchestrator]`. Claude Code's orchestrator is limited to an allowlist of `ws` and read-only commands; Cursor's runs with `--force`.
6. **Artifacts** (phase 6 below): references to files (default branch, capsule, private), PRs and links, with kinds and a state worked out each time; `ws mission artifact add/rm`, `--read` on dispatch, the orchestrator's prompt, and `[missions] records`.

## Next phases

Where the build still falls short of the idea, in the order to close the gaps.

### 6. Artifacts as connective tissue

Missions are personal: `.ws/` stays on one machine. An artifact is anything the mission produces or relies on, wherever it lives, and the mission keeps a pointer to each one. Before this phase an artifact was a URL and a label, and the orchestrator never saw them.

| Location | Reference | Resolves to | Shared? |
|---|---|---|---|
| File on a repo's default branch | `records:adr/0042-cache-tokens.md` | `repos/records/.ground/adr/0042-cache-tokens.md`, and its GitHub link | With anyone who has the repo |
| File in one of the mission's capsules | `records/cache-adr:adr/0042-cache-tokens.md` | The capsule's copy while it's a draft or in review; the default-branch file once the capsule's PR has merged | Once pushed |
| Private mission file | `mission:spike-notes.md` | `.ws/missions/<slug>/files/spike-notes.md` | No |
| PR or issue | `api#88` | Its GitHub page | Yes |
| Link | `https://…` | Itself | Depends |

**Stored** in `mission.toml`, as one `[[artifacts]]` table each:

```toml
[[artifacts]]
ref = "records/cache-adr:adr/0042-cache-tokens.md"
kind = "adr"
label = "Cache token lookups"
crew = "c2"                     # who recorded it; empty for you
added = 2026-11-05T15:00:00Z
```

Older entries with only `url` read as links. A reference is recorded once; adding it again updates its kind and label. Where an artifact is *now* is never stored. Like mission state, it's worked out from git and GitHub each time, so it can't drift.

**Kinds:** `proposal`, `adr`, `note`, `doc`, `plan`, `handoff`, `pr`, `issue`, `link`, or any lowercase word. If none is given: `pr` for PRs, `link` for URLs, `doc` for files.

**Adding:** `ws mission artifact add <what> [--kind adr] ["label"]`. `<what>` can be:
- a reference (as in the table above)
- a URL: a workspace repo's PR URL becomes `repo#N`
- a plain path: ws classifies it by where it sits, in one of the mission's capsules, a `.ground`, or the mission's `files/`

Anything else is refused, with what to do instead: copy it into the mission's files, or attach the capsule. `ws mission artifact rm <ref>` removes one.

**State**, shown with every artifact:
- **Capsule file:** `draft` (its capsule has no PR), `in review` (open PR), `landed` (the PR has merged, or the capsule is gone and the file is on the default branch; from then on it resolves there), or `missing`.
- **Default-branch file:** `landed`, or `missing` if it isn't in `.ground` (which may be behind origin).
- **Mission file:** `private`, or `missing`.
- **PR:** `open` or `merged` when it's one of the capsules' PRs, else just `pr`.
- **Link:** `link`.

**Who sees them:**
- `ws mission show -f llm` lists each artifact with its kind, state, the local path to read and its link, and names the mission's private files folder.
- The orchestrator's prompt has the same list.
- `ws mission dispatch --read <ref>` (repeatable) adds *Read these first*, with paths, to the handoff.
- The deck's mission view groups artifacts by kind.
- Crew are told to record what they make.

**Records repo:** `[missions] records = "records"` in `ws.toml` names where shared proposals, ADRs and notes go. `ws mission show -f llm` tells crew and briefing agents to lift a capsule there when a stage produces one. The repo's README or AGENTS.md keeps its conventions.

**Review gate (decided: build on it).** The next stage may read an artifact that's still a draft or in review, from its capsule. If a stage should wait for a record to land, the brief says so as a check-in ("check in with me once the proposal is merged"), and the orchestrator flags instead of dispatching.

### 7. Handoffs that carry what was learned

`ws mission done` takes one line. What the next agent needs most is where things live, what was tried and didn't work, and what's left. Today the orchestrator rebuilds that from the end of a transcript.

- Crew finish with a short, structured handoff: *done*, *learned*, *left*, *read first*. It's kept as a `handoff` artifact and summarised in the log.
- New crew get the latest handoffs for their capsules ahead of the brief, so they start where the last crew stopped instead of rediscovering.

### 8. Mission state decides, not processes

The deck and dispatch still reason about processes: a capsule is busy while a crew's terminal is open. So crew that reported done, but whose session is still open, block dispatching the next stage into their capsule. This happened with both Cursor and Claude Code.

- **Built:** crew that have reported done (or finished briefing) have handed the work back, so they don't make a capsule busy. Dispatching into that capsule ends their session (it can still be resumed) and logs it, and launching there from the deck offers to.
- The mission view becomes the deck's main surface: intent, stage progress, artifacts and log, with crew terminals one step away.

### 9. Stages ws understands

The orchestrator rereads the brief's prose on every run and decides from it. The mechanical decisions shouldn't depend on a model.

- ws parses `## Stages` as an ordered list. A stage can name what it produces and whether to check in with the human after it. ws records which stage each crew was dispatched for and when it reported done.
- ws shows stage progress and handles the mechanical cases itself: the next stage is dispatched, and a check-in becomes a flag. The orchestrator is asked only for judgement: is the output good enough, does the plan need to change, is the mission finished.
- The brief's acceptance criteria are what a suggested conclusion is checked against.

### Later

- Starting a mission from a GitHub issue or PR directly.
- More harnesses (Codex).
- Sharing missions themselves, beyond the records they produce, if personal stops being enough: syncing through the config repo, or a branch per mission.

## Open questions

1. **When is a mission done?** With every output arriving by PR, records included, "landed" (every PR merged) now covers docs-only missions too. What's left: should the brief's acceptance criteria also be checked before a mission counts as done, or is merged enough?
2. **How autonomous by default?** "Brief once, come back to a PR" and "approve each stage" want different defaults for `max_dispatches`, for check-ins, and for whether dispatched crew launch at once (`dispatch = "auto"`) or wait (`"ask"`).

Settled:
- **Missions are personal**, kept in `.ws/` on one machine. What they produce is shared through repos: proposals, ADRs and notes in the records repo, persisted on the default branch. Artifacts can point at those files and at anything else (see phase 6).
- The command is `ws deck`.
- `ws lift` never creates a mission by itself.
- Several crew can work on a mission; the deck asks before two share a capsule.
- The human owns the brief: a briefing agent writes it before `ready`, and the orchestrator may rename a mission still titled with its request.
- The prefix key is `ctrl+g`; it isn't configurable yet.
- The deck runs on Bubble Tea v2.

## Risks

- **Terminal emulation fidelity.** Mitigated by the swappable `termpane` and passthrough zoom. One known bug: `charmbracelet/x/ansi` ends an OSC string at a raw `0x9C` byte, which is part of the UTF-8 for `✓ ✗ ✳` and much CJK. Agents' titles containing those characters are cut short (Claude Code's `✳` title shows as `�`), and the rest of the sequence can leak into the pane. It needs an upstream fix.
- **Automation decided from prose.** The orchestrator's decisions vary by model and prompt. An early Claude orchestrator gave up on the shell after one refused command, until the allowlist and prompt were fixed. Mitigated by acting only through `ws` commands that validate what they're asked, by the dispatch limit and flags, and by phase 9.
- **Orchestrator permissions differ by harness.** Claude Code's runs with `--permission-mode dontAsk` and an allowlist, with git limited to exact capsule paths. Cursor's print mode can't be limited and runs with `--force`, relying on its instructions.
- **Agent CLIs change underneath us.** Every assumption about a CLI lives in its harness file, marked VERIFIED with the version it was checked against.
