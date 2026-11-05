build:
    go build -trimpath -ldflags="-s -w" -o bin/workspace ./cmd/workspace

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
