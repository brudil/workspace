# Web and review: design

Status: phase 1 (the web server) built. Lives on `v2` with missions.

## Why

The deck is a terminal UI, and the terminal is the right place for crew: they are terminals. But some of what a mission needs from you is reading and judging, and a TUI is poor at that. Reviewing a diff is the clearest case: scrolling hundreds of lines, jumping between files, leaving a comment on line 214, seeing what changed since you last looked.

Today, when crew finish a stage you asked to check in on, the orchestrator flags you with a question and you answer in a line of text. That's a thin channel for "this is mostly right, but the cache key in `tokens.go` ignores the tenant, and I'd rather the test didn't sleep". What you'd write on a GitHub PR, you can't write to crew.

So:

1. **`ws deck` gets a web server**, for the work the TUI isn't good at.
2. **The first thing it serves is review**: GitHub's PR review, Linear-style. Keyboard-first, fast, nothing between you and the diff.
3. **Reviews are written for agents to read.** A review isn't a page for people; it's structured feedback that crew act on, answer and resolve, and that the orchestrator decides on.

## Principles

1. **The TUI stays the deck.** The web UI is where you go to do one thing well, then come back. Nothing about running crew moves to the browser.
2. **Local first.** Review targets a capsule's work before there's a PR. That's the agentic loop GitHub doesn't serve: crew write, you review in seconds, crew fix, you look at what changed. GitHub PRs are a later source behind the same UI.
3. **Reviews are data, the UI is a view.** The JSON is the truth; the web UI, the agent's markdown and the deck's summary all render it.
4. **Services record and emit; orchestration decides.** Submitting a review doesn't accept a stage or resume crew. It records the review and says it happened; the orchestrator, given the review, decides. (See [Events and orchestration](#events-and-orchestration).)
5. **The orchestrator doesn't mutate.** It never edits code, and never edits a review: it doesn't resolve comments or change verdicts. What it can do is dispatch crew to address them.
6. **Fast is a feature.** Every action has a key. No spinners for local data. Diffs come from git on the same machine; there's no excuse for slow.

## Concepts

