package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Ports the web UI picks from when [web] port isn't set: each workspace
// gets a stable one from its path, so its URLs survive restarts.
const (
	portBase  = 47000
	portRange = 1000
	// portTries is how many ports after the workspace's own are tried when
	// it's taken.
	portTries = 20
)

// DefaultPort is the workspace's own port.
func DefaultPort(root string) int {
	h := fnv.New32a()
	h.Write([]byte(filepath.Clean(root)))
	return portBase + int(h.Sum32()%portRange)
}

// Info is .ws/web.json: where a workspace's web UI listens, and the token
// that opens it. The token outlives the server, so a browser that has
// signed in stays signed in across restarts; Port and Pid are 0 while no
// server is up.
type Info struct {
	Port  int    `json:"port"`
	Pid   int    `json:"pid"`
	Token string `json:"token"`
}

// InfoPath is where Info is kept.
func InfoPath(root string) string { return filepath.Join(root, ".ws", "web.json") }

// ReadInfo reads a workspace's Info. A missing file is a zero Info.
func ReadInfo(root string) (Info, error) {
	var i Info
	data, err := os.ReadFile(InfoPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return i, nil
	}
	if err != nil {
		return i, err
	}
	if err := json.Unmarshal(data, &i); err != nil {
		return Info{}, fmt.Errorf("%s isn't valid JSON: %w", InfoPath(root), err)
	}
	return i, nil
}

func writeInfo(root string, i Info) error {
	path := InfoPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "web.json.*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(data, '\n'))
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp.Name(), 0o600) // the token opens the workspace
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		os.Remove(tmp.Name())
	}
	return werr
}

func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Origin is the web UI's address, http://127.0.0.1:<port>.
func (i Info) Origin() string { return fmt.Sprintf("http://127.0.0.1:%d", i.Port) }

// SignInURL opens the web UI at path, signing the browser in on the way.
func (i Info) SignInURL(path string) string {
	return i.Origin() + "/auth?" + url.Values{"t": {i.Token}, "next": {safeNext(path)}}.Encode()
}

// safeNext is path if it's a path on this server, else "/": a sign-in link
// mustn't send the browser anywhere else.
func safeNext(path string) string {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.HasPrefix(path, "/\\") {
		return "/"
	}
	return path
}
