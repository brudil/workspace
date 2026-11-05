# Deck crew: Cursor CLI verification checklist

**Date:** 2026-11-04
**Status:** Run locally on 2026-11-05 against `agent` 2026.11.05-86047df (macOS, deck driven through tmux). Everything except the orchestrator end to end (5d) and things that need a real terminal (mouse, links, notifications). Results below.

## Results, 2026-11-05

**Steps 1–4.**
- `cursor-agent` and `agent` are both installed.
- `create-chat` prints a bare UUID and exits 0 in about 1s.
- Crew launch as `cursor-agent --trust --resume <id>`, and the prompt is typed in afterwards.
- Print mode works with `--model auto` and with no `--model`, about 5s for a one-word reply.

**Fixed after this run:**
- **Trust dialog swallowed the opening prompt.** Cursor shows a "Workspace Trust Required" dialog in any folder it hasn't seen, which includes every newly lifted capsule. The prompt was typed into it, an `a` in the text answered it, and the rest was lost. Crew now launch with `--trust`, which skips the dialog interactively too, alone or with `--resume`.
- **`ws lift` panicked with a `ws.local.toml` that has no `[boarded]` section** (e.g. only `[orchestrator]`): the first lift wrote to a nil map. This was older than missions.
- **Deck server shutdown race.** The server signalled it was done before it removed its socket.

**Environment, not ws:**
- **Agents found an older `ws`.** Cursor's shell loads `~/.zshrc`, and a `PATH` line there (`$HOME/go/bin`) put an installed `workspace` ahead of `.ws/bin`. Crew got `unknown command "mission"`. Keep the installed `ws` current; on the test machine `~/go/bin/workspace` is a symlink to `bin/workspace`.
- **The orchestrator's default model is refused on a Free plan.** On Cursor Free, `--model claude-haiku-5-5-medium` exits 1 with "Model unavailable on Free". `--list-models` lists it anyway. Test with `[orchestrator] model = "auto"`; haiku stays the default.
- **A stale trust dialog stays drawn after it's answered.** Cursor leaves it on screen until it next redraws, in plain tmux as well, so it isn't the deck's emulator.

**Passed:**
- **Launch:** launching crew, opening prompt delivery (trusted folder), `WS_MISSION`/`WS_CREW` in the agent's shell, cwd is the capsule.
- **Input and screen:** multi-line bracketed paste arrives as one paste, scrollback, zoom in and out (redraws on return).
- **Crew:** second crew asks before sharing a capsule, stop (`hangup`), `R` resumes the same crew, chat and capsule with history.
- **Deck server:** detach, `deck server status`, reattach with screens intact, stop and quit, transcripts saved, idle server exit, long-path socket fallback under `$TMPDIR`.
- **Status and signals:** working/waiting; Cursor's command approval counts as waiting (Needs you); ask (`? asks`, question in the view, log); done (`✓ done`, summary); orchestrator skipped with `enabled = false`.
- **Briefing:** `n` → briefing agent → brief, title, capsule → `ws mission ready --slug` → lands on the ready mission.

**Not run:**
- 5d, the orchestrator end to end: it runs `agent -p --force`.
- Mouse selection, Cmd-click links, bell and desktop notifications: these need a real terminal.
- `ctrl+r`, `shift+tab`, `alt+enter`, `ctrl+g n`/`p`, artifacts from a real PR.

**Tip:** keys typed while Cursor shows a command approval go to the approval. A `y` approves it, and the next few characters can be dropped while Cursor changes screens.

---

Every assumption about the Cursor CLI is in `internal/harness/cursor.go`, marked `GUESS`. This checklist confirms or corrects each one. Paste the outputs (or "works" / "broken: …") back and the adapter gets fixed to match.

## Setup

```sh
git checkout claude/deck-crew
just build
```

Have a workspace with at least one mission that owns a capsule:

```sh
./bin/workspace mission new "Crew trial" --brief "Say hello, then list the files in this capsule."
WS_MISSION=crew-trial ./bin/workspace lift <repo> crew-trial
```

## 1. Binary name

```sh
which cursor-agent agent
```

**Guess:** `cursor-agent`, with `agent` as a fallback. If it's something else, `WS_CURSOR_AGENT=<name or path>` overrides it for now.

## 2. Getting a chat id up front

```sh
cursor-agent create-chat; echo "exit=$?"
```

**Guess:** prints a chat id alone on the last line and exits 0. If the command doesn't exist, or prints something else, paste the output. Without an id, crew still launch but `R` (resume) won't be offered.

## 3. Opening prompt

```sh
cursor-agent "Say hello and then wait for me"
```

**Guess:** the positional argument is sent as the first message and the agent **stays interactive** afterwards. Report whether it exits after answering (then it needs a different flag) or ignores the argument.

## 4. Resume

