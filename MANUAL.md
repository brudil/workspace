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
  - [Groups](#groups)
  - [Searching](#searching)
  - [Missions](#missions)
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

If you're standing inside the capsule you're burning, the [shell wrapper](#the-ws-wrapper) steps you out to the workspace root before the worktree is removed, so your shell is never left in a directory that no longer exists.

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

Repos in hidden [groups](#groups) are always found by exact name or alias. Fuzzy matching only considers them when no visible repo matches, and the picker leaves them out.

### Groups

A repo can belong to one group. Groups let you keep repos in the workspace that you mostly read, such as another team's platform repos, without them crowding everything else. They also let you work with a slice of the workspace (`ws --group product`).

```toml
[groups.platform]
hidden = true

[repos.frontend]
group = "product"

[repos.platform-infra]
group = "platform"
aliases = ["infra"]
```

**Hidden groups** are left out of commands that cover every repo: `ws`/`status`, `mc`, `rg`, `debrief`, `setup`, `init`, `upgrade` and mission control's fetch-all. Naming a hidden repo always works (`ws jump infra`, `ws lift infra fix-alerts`, `ws rg --repo infra`), and shell completions include hidden repos.

- **Cloned on first use.** `init`, `setup` and `upgrade` don't clone hidden repos. The first `jump`, `lift`, `dock` or `rg` that needs one clones it.
- **Shown while you're working there.** A hidden repo with a capsule appears in status, mission control and debrief, marked `· platform (hidden)`. Once its capsules are debriefed, it drops out of view again.
- **Standing in one.** `ws rg` searches the hidden repo you're standing in, as well as the visible ones.

**`--group <name>`** limits a command to those groups, hidden or not. It is repeatable and comma-separated, and works on `ws`/`status`, `mc`, `rg`, `debrief` and `setup`. `ws rg --group` adds the group's repos to any `--repo` selection. `ws setup --group platform` clones a whole group up front.

**Your own choice.** The team sets defaults in `ws.toml`. Each person can override them in `ws.local.toml`:

```bash
ws group                  # list groups, whether each is shown, and their repos
ws group show platform    # show it for you, and clone its repos
ws group hide product     # hide it for you
ws group reset platform   # go back to the team default
```

To hide a single repo for yourself, put it in a group locally (`[repos.mobile] group = "mine"`) and hide that group.

### Searching

`ws rg` runs [ripgrep](https://github.com/BurntSushi/ripgrep) over the right worktree of each repo. Plain `rg` from the workspace root searches `.bare/`, `.silo/`, `.ground/` and every capsule, so every match comes back once per capsule. `ws rg` returns each match once, which matters most for agents whose context fills with duplicates.

```bash
ws rg useAuth                        # where I stand: my capsule for this repo, ground elsewhere
ws rg --repo fe,api useAuth          # restrict to repos (names, aliases, fuzzy)
ws rg --repo fe/login-fix TODO       # one specific capsule
ws rg --group platform Ingress       # a whole group, hidden or not
ws rg --ground -t go 'func Load'     # .ground everywhere
ws rg --all FEATURE_FLAG_X           # every worktree: who else is touching this?
ws rg -g 'src/**/*.ts' -l needle     # globs anchored per root
ws rg --roots                        # show what would be searched, run nothing
```

Everything that is not a `ws rg` flag is passed to ripgrep unchanged, so `-i`, `-t`, `-C`, `--json`, `--files` and friends all work. Scope flags are long-form only so they never shadow ripgrep's short flags. Parsing of scope flags stops at the first ripgrep argument or at `--`.

**Scope.** With no flags, the scope is "where I stand": if you are inside a capsule or `.ground`, that worktree is searched for its repo and `.ground` is searched for every other repo. Outside any worktree, `.ground` everywhere. `--ground` forces `.ground` for every repo, `--all` adds every capsule. `.bare` and `.silo` are never searched unless you name them explicitly (`--repo fe/.silo`).

`--repo` takes `name` or `name/worktree`, repeatable or comma-separated. Both halves resolve like any other repo or capsule argument, except that ambiguity is an error listing the candidates rather than an interactive picker, so the command is safe to call from scripts and agents.

**Output.** Paths are absolute, so each line says which repo and worktree the match came from, and you can open it from any directory. `ws` prints nothing of its own on success. Ripgrep's exit code is passed through: `0` matches, `1` none, `2` error.

**Globs.** Ripgrep matches a glob containing a `/` against the printed path, not against each search root, so a multi-root search would normally make `-g 'src/*.ts'` match nothing. `ws rg` expands such globs to one per root, keeping their anchoring. Basename globs (`-g '*.ts'`) pass through untouched. Globs from a ripgrep config file (`RIPGREP_CONFIG_PATH`) are not rewritten.

Do not pass positional paths; narrow with `--repo` and `-g` instead.

### Missions

A mission is a unit of work that the workspace owns: a brief, the capsules that carry it out (possibly across several repos), and a log of what happened. Capsules are still the building block. A mission just says which capsules belong to one piece of work, and remembers it after the agents and terminals that did the work are gone.

```bash
ws mission new "Fix token refresh race"     # creates it and opens the brief in $EDITOR
ws mission                                   # list missions (alias: ws mission list)
ws mission show fix-token-refresh-race       # brief, capsules, PRs, recent log
WS_MISSION=fix-token-refresh-race ws lift fe token-race   # new capsule joins the mission
ws mission attach -m fix-token-refresh-race api token-race   # add an existing capsule
ws mission note "Went with a mutex, not a channel"
```

**Which mission.** Commands that act on a mission take it from the slug argument or `-m/--mission`, then `$WS_MISSION`, then the mission owning the capsule you are standing in.

**`WS_MISSION`.** With it set, `ws lift` and `ws dock` attach the new capsule to that mission and print one extra `→ mission <slug>` line. `ws status -f llm` gains a short mission header. Without it, those commands behave exactly as they always have. Burning or debriefing a capsule that belongs to a mission logs the burn there. The capsule stays listed (as burned) so its PR still counts.

**State.** `draft` (nothing live yet), `active`, `review` (every capsule has a PR), and `landed` (every PR merged) are worked out from git and GitHub each time, so they never drift. `ws mission park`, `ws mission archive` and `ws mission reopen` set the two manual states. Archived missions are hidden from the list unless you pass `--all`.

**Brief.** `brief.md` is yours. `ws mission new --brief "..."` or `--brief-file path|-` writes it without an editor. Agents add to the log with `ws mission note` rather than editing the brief.

**The deck.** `ws deck` is a full-screen view of your missions. It opens on the **Overview**: a *What do you want to do?* field (`⏎`, then say it; a briefing agent turns it into a mission), everything that needs you and why, and what's been happening across every mission's log, newest first. Click a mission there to open it, or the field to start typing. The header and the terminal's tab title say how many missions need you. Terminals that show a program's status on its tab (the Program Status Protocol, OSC 7501) also see the deck as *blocked* while a mission needs you, with the most pressing reason, and *working* while crew work, launch or the orchestrator decides; when all is quiet the deck reports nothing.

The sidebar groups missions by what they ask of you, each shown by its title with a short status, and its live crew beneath it:

| Group | When | Status |
|---|---|---|
| **Needs you** | crew ask something or have gone quiet waiting for you, the orchestrator asks you something or queued crew for you to launch, it looks finished, or crew have worked on it and nobody is now | `? asks`, `◐ waiting`, `! asks you`, `→ next crew`, `✓ wrap up`, `idle` |
| **In progress** | crew working, crew done but still running, the orchestrator deciding, PRs in review | `● working`, `✓ done`, `◆ deciding`, `in review` |
| **Up next** | waiting to be briefed, briefed but never started, parked | `to brief`, `ready`, `parked` |

Landed and archived missions are behind a collapsed **Past** entry at the bottom, and capsules that belong to no mission behind **Loose capsules**; `⏎` opens either. Selecting a mission shows it on the left, in four tabs (`1`–`4`, or click one; the tab stays as you move between missions): **Summary** leads with what needs you and what to press, then a line each for the goal, crew, capsules, artifacts and the latest log; **Brief** is the rendered brief; **Work** is every capsule with its path, git and PR state, and every artifact; **Log** is the whole log, then every crew's history (when it ran, how it ended, what it was dispatched for). From there, `n` starts a new mission (see Briefing below), `N` creates one you name and brief yourself in `$EDITOR`, `e` edits the brief, `a` attaches a capsule, `d` detaches one (it asks which), `p` parks and `x` archives. `?` lists every key.

**Briefing.** `n` in the deck asks one thing: what's the mission? Answer however you like: `fe issue 45`, a pasted issue URL, or a sentence. The deck creates the mission in the `briefing` state and launches a briefing agent in the workspace root. Its only job is to turn your request into a ready mission:

1. Work out what you mean: repos and aliases from `ws status -f llm`, issues and PRs with `gh`, code in `.ground`.
2. Ask you about anything unclear.
3. Write the brief with `ws mission brief --set -`, and name the mission with `ws mission title`: a terse, plain-English name of at most 50 characters, which is how it reads in the deck.
4. Lift (or dock) the capsules the work needs.
5. Check with you, then run `ws mission ready`.

`ws mission ready` refuses until the brief is written and a capsule exists (`--no-capsules` if none is needed), can rename the mission (`--slug`), records the briefing agent as `briefed` and ends its session. The deck then lands on the ready mission, and `⏎` launches crew to do the work. A briefing agent asking you something puts its mission under **Needs you**; one nobody has started briefing waits under **Up next**. Stop a briefing early and `⏎` offers to resume it.

`ws mission new "<what it is>"` does the same from a shell, or from an agent: the mission waits to be briefed (`⏎` on it in the deck starts the briefing agent). Crew that spot something outside their mission are taught to raise it this way; the new mission records which mission and crew raised it. To create a mission ready, with a brief you write yourself, pass `--brief`, `--brief-file`, `--edit` (opens `$EDITOR`) or `--no-edit` (the template).

The same commands work by hand: `ws mission brief` prints or replaces a brief, `ws mission title` renames (at most 50 characters), and `ws mission ready` finishes a briefing. Crew can only change the brief or title while briefing; after that they belong to you (and the orchestrator, which names a mission still titled with the request it was raised with).

**Crew.** Press `⏎` on a mission to launch an agent on it. It starts in one of the mission's capsules, with `WS_MISSION` and `WS_CREW` set, and is told to read the mission with `ws mission show`, so the capsules it lifts join the mission. Each launch is recorded on the mission as crew (`c1`, `c2`, …) with how it ended. Crew get `.ws/bin` first on `PATH`, where `ws` and `workspace` point at the deck's own binary, so the commands they're told to run always match the deck's version.

- **Several crew.** Each crew gets its own row under its mission in the sidebar, saying what it's doing and what it was dispatched for. The mission row shows the mission; a crew row shows that agent's terminal. `⏎` on the mission launches more crew, in a capsule nobody is working in, and asks before two crew share one. Crew that have reported done (`✓`), or finished briefing, have handed their work back and don't count as working: launching in their capsule offers to end their session first (`R` resumes it later).
- **Focus.** `tab` (or `⏎`) on a crew row hands it the keyboard: every key and paste goes to the agent, except `ctrl+]`, which zooms (below). `ctrl+g` is the deck's prefix key while an agent has focus:

  | Key | Does |
  |---|---|
  | `ctrl+g tab` | Back to the sidebar (the agent keeps running) |
  | `ctrl+g [` | Scroll back through the agent's history (`k`/`j`, `ctrl+u`/`ctrl+d`, `g`/`G`, `esc`) |
  | `ctrl+g z` | Zoom: give the agent the whole real terminal, unemulated, until `ctrl+g z` again |
  | `ctrl+]` | Zoom in or out in one key, no prefix; from the sidebar it zooms the selected crew (or the mission's latest), and coming back out lands in its terminal |
  | `ctrl+g n` / `p` | Next / previous crew |
  | `ctrl+g s` | Hide or show the sidebar |
  | `ctrl+g S` | Stop this crew |
  | `ctrl+g q` | Quit the deck |
  | `ctrl+g ctrl+g` | Send a literal `ctrl+g` to the agent |

- **Mouse, selecting and links.** The deck keeps the mouse, even while an agent has the keyboard: the wheel scrolls the agent's history (or goes to the agent, if it draws full-screen and asks for the mouse), and you can click sidebar rows, mission tabs, missions on the overview and a picker's choices. Scrolling with the wheel is soft: start typing and you're back on the live screen, with the keys going to the agent. Select text and copy with Shift-drag (terminals let Shift bypass a program's mouse capture). `M` (or `ctrl+g m`) lets go of the mouse everywhere, for plain selection and Cmd-click on links without Shift, until pressed again. While it's let go, the deck stops your terminal turning the wheel into ↑/↓ keys, which would otherwise reach the agent and page through its prompt history. To copy several lines without picking up the sidebar's column, hide it first (`ctrl+g s`) or zoom (`ctrl+g z`).
- **Copying from an agent.** When an agent copies something to the clipboard itself (OSC 52, as Claude Code's `/copy` does), the deck puts it on yours and says so in the footer: through your terminal (OSC 52, which also works over SSH where the terminal allows it) and through `pbcopy` where there is one, since some terminals refuse OSC 52 by default. Agents can't read your clipboard. Crew run in the deck server, so this needs a server from the same build: one that was already running keeps the old behaviour until it restarts.
- **Working or waiting.** Each crew row shows what the agent is doing: `●c2 working`, `◐c2 waiting 4m` once it has finished its turn and is waiting for you, or `?c2 permission` when it has stopped to ask for one. ws knows this from the agent itself: it installs hooks in each crew session it launches (Claude Code through `--settings`, Cursor through a plugin, both for that session only, alongside your own hooks), and the agent reports when you've sent it a message (working), when its turn ends (waiting, with what it said, which the mission's Summary and the notification show), and, for Claude Code, when it needs permission. Where there are no hooks (the `shell` harness) the deck judges from the screen: still for 5 seconds means waiting. When crew goes quiet and you aren't looking at it, the deck says so in its footer, and if its window isn't focused, rings the bell and sends a desktop notification (iTerm2, kitty, Ghostty and WezTerm show these). The deck learns about focus from your terminal; one that doesn't report focus (or tmux without `focus-events on`) always gets the bell and notification.
- **Cursor's hooks need one thing from you.** Cursor runs a plugin's hooks for an event only if your own `~/.cursor/hooks.json` has a hook for it too. `ws doctor` checks this (once missions exist and the Cursor CLI is installed), and `ws doctor --fix` adds hooks for `beforeSubmitPrompt` and `stop` that do nothing (`/usr/bin/true`), keeping everything already there. Without them, ws still learns Cursor crew's session id, but judges working or waiting from the screen.
- **Ask and done.** Crew are taught to say where they are. `ws mission ask "<question>"` marks them `?c2 asks` with the question; `ws mission done "<summary>"` marks them `✓c2 done`. Each is announced once, and stands until you send that crew a message (`⏎`): moving around, typing, pasting or scrolling doesn't clear it. A crew asking or waiting moves its mission under **Needs you**. Crew are taught not to report done while a question of theirs is open.
- **Artifacts.** The mission view lists what the mission has produced: each capsule's PR (found from its branch, so a mission in several repos lists one per capsule), then the artifacts recorded on it, grouped by kind, each with where it is now. Once every PR is merged it says the mission has concluded, and `x` archives it. See [Artifacts](#artifacts) below.
- **Harnesses.** A harness is the agent CLI crew run in: `cursor` (the Cursor CLI, the default), `claude` (Claude Code) or `shell` (your `$SHELL`). `⏎` launches the one configured for crew (see [Agents](#agents) below), and `L` picks another for one launch. If the mission's last crew can be resumed (the agent reported a conversation id), `⏎` asks whether to resume it or launch new crew, and `R` resumes it directly. Resuming continues the same crew: same `c1`, same conversation and harness, back in the capsule it worked in.
- **Detach and reattach.** Crew run in a per-workspace deck server, not in the deck itself, so they keep working when you quit the deck, close the terminal, or the deck crashes. Quitting with crew running asks whether to detach (crew keep running) or stop them. The next `ws deck` reattaches with their screens intact. The server starts with the first launch, logs to `.ws/deck-server.log`, and exits a minute after its last crew ends with no deck attached. `ws deck server status` shows what it holds; `ws deck server stop` hangs every crew up. `WS_DECK_SERVER=0` keeps crew inside the deck instead, as a debugging aid: they start, end and are recorded exactly as on the server (the orchestrator runs too), but they stop when the deck quits, and dispatches are queued for you.
- If a deck server dies, crew records it left open are marked `lost` the next time a deck opens.

**Orchestrator.** When crew report done, or end by themselves (not when you stop them), ws runs the orchestrator: a headless agent (`cursor-agent -p` or `claude -p`) that reads the brief, the log, the crew so far, each capsule's diff and PR checks, and the end of the latest crew's output, then decides what happens next. There's no fixed plan: if the brief says "write a proposal and commit it to docs, then a new agent implements it", the orchestrator sees from the history that the proposal is done and dispatches the implementation. It acts only through `ws` commands, so a mistake comes back as an error it corrects:

| Command | Means | In the deck |
|---|---|---|
| `ws mission note "Orchestrator: …"` | What it found and decided, always one | In the log, read by its next run |
| `ws mission dispatch "<handoff>" [--capsule repo/name]` | Start fresh crew on the next piece of work | Crew appears under the mission, with *joined (dispatched)* and a notification |
| `ws mission resume-with <crew> "<what's left>"` | That crew should carry on | `⏎`/`R` offers to resume it and types the message in |
| `ws mission flag "<question>" --option "<answer>"…` | Something only you can decide, or the brief says to check with you first | `! asks you` on the mission row, the question and its answers in the view. `⏎` offers them (or write your own); your answer goes to the orchestrator, which acts on it. `F` dismisses |
| `ws mission new "<follow-up>"` | Separate work worth doing | A new mission waiting to be briefed, raised from this one |
| `ws mission conclude "<why>"` | Looks finished | *Suggested conclusion* in the mission view; `x` archives |

**Answering.** A flag is the orchestrator's question, usually with two or three answers it would act on. On a flagged mission, `⏎` offers those answers, *Write my own answer…*, *Ask an agent to answer it* and *Dismiss the question*. Asking an agent starts crew (the crew harness and model, in the workspace root) whose one job is the question: it's told the question and the suggested answers, not the whole brief; it finds out what's needed (runs the tests the orchestrator can't, reads the capsules) and answers with `ws mission answer`, which counts as your answer. It doesn't do the work the answer leads to: the orchestrator dispatches that. While it works, the mission shows it working rather than asking you. Answering clears the flag, logs your answer, counts as you stepping in (the dispatch count starts again), and runs the orchestrator with your question and answer, so it dispatches, resumes or concludes as you said. `ws mission answer "<text>"` does the same from a shell. The orchestrator doesn't flag what crew already asked you: you answer the crew. With `[orchestrator] enabled = false`, answering clears the flag and the rest is up to you.

**Dispatch.** Dispatched crew start fresh, told the brief and their handoff (what to do, where earlier crew's output is), and do only that part before reporting done. They run in the deck server, so the orchestrator can move a mission on with no deck open; the deck picks them up when it's running or next opens. If the next crew needs a capsule the mission doesn't have, the orchestrator lifts one first. You can dispatch by hand too (`ws mission dispatch -m <slug> "<handoff>"`). Briefing agents are taught to write staged work as a `## Stages` section, noting where you want to check in.

The orchestrator may dispatch three crew in a row without you; after that `dispatch` refuses and it has to flag you. Anything you do on the mission (launching crew, dismissing a flag in the deck or with `ws mission flag --clear`) starts the count again, and dispatches that fail to launch don't count. A dispatch that would launch at once refuses a capsule other crew are still working in (two agents in one worktree edit the same files); a queued one asks when you launch it. Crew that have reported done don't count: they've handed their work back, so a dispatch into their capsule ends their still-open session (logged, and resumable) and starts the new crew there. That's what lets a staged mission in one capsule move on by itself. Only one dispatch is queued at a time: a second is refused, and the orchestrator flags you instead. `F` dismisses the flag first, then the queued crew. It may look around read-only (`git diff`, `gh pr checks`) and lift capsules, but doesn't change code or push. Each crew event is handled once. Its output goes to `.ws/missions/<slug>/orchestrator.log`, and the prompt it was given to `orchestrator-prompt.md` beside it. `ws mission orchestrate` runs it by hand (`--crew c2` to say what prompted it, `--print-prompt` to see what it would be told). Crew output is saved to `crew-<id>.log` in the mission's folder when a run ends, so it can read it later.

Configure it in `ws.toml` or `ws.local.toml`:

```toml
[orchestrator]
harness = "claude"                  # which agent runs it: see Agents below
model = "haiku"                     # default: claude-haiku-5-5-medium on cursor, haiku on claude
enabled = true                      # false turns the automatic runs off
dispatch = "auto"                   # "ask" queues dispatched crew: → on the mission row, ⏎ launches, F dismisses
max_dispatches = 3                  # in a row without you
```

**Agents.** Three roles run agents, and each picks its harness and model in its own section of `ws.toml` or `ws.local.toml` (local settings win, field by field):

```toml
[crew]                 # agents doing the work
harness = "claude"     # cursor (default), claude or shell
model = "opus"         # passed as --model, in that agent's own naming; default: the agent's

[briefing]             # agents turning a request into a ready mission
harness = "cursor"     # default: crew's harness (and model)
model = "claude-opus-5-5-high"

[orchestrator]         # the headless agent deciding what's next
harness = "claude"     # cursor or claude (it must run headless); default: crew's, if it can
```

- A model belongs to the harness it's configured with: crew picked with `L` in another harness run on that agent's default. A `[briefing]` model with no harness applies to crew's harness.
- `WS_DECK_HARNESS` overrides `[crew] harness` for one session. `WS_CURSOR_AGENT` and `WS_CLAUDE_BIN` name the binaries if they aren't `cursor-agent` and `claude`.
- **Cursor.** Crew launch with `--trust`, so Cursor's workspace-trust dialog doesn't swallow the opening prompt, which is typed in once its screen settles. The orchestrator runs with `--force`: Cursor's print mode can't limit which commands it runs, so it relies on being told to act only through `ws`. A model your Cursor plan doesn't include fails the run ("Model unavailable on Free"); `auto` works on every plan.
- **Claude Code.** Crew get the opening prompt as their first argument and a session id ws chose (`--session-id`), so `R` resumes them. Claude Code asks once per repo whether to trust it: the first crew in each repo opens on its trust dialog (and shows as waiting, under *Needs you*). Answer it, and the prompt runs; that repo's later capsules don't ask again. Crew the orchestrator dispatches into a repo nobody has trusted yet wait at that dialog for you, so answer it once per repo when you start using Claude Code. The orchestrator runs with `--permission-mode dontAsk` and an allowlist: `ws mission`, `ws lift`, `ws status`, read-only `git` (`diff`, `log`, `show`, `status`) and `gh pr view/checks/list`, and reading files. Anything else is refused without a prompt.
- Crew and the orchestrator don't inherit the session identity of an agent ws itself runs in (`CLAUDECODE`, `CLAUDE_CODE_SESSION_ID` and the like), so a deck started from Claude Code's terminal launches independent sessions.

**Artifacts.** An artifact points at something the mission produced or relies on, so the next crew, the orchestrator and you can find it. `ws mission artifact add <what> [--kind <kind>] ["label"]` records one, and crew are taught to record what they make. `<what>` can be:

| You give | It records | Where it is now |
|---|---|---|
| A file in one of the mission's capsules (path or `repo/capsule:path`) | `backend/cache-lookup:docs/proposal.md` | `draft`, `in review` (its capsule's PR is open), then `landed`: once the PR merges, or the capsule is burned and the file is on the default branch, it's read from `.ground` |
| A file on a repo's default branch (a path in `.ground`, or `repo:path`) | `records:adr/0042-cache.md` | `landed`, or `missing` |
| A file in the mission's private folder, `.ws/missions/<slug>/files/` (path or `mission:path`) | `mission:spike.md` | `private`: only on your machine |
| A PR or issue (`repo#N`, or its GitHub URL) | `backend#88` | `open` or `merged` when it's a capsule's PR |
| Any other URL | itself | `link` |

Kinds say what it is: `proposal`, `adr`, `note`, `doc`, `plan`, `handoff`, `pr`, `issue`, `link`, or any lowercase word (default `pr`, `link` or `doc`). Adding the same thing again updates its kind and label; `ws mission artifact rm <ref>` forgets one. A path outside those places is refused, with what to do instead. `ws mission show -f llm` lists every artifact with where to read it, the orchestrator sees the same list, and `ws mission dispatch --read <ref>` (repeatable) gives the next crew a *Read these first* list in its handoff. The next stage may build on an artifact still in review; a brief that wants to wait for one to land says so as a check-in.

If your workspace keeps proposals, ADRs and notes in a repo of their own, name it in `ws.toml`, and crew and briefing agents are told to lift a capsule there when a stage produces one (its README or AGENTS.md keeps its conventions):

```toml
[missions]
records = "records"
```

**Files.** Missions live in `.ws/missions/<slug>/` at the workspace root (`mission.toml`, `brief.md`, `log.jsonl`, the private `files/` folder, and crew and review output). They are local to your machine. `ws` adds `.ws/` to the config repo's `.git/info/exclude` so they never show up in its `git status`, and `ws doctor` checks this.

**What ran.** `.ws/exec.log` records every process ws starts for missions, and what it sends them:

- each crew's command line (ready to paste into a shell), the folder it ran in, and its mission environment (`WS_*`, `TERM`, the first entry on `PATH`; never anything else, which may hold tokens);
- the chat id Cursor gave it, or why it gave none;
- the prompt typed into it, and how it ended;
- each orchestrator run: the prompt file it wrote, the exact agent command, how long it took, and how it ended;
- automatic runs that were skipped, and why;
- the deck server starting.

Every ws process appends to the same file, so `tail -f .ws/exec.log` follows the whole workspace. It rolls over to `exec.log.1` at 10 MB.

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
| `group` | The repo's [group](#groups). Ungrouped repos are always visible. |

**Group fields** (`[groups.<name>]`):

| Field | Description |
|---|---|
| `hidden` | Leave the group's repos out of commands that cover every repo. Default `false`. |

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

**Agent overrides:** each `[crew]`, `[briefing]` and `[orchestrator]` setting replaces the shared value (see [Agents](#missions) under Missions), so you can run Claude Code while the team default is Cursor.

**Repo overrides:**

You can override `display_name`, `color`, `after_create`, `group`, and `aliases` per repo. Aliases are appended to the shared list; other fields replace the shared value.

**Group overrides:**

`[groups.<name>] hidden = true|false` replaces the team's setting for you. `ws group show`, `hide` and `reset` write this for you.

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

This defines a `ws` function that wraps the `workspace` binary. For `jump`, `lift`, `dock`, `init`, and `mc`, output is evaluated in your shell. All other commands pass through directly.

Without this wrapper, navigation commands will print a `cd` path instead of actually navigating.

`burn` is handled differently. It removes a worktree that may be the very directory your shell is standing in, and a shell whose working directory has been deleted is in a bad state — some terminals kill it outright. So the wrapper moves first: it steps out to the workspace root, runs the burn from there, and returns you to where you were if the capsule survived. The original directory is passed along in `WS_ORIGIN_PWD`, so `ws burn <capsule>` still infers the repo from where you ran it. Without the wrapper, `ws burn` still works — you're just left standing in the deleted directory and have to `cd` out yourself.

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
after_change = "make generate"
```

| Field | Description |
|---|---|
| `after_switch` | Shell command run in `.silo/` after the sync target changes. Useful for restarting services that don't hot-reload. |
| `after_change` | Shell command run in `.silo/` after each incremental sync (debounced). Useful for code generation or build steps that need to run on every file change. |

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

**`ws root`** — prints the path of the workspace root, for use in scripts and shell substitutions:

```bash
cd "$(ws root)"
```
