# `ws rg` — Workspace-Aware Ripgrep

**Date:** 2026-10-21
**Status:** Approved — implemented

## Problem

Agents (and people) working in a `ws` workspace constantly need to search code across repos. Today the options are all bad:

- **`rg` from the workspace root** walks `.bare/` (hooks, packed refs, config), `.silo/` (a mirror of some capsule), `.ground/` and every capsule. A file that exists in ground, the silo and three capsules shows up five times. The signal is buried in duplicates.
- **`rg` per worktree** requires knowing the layout (`repos/<repo>/<worktree>`), mapping the names humans use (`fe`, `API`) to canonical directories, and stitching results together by hand.
- **The scope an agent actually wants is asymmetric.** When working in a capsule, it wants *its own capsule* for the repo it is changing (the code it is about to edit) and *ground* for every other repo (the stable baseline it is integrating against). Nothing expresses that today.

The real cost is context. An agent that reaches for plain `rg` in a workspace with 200 capsules gets the same match back 200 times, and that flood goes straight into its context window. `ws rg` exists so the agent never has a reason to run raw ripgrep from the workspace root: the default has to be the right scope, with no flags, and the output has to be as small as the question allows.

`ws` already knows the layout, the aliases and where you are standing. `ws rg` puts that knowledge in front of ripgrep.

## Principle: sensible, terse defaults

Every default is chosen to return each relevant match **once**, and to add nothing of our own to the output.

- The default scope is one worktree per repo. Duplicates only appear when you ask for them with `--all`.
- `ws` prints nothing on success. No banner, no scope summary, no footer. Stdout is ripgrep's output and nothing else.
- Ripgrep's own defaults (gitignore-aware, binary-skipping, `path:line:text` when piped) are already terse. We do not override them.
- Errors are one line on stderr.

## Goals

- A thin passthrough: `ws rg` *is* `rg`. Every ripgrep flag works unchanged; agents already know them.
- Scope control along two axes: **which repos** (canonical names, aliases, unambiguous fuzzy matches) and **which worktrees** (ground only, where-I-stand, all capsules, or named capsules).
- Agent-friendly by construction: no interactive pickers, ripgrep's exit codes preserved, absolute paths in output, no extra chatter on stdout.
- Never search `.bare/` or `.silo/`.

## Non-goals

- A search index or any caching. Ripgrep is fast enough.
- Deduplicating identical content across capsules (see Future).
- Reimplementing or wrapping ripgrep's output. We do not parse or rewrite what `rg` prints.
- Searching the workspace config directory itself (`ws.toml`, IDE files). Could be added later.

## Design

### Command shape

```
ws rg [scope flags] [--] <ripgrep args...>
```

Scope flags are consumed from the front of the argument list. Parsing stops at the first token that is not a `ws rg` flag, or at `--`. Everything after that goes to ripgrep verbatim, followed by the resolved search roots.

Scope flags are **long-form only**. Ripgrep uses most single letters (`-r` replace, `-c` count, `-g` glob, `-a` text, `-s` case) and agents reach for them; we must never shadow them.

| Flag | Meaning |
|---|---|
| `--repo <sel>` | Restrict to these repos. Repeatable, comma-separated. `<sel>` is `name` or `name/worktree`. Default: all repos. |
| `--ground` | Use `.ground` for every selected repo, regardless of where you stand. |
| `--all` | Use every worktree (ground + every capsule) for every selected repo. |
| `--roots` | Print the resolved search roots, one per line, and exit without running ripgrep. |

`name` in `--repo` goes through the normal resolution pipeline (exact, alias, fuzzy) but **never** falls back to a picker: an ambiguous fuzzy match is an error that lists the candidates. `name/worktree` selects a specific worktree; the worktree part is exact or unambiguous-fuzzy in the same way. `name/.ground` is allowed and explicit. This mirrors the on-disk layout (`repos/frontend/my-feature`), so there is only one spelling to learn.

### Scope resolution

For each selected repo, pick worktrees in this order:

1. **Explicit worktree in `--repo`** (`fe/my-feature`) always wins.
2. **`--all`**: `.ground` plus every capsule.
3. **`--ground`**: `.ground` only.
4. **Default ("where I stand")**: if the current directory is inside a worktree of this repo (capsule or ground), use that worktree; otherwise use `.ground`.

