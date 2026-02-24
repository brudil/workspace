<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/wordmark-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/wordmark-light.png">
    <img alt="workspace" src="docs/assets/wordmark-light.png" width="420">
  </picture>
</p>

<p align="center">
  Multi-repo worktree first development for humans and agents.
</p>

---

**workspace** gives your multi-repo setup a single control plane. Git worktrees become lightweight capsules you can lift, dock, and debrief — while your IDE stays in sync automatically.

## Get started

Point `ws init` at a config repo containing your `ws.toml` and everything gets cloned and wired up:

```sh
ws init git@github.com:your-org/workspace-config.git
```

## How it works

Define your repos once in `ws.toml`:

```toml
[workspace]
org = "your-org"
default_branch = "main"

[repos.frontend]
display_name = "Frontend"
aliases = ["fe"]

[repos.backend]
display_name = "API"
after_create = "make setup"
```

Then use capsules (worktrees) to work on branches without ever switching:

```sh
ws lift frontend my-feature    # create a new capsule
ws dock frontend 1234          # check out a PR by number
ws jump fe                     # fuzzy-navigate between capsules
ws debrief                     # clean up anything that's landed
```

## Commands

| Command | What it does |
|---------|-------------|
| `ws` | Show status across all repos |
| `ws mc` | Interactive mission control dashboard |
| `ws lift` | Create a new capsule from a base branch |
| `ws dock` | Check out an existing branch or PR |
| `ws jump` | Navigate to any capsule with fuzzy matching |
| `ws debrief` | Remove landed and stale capsules |
| `ws open` | Open workspace in Cursor, VS Code, or IntelliJ |
| `ws board` | Add a capsule to your IDE workspace |
| `ws doctor` | Health check your workspace |
| `ws upgrade` | Pull latest config and set up new repos |

## Install

```sh
go install github.com/brudil/workspace/cmd/workspace@latest
```

Then add to your `~/.zshrc`:

```sh
eval "$(workspace shell-init zsh)"
```

This gives you the `ws` wrapper with shell completions and `cd` integration.

## License

MIT