Using an id from step 2 (or one from an earlier chat):

```sh
cursor-agent --resume <id>
cursor-agent --resume <id> "and now say goodbye"
```

**Guess:** the first reopens the chat interactively. The second (what the deck runs for new crew when `create-chat` worked) reopens the same chat and sends the prompt. Report which of the two works.

## 5. In the deck

```sh
./bin/workspace deck crew-trial
```

Press `⏎` to launch Cursor on the mission. Then check:

| Check | How | Expected |
|---|---|---|
| Launches | `⏎` on the mission | Header shows `crew c1 · cursor`, the agent's UI fills the left pane, the sidebar shows `●c1` |
| Opening prompt arrived | Watch the pane after launch | Cursor draws, then about a second later the deck pastes the opening prompt into its input and submits it (footer: *sent the opening prompt to c1*). Its first turn mentions the mission. Report if the paste lands as separate lines, isn't submitted, or arrives before Cursor is ready |
| Env reaches the agent's shell | Ask it: "run `env \| grep WS_`" | `WS_MISSION=crew-trial` and `WS_CREW=c1` |
| Agent runs the deck's ws | Ask it: "run `command -v ws workspace` and `ws mission show -f llm`" | Both paths are `<workspace root>/.ws/bin/…`, and the mission prints. If the paths are elsewhere, your shell's rc file is putting another directory first on `PATH`: tell me which |
| Works in the capsule | Ask it: "run `pwd`" | The capsule path |
| Typing | Type normally, use arrows, backspace, enter | Same as in a normal terminal |
| Special keys | `ctrl+c`, `ctrl+r`, `shift+tab`, `alt+enter`, `esc`, `tab` | Each does what it does in Cursor outside the deck |
| Paste | Paste a multi-line block | Arrives as one paste, not line-by-line submits |
| Select and copy | With the agent focused, drag over its output, copy, paste elsewhere | Your terminal selects and copies normally (no Option/Shift needed). With the sidebar shown, multi-line selections include its column: `ctrl+g s` hides it |
| Click links | Cmd-click a URL or PR link in the agent's output | Opens in the browser |
| Rendering | Watch a long streamed answer, spinners, colours, any boxes/borders | No garbage characters, no stale lines left behind |
| Resize | Resize the terminal window; `ctrl+g s` to hide/show the sidebar | Agent redraws to the new width |
| Back to the sidebar | `ctrl+g` then `tab` | Footer goes back to deck keys; agent keeps running (`●c1` row under the mission) |
| Return | `tab` (or `⏎`) on the crew row, or `tab` on the mission | Back in the agent |
| History | After a long answer, `ctrl+g [`, then `k`/`ctrl+u`/`g`, `esc` | Scrolls back through earlier output; header shows `↑N lines`; `esc` returns to live. If it says "drawing full-screen", the agent uses the alternate screen and has no history here: report that |
| Wheel | Scroll the mouse wheel over the agent | Scrolls history (or the agent's own UI if it's full-screen) |
| Zoom | `ctrl+g z`, use the agent, `ctrl+g z` | The agent takes the whole terminal with no deck chrome; everything works exactly as outside the deck; `ctrl+g z` brings the deck back intact. **If anything renders or types wrong in the pane, check whether it's right when zoomed** |
| Second crew | `ctrl+g tab`, `g` (mission row), `⏎` | If the mission has one capsule: asks before sharing it; `y` launches `c2`. `ctrl+g n` / `p` switch between them |
| Stop | `ctrl+g` then `S`, `y` | Agent exits; mission view says `c1 (cursor) ended: …` |
| Resume | After stopping crew (or quitting with **Stop crew**), `⏎` on the mission | Asks **Resume c1** / **New crew**; resume brings back the same conversation as `c1` (not `c2`), in the same capsule. `R` does the same without asking |

## 5b. Briefing a new mission

| Check | How | Expected |
|---|---|---|
| Ask, don't name | In the deck, `n` | Footer asks **What's the mission?** (`N` is the old title + editor flow) |
| From an issue | Type e.g. `<alias> issue <number>` for a real issue, `⏎` | A `briefing` mission appears at the top; a briefing agent (`●c1 briefing`) starts and has focus |
| It reads the issue | Watch its first steps | Runs `ws status -f llm`, then `gh issue view … --repo <org>/<repo>`; doesn't edit code |
| It asks, then briefs | Answer its questions | It writes the brief (`ws mission brief --set -`), titles it, and lifts capsules in the right repo(s) |
| Hand-over | When it's done, agree | It runs `ws mission ready`; its session ends a couple of seconds later; the deck lands on the mission (now ready) and says **Briefed: … · ⏎ to launch crew** |
| Work crew | `⏎`, choose **New crew** | A normal agent starts in the capsule with the brief as its first message |
| Stop early | `n`, then `ctrl+g S` mid-briefing | Mission stays `briefing`; `⏎` offers to resume the briefing conversation |

