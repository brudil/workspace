# Deck crew: Cursor CLI verification checklist

**Date:** 2026-11-04
**Status:** To run locally. The Cursor adapter was written without access to a real `cursor-agent`.

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
| Opening prompt arrived | Look at the agent's first turn | It mentions the mission and runs `ws mission show -f llm` |
| Env reaches the agent's shell | Ask it: "run `env \| grep WS_`" | `WS_MISSION=crew-trial` and `WS_CREW=c1` |
| Works in the capsule | Ask it: "run `pwd`" | The capsule path |
| Typing | Type normally, use arrows, backspace, enter | Same as in a normal terminal |
| Special keys | `ctrl+c`, `ctrl+r`, `shift+tab`, `alt+enter`, `esc`, `tab` | Each does what it does in Cursor outside the deck |
| Paste | Paste a multi-line block | Arrives as one paste, not line-by-line submits |
| Rendering | Watch a long streamed answer, spinners, colours, any boxes/borders | No garbage characters, no stale lines left behind |
| Resize | Resize the terminal window; `ctrl+g s` to hide/show the sidebar | Agent redraws to the new width |
| Back to the sidebar | `ctrl+g` then `tab` | Footer goes back to deck keys; agent keeps running (`●c1`) |
| Return | `tab` (or `⏎`) on the mission | Back in the agent |
| Stop | `ctrl+g` then `S`, `y` | Agent exits; mission view says `crew c1 (cursor) ended: …` |
| Resume | `R` on the mission | Same conversation comes back (needs step 2 to have worked) |
| Quit with crew running | Launch again, then `ctrl+g q`, `y` | Deck exits; `ws mission show crew-trial` lists the crew as `stopped: …` |

For comparison, `WS_DECK_HARNESS=shell ./bin/workspace deck crew-trial` runs your `$SHELL` as crew through the same path. If something is broken with Cursor but fine with the shell, the problem is in the Cursor adapter. If it's broken with both, it's in the terminal embedding.

## What to send back

For each numbered step and table row, "ok" or what happened instead. Paste the command outputs from steps 1–4, and a screenshot of any rendering problem.
