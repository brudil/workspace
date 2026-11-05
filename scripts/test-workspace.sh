#!/usr/bin/env bash
# Builds a throwaway ws workspace for trying missions, the deck and agents
# by hand, with Claude Code (on haiku, to keep it cheap and quick) in every
# role: crew, briefing and the orchestrator.
#
#   scripts/test-workspace.sh [dir]     (default: ~/Projects/ws-test)
#
# It has two repos, whose "remotes" are local repos kept inside the
# workspace (.remotes/), so lift, dock and fetch work offline:
#
#   demo      a tiny Go program, for crew to change
#   records   proposals, ADRs and notes ([missions] records = "records")
#
# The GitHub org is made up, so anything that asks GitHub (PRs, gh) finds
# nothing. Delete the folder and run this again for a fresh one.
set -euo pipefail

dir="${1:-$HOME/Projects/ws-test}"
if [ -e "$dir" ] && [ -n "$(ls -A "$dir" 2>/dev/null)" ]; then
	echo "$dir already exists and isn't empty: remove it first for a fresh test workspace" >&2
	exit 1
fi
mkdir -p "$dir"
dir="$(cd "$dir" && pwd)"

# Seed commits are made by "ws test" and never signed, whatever the global
# git config says (a signing agent would otherwise be asked for each one).
git_seed() { git -c user.name="ws test" -c user.email="ws-test@example.com" -c commit.gpgsign=false "$@"; }

# A "remote" for a repo: a plain repo with one commit holding the files the
# caller wrote into it.
make_remote() {
	local name="$1" src="$dir/.remotes/$1"
	git_seed -C "$src" init -q --initial-branch=main
	git_seed -C "$src" add -A
	git_seed -C "$src" commit -q -m "initial $name"
}

mkdir -p "$dir/.remotes/demo" "$dir/.remotes/records/proposals" "$dir/.remotes/records/adr" "$dir/.remotes/records/notes"

cat >"$dir/.remotes/demo/README.md" <<'EOF'
# demo

A tiny Go program for trying ws missions. Run it with `go run .`.
EOF
cat >"$dir/.remotes/demo/go.mod" <<'EOF'
module example.com/demo

go 1.22
EOF
cat >"$dir/.remotes/demo/main.go" <<'EOF'
package main

import "fmt"

func greeting() string { return "hello" }

func main() { fmt.Println(greeting()) }
EOF
cat >"$dir/.remotes/demo/main_test.go" <<'EOF'
package main

import "testing"

func TestGreeting(t *testing.T) {
	if got := greeting(); got != "hello" {
		t.Errorf("greeting() = %q", got)
	}
}
EOF
make_remote demo

cat >"$dir/.remotes/records/README.md" <<'EOF'
# records

Proposals, ADRs and notes, shared through main.

- `proposals/<short-name>.md`: a title, the problem, the proposal, and what changes.
- `adr/NNNN-<short-name>.md`: numbered from 0001. Context, decision, consequences.
- `notes/<short-name>.md`: anything worth keeping that isn't a decision.
EOF
touch "$dir/.remotes/records/proposals/.keep" "$dir/.remotes/records/adr/.keep" "$dir/.remotes/records/notes/.keep"
make_remote records

# The workspace root is the config repo, as `ws init` would clone it.
cat >"$dir/ws.toml" <<'EOF'
[workspace]
org = "example-org"
default_branch = "main"
display_name = "ws test"

[repos.demo]
display_name = "Demo"
aliases = ["dm"]

[repos.records]
display_name = "Records"
aliases = ["rec"]

[missions]
records = "records"
EOF
printf 'repos/\nws.local.toml\n' >"$dir/.gitignore"
git_seed -C "$dir" init -q --initial-branch=main
git_seed -C "$dir" add ws.toml .gitignore
git_seed -C "$dir" commit -q -m "ws test config"
printf '/.remotes/\n/.ws/\n' >>"$dir/.git/info/exclude"

# Each repo the way ws lays it out: a bare clone and its .ground worktree.
for name in demo records; do
	repo="$dir/repos/$name"
	mkdir -p "$repo"
	git clone -q --bare "$dir/.remotes/$name" "$repo/.bare"
	git -C "$repo/.bare" config remote.origin.fetch '+refs/heads/*:refs/remotes/origin/*'
	git -C "$repo/.bare" fetch -q --all
	git -C "$repo/.bare" worktree add -q "$repo/.ground" main
done

# Per machine: Claude Code on haiku in every role.
cat >"$dir/ws.local.toml" <<'EOF'
[crew]
harness = "claude"
model = "haiku"

[briefing]
harness = "claude"
model = "haiku"

[orchestrator]
harness = "claude"
model = "haiku"
EOF

cat <<EOF
Test workspace ready: $dir

  cd $dir
  ws deck              # n: say what you want, and a briefing agent sets up the mission

Claude Code (haiku) plays every role (ws.local.toml). The first crew in each
repo opens on Claude's trust dialog: answer it once per repo. GitHub isn't
real here (org example-org), so PRs never show. Remove the folder and run
this again for a fresh one.
EOF
