# ws

Workspace management CLI tool written in Go.

## Building

Always build for development using `just build`. This produces the binary at `bin/workspace`.

Do not use `go build` directly or produce binaries elsewhere.

## Testing

Run tests with `just test`.

To try missions, the deck and agents by hand, `just test-workspace [dir]` builds a throwaway workspace (default `~/Projects/ws-test`): repos `demo` and `records` with local remotes, Claude Code on haiku in every role. Remove the folder and run it again for a fresh one.
