build: web
    go build -trimpath -ldflags="-s -w" -o bin/workspace ./cmd/workspace

# Build the web UI (web/) into internal/web/dist, which is committed so go install needs no Node.
web:
    #!/usr/bin/env sh
    set -e
    if ! command -v npm >/dev/null; then echo "npm not found: using the committed web UI build"; exit 0; fi
    cd web
    [ -d node_modules ] || npm ci --silent
    npm run --silent build -- --logLevel warn

# Work on the web UI with hot reload at localhost:5173, against a workspace's running web UI (ws web starts it).
web-dev ws=(env_var('HOME') + "/Projects/ws-test"):
    cd web && [ -d node_modules ] || npm ci
    cd web && WS_ROOT="{{ws}}" npm run dev

check:
    gofmt -l . | grep . && exit 1 || true
    go vet ./...
    go build ./...

test:
    go test ./...

cover:
    go test ./... -coverprofile=cover.out && go tool cover -func=cover.out

cover-html:
    go test ./... -coverprofile=cover.out && go tool cover -html=cover.out

# Build a throwaway workspace for trying missions by hand (Claude Code on haiku in every role).
test-workspace dir=(env_var('HOME') + "/Projects/ws-test"):
    ./scripts/test-workspace.sh "{{dir}}"