| Concept | What it is |
|---|---|
| **Web server** | An HTTP server inside the deck server, serving the web UI and its API on localhost. |
| **Service** | A kind of non-agentic work ws offers, as a harness is a kind of agent. Review is the first. |
| **Review** | One pass over a mission capsule's committed changes, at one commit: comments, then a verdict. |
| **Comment** | Feedback anchored to lines of the diff, with a kind and a thread. |
| **Task** | Deliberately left undesigned. See [Tasks](#tasks-later). |

## The web server

### Where it lives

In the **deck server**: the per-workspace process that already owns crew, outlives decks and starts on demand (`ws mission dispatch` starts it with no deck open). A web UI that only worked while a TUI was open would be a trap: the review link in a notification would be dead.

- The deck server's Unix socket stays the deck's protocol. HTTP is a second listener on `127.0.0.1`.
- **Staying up.** The server idles out after a minute with no crew and no decks. An open web UI counts as a client: each tab holds an event stream (SSE), and the server stays up while any is connected.
- **Starting it.** `ws web [path]` starts the server if needed and opens the browser. The deck has `o` (open the selected mission in the browser) and `v` (review the selected capsule).

### Address

- **Port:** `[web] port` in `ws.toml` or `ws.local.toml`; by default one derived from the workspace root's path (a stable number in 47000–47999), so URLs survive restarts and two workspaces don't collide. If it's taken, the next free one.
- The server writes what it's listening on to `.ws/web.json` (`{port, token}`, mode 0600). The deck and `ws web` read it.

### Security

The server can read every repo in the workspace and will start agent work. A page on any website can make your browser send requests to localhost, so:

- **Token.** A random 32-byte token in `.ws/web.json`, kept across restarts. `ws web` opens `http://127.0.0.1:<port>/auth?t=<token>`, which sets an `HttpOnly` cookie and redirects to the clean URL. Every request needs the cookie (or `Authorization: Bearer <token>` for scripts). The cookie's name is per workspace: browsers keep cookies per host, not per port.
- **`SameSite=Lax`, not Strict**, so a link to the UI from elsewhere (a notification, a chat) opens signed in. A cross-site request can only read, and can't see the answer; writes are covered by the Origin check. Note that SameSite ignores the port: a page on any other `127.0.0.1` port counts as the same site, which is why the Origin check matters.
- **Host check.** Requests whose `Host` isn't `127.0.0.1:<port>` or `localhost:<port>` are refused (DNS rebinding).
- **Writes** need `Content-Type: application/json` and a matching `Origin` (CSRF).
- No CORS. Bound to loopback only; no option to bind wider in v1.

### API

JSON over HTTP, under `/api`, plus one event stream:

- `GET /api/workspace`: repos, missions, capsules (what `ws status -f llm` knows).
- `GET /api/capsules/{repo}/{name}/diff?base=&head=`: the diff as structured files and hunks.
- `GET /api/capsules/{repo}/{name}/file?path=&rev=`: file contents, for expanding context.
- `GET|POST /api/reviews…`: reviews, comments, threads, submitting (see below).
- `GET /api/events`: Server-Sent Events. New commits in a capsule, a crew reply on a comment, a review submitted elsewhere. The UI never polls.

The UI is served from `/` as a single-page app; any non-API path serves `index.html`.

## Review

### What's reviewed

A **mission capsule's changes against its base**: `git diff <merge-base of origin/<default branch> and HEAD>..HEAD`. Review is a service of the deck, and the deck is about missions, so a capsule that isn't on a mission can't be reviewed.

**Committed work only**: a comment needs a commit to anchor to. Crew are told to commit before they report done. If the capsule has uncommitted changes anyway, the review says so in a banner ("4 files with uncommitted changes aren't in this review").

A review is pinned to the **head commit** it was started at. If crew commit while you're reviewing, the UI says "3 new commits" and offers to move up; comments already written keep their anchors.

**Later:** GitHub PRs as a second source (the diff from `gh`, submitting through `gh api` as a GitHub review), for reviewing other people's work in the same UI.

### Lifecycle

```
draft ──submit──▶ submitted
```

- **Draft:** you're writing comments. Agents can't see a draft (like GitHub's pending review).
- **Submit** with a verdict: **approve**, **request changes**, or **comment** (feedback, no verdict). The summary is optional.
- Each **comment** then has a status: **open**, **addressed** (crew says it's done, with the commit), **won't fix** (crew disagrees, with why), **resolved** (you accept it) or **reopened**.
- **Re-review.** A new review of the same capsule starts from the last one's head: the UI shows the interdiff (what changed since you last looked) by default, with open comments from earlier reviews carried along and shown where their lines are now. This is where it should beat GitHub.

### Data

The JSON is the truth. One file per review:

```json
{
  "id": "r3",
  "capsule": "demo/cache-tokens",
  "mission": "cache-tokens",
  "base": "9f1c2e0",
  "head": "a41b77d",
  "state": "submitted",
  "verdict": "changes",
  "summary": "Close. Two things before this can go up.",
  "created": "2026-11-09T10:00:00Z",
  "submitted": "2026-11-09T10:12:00Z",
  "comments": [
    {
      "id": "r3.c1",
      "path": "internal/tokens/cache.go",
      "side": "new",
      "line": 214, "endLine": 216,
      "commit": "a41b77d",
      "excerpt": ["func cacheKey(t Token) string {", "\treturn t.ID", "}"],
      "kind": "blocking",
      "body": "The key ignores the tenant: two tenants' tokens with the same ID collide.",
      "suggestion": "\treturn t.Tenant + \"/\" + t.ID",
      "status": "open",
      "thread": [
        { "by": "c4", "at": "2026-11-09T10:30:00Z", "body": "Fixed: key is tenant/id.", "commit": "c09e1aa", "status": "addressed" }
      ]
    }
  ]
}
```

- **Kinds:** `blocking`, `suggestion`, `question`, `nit`. They tell crew what must change, what's a judgement call, what needs an answer rather than a change, and what to do only if it's cheap.
- **`excerpt`** is the lines as they were when you commented. The agent's rendering quotes it, so it reads the same however the code has moved since.
- **`suggestion`** replaces the commented lines, as GitHub's suggested changes do.
- **Anchoring:** v1 anchors to the commit the comment was made on; when a later commit changes those lines, the comment shows as *outdated* at its old position. Later: map comments forward through each commit, as GitHub does.

### Where reviews live

With the capsule's mission, alongside handoffs: `.ws/missions/<slug>/reviews/r3.json`, with a rendered `mission:reviews/r3.md` that's recorded as an artifact of kind `review` once submitted. Review ids count up per mission.

### What agents read and do

```
ws review show [r3] -f llm         # the latest review of the capsule you're in, by default
ws review reply r3.c1 "…"          # answer a question, or explain
ws review resolve r3.c1 --commit c09e1aa ["note"]    # addressed
ws review wontfix r3.c1 "why"
```

`ws review show -f llm` renders the review as markdown grouped by file: each comment's kind, the quoted excerpt with its line numbers, the body, any suggestion as a diff, and its thread. Crew don't need the diff to act on it.

Crew can address and argue; only you resolve. The orchestrator does neither (it doesn't mutate): it can dispatch crew to address open comments. `ws mission show -f llm` lists open review comments, and new crew get the latest review's open comments in *Where earlier crew left off*.

### The UI

React, keyboard-first. Three regions: a file list (with viewed state and comment counts), the diff, and a summary bar (verdict, counts, submit).

Keys, as a starting point:

| Key | Does |
|---|---|
| `j` / `k` | Next / previous line |
| `n` / `p` | Next / previous hunk |
| `J` / `K` | Next / previous file |
| `t` | Jump to file (fuzzy) |
| `c` | Comment on the line (or selected lines; `V` starts a selection) |
| `1`–`4` | While writing: blocking, suggestion, question, nit |
| `⌘⏎` | Save the comment |
| `r` | Reply in the thread under the cursor |
| `x` | Mark the file viewed (and move to the next) |
| `u` | Unified / split |
| `i` | Since last review / everything |
| `s` | Submit: then `a` approve, `c` request changes, `m` comment |
| `⌘K` | Command palette: everything, by name |
| `?` | Keys |

Syntax highlighting on the client. Large diffs are virtualised; a file over a size threshold is collapsed until opened. Context expands on demand from `/file`.

## Events and orchestration

Today the orchestrator is per mission and runs on crew events (`done`, `ended`, `answer`), with a prompt ws builds. The direction is a **workspace-level orchestrator prompt**: your policy, in prose, for how events and state changes are handled across the workspace. Tasks hang off that, not off a fixed workflow inside each service.

So review is built as a service that **records and emits**, nothing more:

- Submitting a review writes it, renders the markdown, records the artifact, logs `review_submitted` (verdict, counts) to the mission log, and runs the mission orchestrator with a new trigger, `review`.
- The orchestrator's prompt gets the review rendered as crew would read it, and which crew worked in that capsule. It decides with the commands it already has: resume the crew with the review (`resume-with`), dispatch new crew to address it (`--read mission:reviews/r3.md`), `accept` the stage on an approval, or `flag` you. It never touches the review itself.
- A crew answer on a comment logs `review_reply` but doesn't trigger the orchestrator by itself. Crew reporting done after addressing comments does, as now.

When the workspace-level prompt exists, it's the seam: the same `review_submitted` event goes to it, and what happens is policy. Review needs no change.

## Tasks (later)

Deliberately undesigned. What review teaches us goes into it:

- Some work waits on a person (a review) or a process (CI, a test run, a deploy), not an agent.
- It needs a record (state, who it's waiting on, its result) and a place in the deck (*Needs you*).
- Its end is an event for the orchestrator, like crew's.

A review *could* be the first task: open while you haven't submitted, done when you have. We build review first and extract the task shape from it, rather than guess the abstraction now.

## Code and build

```
web/                     # the React app (Vite, TypeScript)
  src/
  package.json
internal/web/            # Go: HTTP server, auth, API handlers, go:embed of dist/
  dist/                  # built app, committed
internal/review/         # Go: reviews, comments, storage, anchoring, the agent's markdown
```

- **Vite + React + TypeScript.** npm, since it comes with Node.
- **Built assets are committed** (`internal/web/dist/`), so `go install github.com/brudil/workspace/cmd/workspace@latest` keeps working without Node. CI builds the app and fails if `dist/` differs.
- `just build` builds the app, then the binary. `just web-dev` runs Vite's dev server with its API proxied to the running deck server, for working on the UI with hot reload.
- The diff comes from git in Go and is served structured (files, hunks, lines with numbers on both sides); the client doesn't parse patches.

## Phases

1. **Web server.** In the deck server: listener, port, `.ws/web.json`, token auth, host and origin checks, SSE keeping the server alive, `ws web`, the deck's `o`. A first page that lists missions and capsules, to prove the stack end to end (embed, build, CI check).
2. **Read-only review.** The capsule diff API, the diff viewer, file list, viewed state, the keyboard model, unified and split, expanding context. `v` in the deck.
3. **Comments and submitting.** Drafts, comments with kinds and suggestions, submit with a verdict, storage, the rendered markdown and artifact, `ws review show -f llm`, the `review_submitted` event and the orchestrator's `review` trigger. Crew are told to commit before reporting done.
4. **The loop.** `ws review reply / resolve / wontfix`, threads in the UI live over SSE, re-review by interdiff with earlier comments carried along, open comments in `ws mission show -f llm` and in handoffs.
5. **Later.** GitHub PRs as a source. Comments mapped forward through commits. Other services. Tasks, once the workspace orchestrator exists.

## Decided

- **Only mission capsules are reviewed.** Review is a service of the deck.
- **Only you resolve comments.** Crew address or decline them; the orchestrator doesn't mutate, so it dispatches crew to address them instead.
- **Committed work only.** Crew commit before reporting done; uncommitted changes are flagged, not reviewed.

## Risks

- **A second UI to keep consistent.** Mitigated by the principle that the TUI stays the deck; the web UI does a few things, deeply.
- **Node in the build.** Contributors to Go code don't need it (dist is committed); changing the UI does. CI keeps dist honest.
- **Localhost servers are attack surface.** Token, host check, origin check, loopback only. Reviewed again before anything in the web UI can start crew directly.
- **Anchoring drift.** v1's "outdated at old position" is crude next to GitHub's. Interdiff re-review covers the common case: you mostly want to see what changed, not where an old comment moved to.