The default is the asymmetric scope described in the problem statement. An agent in `repos/frontend/login-fix` running `ws rg useAuth` searches `frontend/login-fix`, `backend/.ground`, `infra/.ground`. The same command from the workspace root searches ground everywhere. Outside any worktree the default and `--ground` are identical.

The current directory comes from `WS_ORIGIN_PWD` when set (the shell wrapper's convention), else `os.Getwd()`, exactly as the other commands do.

`.bare/` and `.silo/` are never included by `--all` or the default. The silo is a mirror of a capsule and would only produce duplicates; its untracked files belong to the runtime, not the code. `--repo fe/.silo` as an *explicit* selection is permitted since it names a real directory.

### Output paths

Roots are passed to ripgrep as **absolute paths**, so every result line carries an absolute path. Two reasons:

- Agents run from arbitrary directories and feed matched paths straight into file tools. Absolute paths cannot be misread.
- The path itself answers "which tree did this come from?": `/…/repos/frontend/login-fix/src/auth.ts` vs `/…/repos/frontend/.ground/src/auth.ts`. No header or prefix is needed, and stdout stays pure ripgrep output (including `--json`, `-l`, `--files`, context lines, `--sort`).

A `--relative` mode (paths relative to the workspace root, i.e. `repos/frontend/.ground/…`) is cheap to add later for humans; not needed for v1.

### Execution

- Locate `rg` with `exec.LookPath`. If missing, fail with an install hint. No fallback to `grep`.
- Run `rg <user args> <roots…>` with `cmd.Dir` set to the **workspace root** (see glob handling below) and stdin/stdout/stderr inherited. Colour and paging therefore behave exactly as plain `rg` would (it detects the TTY itself).
- Propagate ripgrep's exit code unchanged: `0` matches, `1` no matches, `2` error. Agents and scripts depend on this.
- Nothing is printed by `ws` itself on success. Our own errors (bad repo, ambiguous alias, no `rg`) go to stderr with a non-zero exit.

Because roots are appended *after* the user's arguments, `ws rg --files`, `ws rg -e foo -e bar`, `ws rg -l needle`, `ws rg --json needle` all fall out naturally: ripgrep treats trailing positionals as paths.

**Users must not pass their own paths.** `ws rg foo src/` would make ripgrep search `src/` relative to the workspace root *in addition to* the roots. Narrowing is done with `--repo` and `-g`. Document this; do not try to detect it.

### Glob handling (the one non-trivial bit)

Ripgrep matches a `-g` glob that contains a `/` against the path **as it will print it, relative to the current working directory**, not relative to each search root. Verified against ripgrep 14.1.0 with a two-repo fixture:

| Invocation | `-g 'src/*.ts'` matches? |
|---|---|
| `rg -g 'src/*.ts' needle /abs/repos/fe/.ground /abs/repos/api/.ground` | no (exit 1) |
| cwd = workspace root, `rg -g 'src/*.ts' needle repos/fe/.ground repos/api/.ground` | no |
| cwd = workspace root, `rg -g 'repos/fe/.ground/src/*.ts' -g 'repos/api/.ground/src/*.ts' needle /abs/repos/fe/.ground /abs/repos/api/.ground` | **yes**, absolute paths in output |
| `rg -g '/abs/repos/fe/.ground/src/*.ts' …` | no (absolute globs never match) |
| cwd = a capsule, same expanded globs | no (prefix stripping only works under cwd) |

Globs without a `/` (`-g '*.ts'`, `-g '!*.snap'`) match on basename and work with any root form.

So a naive passthrough silently breaks `ws rg -g 'src/**/*.ts' needle`, which is one of the most common ways an agent narrows a search. The fix is mechanical:

1. Run ripgrep with `cmd.Dir` = workspace root and absolute roots (so prefix stripping applies and output stays absolute).
2. For each `-g`, `--glob`, `--iglob` value (in `-g VAL`, `-gVAL`, `--glob VAL`, `--glob=VAL` forms): if the glob contains a `/` once a leading `!` is removed, replace it with one glob per root: `[!]repos/<repo>/<worktree>/<glob with any leading / stripped>`. Globs without a `/` pass through untouched.

This preserves ripgrep's anchoring semantics exactly (`src/*.ts` stays anchored to each root; `**/src/*.ts` becomes `repos/fe/.ground/**/src/*.ts`, same meaning). The expansion is N roots × M globs, which is trivially small.

Known gap: globs coming from a ripgrep config file (`RIPGREP_CONFIG_PATH`) are not rewritten. Document it; it is rare.

### What ripgrep already gets right

- Each worktree has a `.git` *file* pointing at `.bare`. Verified that ripgrep still detects the repo boundary and honours `.gitignore` there, so `node_modules`, build output and so on are skipped per worktree as usual.
- `.rgignore` / `.ignore` files inside a repo keep working, so repos can self-manage noisy paths without `ws` involvement.

### Completions

`--repo` completes canonical names, aliases, and `repo/worktree` once a `/` is typed, reusing `completeRepoNames` and `ListAllWorktrees`.

### Shell wrapper

No change. `rg` falls through the wrapper's default case to `command workspace "$@"`.

### Naming

`rg`, not `grep` or `search`. It is honest about being a passthrough, and an agent that knows ripgrep's flags can use it with zero new learning. The only new vocabulary is `--repo`, `--ground`, `--all`, `--roots`.

## Examples

```bash
ws rg useAuth                        # my capsule for the repo I'm in, ground for the rest
ws rg --repo fe,api useAuth          # two repos by alias
ws rg --repo fe/login-fix TODO       # one specific capsule
ws rg --ground -t go 'func Load'     # baseline only, everywhere
ws rg --all FEATURE_FLAG_X           # every worktree: who else is touching this?
ws rg -g 'src/**/*.ts' -l needle     # globs are anchored per root, as you'd expect
ws rg --json needle | jq .           # ripgrep's own JSON, untouched
ws rg --roots                        # show what would be searched, run nothing
```

## Implementation sketch

- `internal/workspace/searchroots.go` — `SearchRoot{Repo, Worktree, Path}` and `(*Workspace).SearchRoots(cwd string, sel []RepoSelector, mode ScopeMode) ([]SearchRoot, error)`. Pure apart from `ListWorktrees`; tested with temp-dir fixtures. Roots ordered by `RepoNames` (sorted), ground before capsules, for stable output.
- `internal/cli/context.go` — add a strict, non-interactive `ResolveRepoStrict` (exact → alias → single fuzzy; multiple fuzzy is an error naming the candidates). `ResolveCapsule` gets the same strict variant.
- `internal/cli/rg.go` — cobra command with `DisableFlagParsing: true` so ripgrep flags are never interpreted by cobra. Front-parses scope flags, resolves roots, rewrites globs (`rewriteGlobs(args []string, roots []SearchRoot) []string`, pure, table-tested), execs `rg`, maps `*exec.ExitError` to our exit code.
- `FOR_LLMS.md` — a "Searching" section: use `ws rg` instead of `rg`/`grep` from the root; explain the default scope and `--repo`.
- `MANUAL.md` — a short section under Concepts or Shell Integration.

Roughly 250 lines plus tests.

## Decisions

- **Default scope is "where I stand"**: the current worktree for the repo you are in, `.ground` for every other repo. Behaviour depends on cwd, which is acceptable because every output path shows which tree it came from, and `--ground` is the explicit override.

- **`--all` excludes `.silo`.** It is a mirror of a capsule and only adds duplicates. `--repo fe/.silo` still works as an explicit selection.
- **Workspace root files are not searched.** `--repo ~` by analogy with `ws jump ~` can be added later if anyone asks.
- **No scope header.** Absolute paths already encode repo and worktree, and pure passthrough is the stronger property.

## Future

- **`--changed`**: search ground in full, but in each capsule only the files that differ from ground (`git diff --name-only <base>...HEAD` plus dirty files). That gives "all capsules, no duplicates", which is what `--all` users usually mean. Implementable by passing explicit file lists per capsule as extra roots.
- **`--relative`** output paths for human use.
- **Workspace-root `.ignore` file.** A defence for agents that run raw `rg` anyway: `ws init` / `ws doctor` could write an `.ignore` at the workspace root excluding `repos/*/.bare` and `repos/*/.silo`. It cannot express "every capsule but mine", so it only trims the flood rather than fixing it. Needs a check that ripgrep applies a parent-directory `.ignore` to searches started below it.
- A `ws files` sibling (`rg --files` with the same scoping) if listing turns out to be a common need; today `ws rg --files` already covers it.
