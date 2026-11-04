package harness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Cursor runs the Cursor CLI agent.
//
// Every assumption about the Cursor CLI lives in this file and is marked
// GUESS. They have not been checked against a real cursor-agent yet; see
// docs/plans/2026-11-04-deck-crew-cursor-checklist.md for how to verify them.
type Cursor struct {
	// lookPath and run are swapped in tests.
	lookPath func(string) (string, error)
	run      func(ctx context.Context, bin string, args ...string) ([]byte, error)
}

// EnvCursorBin overrides the Cursor CLI binary (name or path).
const EnvCursorBin = "WS_CURSOR_AGENT"

// GUESS: the binary is installed as cursor-agent, with agent as a newer alias.
var cursorBinaries = []string{"cursor-agent", "agent"}

// GUESS: `create-chat` prints a new chat id (alone, on the last line) and
// exits, letting us know the conversation id before the agent starts.
var cursorCreateChatArgs = []string{"create-chat"}

const cursorCreateChatTimeout = 15 * time.Second

// A chat id is one token: letters, digits, dashes, underscores.
var chatIDShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{5,}$`)

func init() {
	register(&Cursor{
		lookPath: exec.LookPath,
		run: func(ctx context.Context, bin string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, bin, args...).Output()
		},
	})
}

func (c *Cursor) Name() string { return "cursor" }

func (c *Cursor) bin() (string, error) {
	if b := os.Getenv(EnvCursorBin); b != "" {
		return c.lookPath(b)
	}
	for _, name := range cursorBinaries {
		if p, err := c.lookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("cursor CLI not found (looked for %s; set %s to override)", strings.Join(cursorBinaries, ", "), EnvCursorBin)
}

func (c *Cursor) Available() error {
	_, err := c.bin()
	return err
}

func (c *Cursor) Start(b Brief) (Launch, error) {
	bin, err := c.bin()
	if err != nil {
		return Launch{}, err
	}
	conv := c.createChat(bin)
	args := []string{bin}
	if conv != "" {
		// Verified: --resume <id> opens the chat create-chat made,
		// interactively.
		args = append(args, "--resume", conv)
	}
	// The opening prompt is typed in once the agent has drawn (see
	// Launch.Prompt), not passed as an argument: a long multi-line prompt
	// argument got new crew killed before they rendered.
	return Launch{Args: args, Dir: b.Dir, Conversation: conv, Prompt: InitialPrompt(b)}, nil
}

func (c *Cursor) Resume(dir, conversation string) (Launch, error) {
	if conversation == "" {
		return Launch{}, errors.New("no cursor chat id recorded for this crew, so it can't be resumed")
	}
	bin, err := c.bin()
	if err != nil {
		return Launch{}, err
	}
	// GUESS: --resume <id> reopens an earlier chat interactively.
	return Launch{Args: []string{bin, "--resume", conversation}, Dir: dir, Conversation: conversation}, nil
}

// createChat asks the CLI for a chat id up front. Any failure just means the
// crew can't be resumed later; the agent still starts.
func (c *Cursor) createChat(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), cursorCreateChatTimeout)
	defer cancel()
	out, err := c.run(ctx, bin, cursorCreateChatArgs...)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if chatIDShape.MatchString(last) {
		return last
	}
	return ""
}
