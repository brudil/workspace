# ws CLI — Ideas & TODO

## High Priority
- [ ] SSH clone URL support (configurable per-workspace in ws.toml)

## Medium Priority
- [ ] Repo groups/tags — e.g. `my-repo = { groups = ["api"] }`, then `ws fetch --group api`, `ws status --group api` to scope commands to a subset
- [ ] Integration tests — real git repos in temp dirs, exercise clone/worktree/fetch end-to-end

## Medium Priority — MC View
- [ ] `ws mc` filtering commands/shortcuts — `//local`, `//me`, `//rr` (review requested), etc.

## Low Priority
- [ ] CI pipeline (GitHub Actions) — needed closer to v1

## Deprioritised
- `ws list` — `ws status` covers this for now
- `ws list` — could be `ws status .` scoped to current repo
- Per-repo config overrides — not needed yet
- `ws exec <command>` — idea is sound but solution unclear given diverse repo languages/tooling

## Completed
- [x] `ws shell-init` — shell-init command with zsh wrapper + completions, sourced via `eval "$(workspace shell-init zsh)"`
- [x] `ws status --json` — machine-readable output for scripting
- [x] `ws doctor` — health check: all repos cloned, symlinks valid, no orphaned worktrees, gh installed
- [x] `ws mc` should be smart about the cursor from the PWD
- [x] `ws new <pr-number-or-url>` — create worktree from a PR, auto-detect repo + branch from PR metadata
- [x] Shell prompt integration — `ws prompt` outputs repo/worktree info for PS1/starship (e.g. `vertex-seclr:feature-x`)
- [x] Post-worktree hook per repo — e.g. `long-backend-repo = { post_create = "npm install" }`, runs automatically after `ws new` and `ws setup`
- [x] Shell completions (zsh/bash/fish) — cobra has built-in support, quick win
- [x] `ws status` shows PR info per worktree — PR number, check status, unseen review activity (shell out to `gh pr view --json`)
- [x] Repo aliases in ws.toml — e.g. `long-backend-repo = { aliases = ["b", "backend"] }`, resolved everywhere a repo name is accepted (commands, jump, completions)
- [x] Spinner/progress for long operations (clone, fetch) — bubbletea spinner, quick win
- [x] Color-coded output for `ws setup` (success/skip/error)
- [x] `ws stale` — show worktrees with no recent commits or whose branches are merged; `--prune` flag to interactively remove them
- [x] Shell completions (zsh/bash/fish) — dynamic completion for repo names, aliases, worktrees, and editors via cobra
- [x] Shell prompt integration — `ws prompt` with `--format json` and `--template` flags for starship/p10k/PS1
- [x] `ws jump` / `ws j` — shell eval integration (`j() { eval "$(ws jump "$@")" }`) for cd'ing to repos/worktrees with interactive pickers
