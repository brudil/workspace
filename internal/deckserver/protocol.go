// Package deckserver keeps crew (agents) running when no deck is attached.
//
// One server per workspace owns the crew pseudo-terminals and their emulated
// screens. Decks connect over a Unix socket as thin clients: they send input
// and get rendered frames back, so detaching and reattaching loses nothing.
//
// The protocol is newline-delimited JSON over one connection. Requests carry
// an id and get exactly one reply with that id; events (frames, raw output,
// exits) carry no id.
package deckserver

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"

	uv "github.com/charmbracelet/ultraviolet"
)

// ProtocolVersion changes whenever messages change incompatibly. A deck
// refuses a server speaking another version.
const ProtocolVersion = 1

// Spec describes crew to launch.
type Spec struct {
	Slug    string   `json:"slug"`
	CrewID  string   `json:"crew"`
	Harness string   `json:"harness"`
	Capsule string   `json:"capsule"`
	Role    string   `json:"role,omitempty"`
	Args    []string `json:"args"`
	Dir     string   `json:"dir"`
	Env     []string `json:"env"`
	Width   int      `json:"w"`
	Height  int      `json:"h"`
}

// Key returns the crew's identity on the server.
func (s Spec) Key() string { return s.Slug + "/" + s.CrewID }

// CrewInfo is running crew as listed by the server.
type CrewInfo struct {
	Spec Spec `json:"spec"`
	Pid  int  `json:"pid"`
}

type request struct {
	ID  int    `json:"id"`
	Op  string `json:"op"`
	Ver int    `json:"ver,omitempty"`

	Crew   string            `json:"crew,omitempty"`
	Spec   *Spec             `json:"spec,omitempty"`
	Key    *uv.KeyPressEvent `json:"key,omitempty"`
	Mouse  *uv.Mouse         `json:"mouse,omitempty"`
	Text   string            `json:"text,omitempty"`
	Data   []byte            `json:"data,omitempty"`
	W      int               `json:"w,omitempty"`
	H      int               `json:"h,omitempty"`
	Offset int               `json:"offset,omitempty"`
	On     bool              `json:"on,omitempty"`
	Grace  int               `json:"grace_ms,omitempty"`
}

// message is a reply (ID set) or an event (Ev set).
type message struct {
	ID    int    `json:"id,omitempty"`
	Error string `json:"error,omitempty"`

	Ver   int        `json:"ver,omitempty"`
	Pid   int        `json:"pid,omitempty"`
	Crews []CrewInfo `json:"crews,omitempty"`

	Ev    string `json:"ev,omitempty"`
	Crew  string `json:"crew,omitempty"`
	Frame *Frame `json:"frame,omitempty"`
	Data  []byte `json:"data,omitempty"`
	Exit  string `json:"exit,omitempty"`
}

// Frame is one rendering of a crew's screen for one attached deck.
type Frame struct {
	Screen        string `json:"screen"`
	Offset        int    `json:"offset"`
	ScrollbackLen int    `json:"sb"`
	AltScreen     bool   `json:"alt"`
	CursorX       int    `json:"cx"`
	CursorY       int    `json:"cy"`
	CursorVisible bool   `json:"cv"`
	Title         string `json:"title,omitempty"`
	Width         int    `json:"w"`
	Height        int    `json:"h"`
}

// maxSocketPath is under the smallest sun_path limit we run on (104 on macOS).
const maxSocketPath = 100

// SocketPath is where a workspace's server listens.
func SocketPath(root string) string {
	p := filepath.Join(root, ".ws", "deck.sock")
	if len(p) <= maxSocketPath {
		return p
	}
	sum := sha1.Sum([]byte(root))
	return filepath.Join(os.TempDir(), "ws-deck-"+hex.EncodeToString(sum[:6])+".sock")
}

// LogPath is where an auto-started server writes its log.
func LogPath(root string) string { return filepath.Join(root, ".ws", "deck-server.log") }