If the briefing agent tries to run `ws mission ready` before it has written a brief or lifted a capsule, ws refuses with a message telling it what's missing: say whether it recovered.

## 5c. Working, waiting, asking, done

| Check | How | Expected |
|---|---|---|
| Working / waiting | Launch crew, give it a task, `ctrl+g tab` to the sidebar | Row shows `●c2 working` while Cursor streams, then `◐c2 waiting …` a few seconds after it stops. If it flips to waiting while Cursor is still thinking (no visible output for 5s+), say so |
| Notified | Stay in the sidebar (or another window) while it finishes | Deck says *c2 … is waiting for you*, bell rings, desktop notification (if your terminal supports OSC 9) |
| Asks | Give it something needing a decision | It runs `ws mission ask "…"`; row shows `?c2 asks`, mission row `?1`, notification with the question. Typing to it clears it |
| Done | Let it finish | It runs `ws mission done "…"`; row shows `✓c2 done`, mission view shows the summary |
| Artifacts | After it opens a PR | Mission view's **Artifacts** lists the capsule's PR; anything it recorded with `ws mission artifact add` too |

## 5d. Orchestrator

We run `cursor-agent -p --force --trust --output-format text --model <model> "<prompt>"` (flags confirmed from `cursor-agent --help`). Still to confirm: paste `cursor-agent --list-models`, so we know `claude-haiku-5-5-medium` is a name it accepts (it may want the `name[effort=…]` form). If it isn't, set `[orchestrator] model` in `ws.local.toml` to one that is.

Try it on a staged mission: `n`, and ask for something like *find the note about X in the docs repo, write a proposal and commit it to docs; then a new agent implements it*.

| Check | How | Expected |
|---|---|---|
| Stages | Briefing | The brief has a `## Stages` section, and the briefing agent lifted at least the first stage's capsule |
| First stage | `⏎`, let the crew work | It does the proposal only, commits it and runs `ws mission done` |
| Dispatch | Wait (up to a minute or so) | The log has an *Orchestrator:* note, the mission gains `c2` (*joined (dispatched)*, a notification), and c2's first message includes the handoff. If c2 needs a backend capsule, the orchestrator lifted it |
| No deck | Repeat with the deck closed after the first stage starts | c2 still starts; `ws deck` shows it running |
| Conclude | Let c2 finish | *Suggested conclusion* in the mission view |
| Prompt | `./bin/workspace mission orchestrate -m <slug> --print-prompt` | The brief, capsules with diffs, log, crew list and the command. Anything missing or wrong? |
| Ask mode | `[orchestrator] dispatch = "ask"` in `ws.local.toml` | Instead of launching, `→` on the mission row and *→ Next…* in the view; `⏎` launches it, `F` dismisses |
| Limit | Say whether it ever hit the three-dispatches limit | It should flag you instead of dispatching a fourth |
| Stopped crew | Stop crew with `ctrl+g S` | The orchestrator doesn't run |
| Logs | `cat .ws/missions/<slug>/orchestrator.log` | Each run's output, nothing alarming |

Say whether its decisions were sensible, and whether it ever did something it shouldn't (edited code, pushed).

## 6. Detach and reattach

| Check | How | Expected |
|---|---|---|
| Detach | With crew running, `q` in the sidebar, choose **Detach** | Deck exits; `./bin/workspace deck server status` lists the crew |
| Reattach | `./bin/workspace deck crew-trial`, then `tab` | Crew row is back with its screen as you left it; the agent kept working while detached |
| Crash survival | Close the terminal window the deck is in, open a new one, `./bin/workspace deck` | Crew still there |
| Stop and quit | `q`, choose **Stop crew and quit** | `ws mission show crew-trial` lists the crew ended; `deck server status` shows no crew |
| Server log | `cat .ws/deck-server.log` at the workspace root | One line per launch and end, nothing alarming |
| Stop everything | `./bin/workspace deck server stop` | Hangs up every crew and the server |

`WS_DECK_SERVER=0` runs crew inside the deck instead (no detach). If something misbehaves only with the server, try with `WS_DECK_SERVER=0` and say which.

For comparison, `WS_DECK_HARNESS=shell ./bin/workspace deck crew-trial` runs your `$SHELL` as crew through the same path. If something is broken with Cursor but fine with the shell, the problem is in the Cursor adapter. If it's broken with both, it's in the terminal embedding.

## What to send back

Whenever something misbehaves, `.ws/exec.log` at the workspace root shows exactly what ws ran, with which arguments, and how it ended: paste the entries around the problem.


For each numbered step and table row (sections 5 to 6), "ok" or what happened instead. Paste the command outputs from steps 1–4, and a screenshot of any rendering problem.
