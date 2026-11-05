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
// Every assumption about the Cursor CLI lives in this file. Those checked
// against a real cursor-agent say VERIFIED; any left marked GUESS haven't
// been, see docs/plans/2026-11-04-deck-crew-cursor-checklist.md.
type Cursor struct {
	// lookPath and run are swapped in tests.
	lookPath func(string) (string, error)
	run      func(ctx context.Context, bin string, args ...string) ([]byte, error)
}

// EnvCursorBin overrides the Cursor CLI binary (name or path).
const EnvCursorBin = "WS_CURSOR_AGENT"

// VERIFIED: the binary is installed as cursor-agent, with agent as a newer alias.
var cursorBinaries = []string{"cursor-agent", "agent"}

// VERIFIED: `create-chat` prints a new chat id (alone, on the last line) and
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
	conv, note := c.createChat(bin)
	args := []string{bin}
	if conv != "" {
		// Verified: --resume <id> opens the chat create-chat made,
		// interactively.
		args = append(args, "--resume", conv)
	}
	// The opening prompt is typed in once the agent has drawn (see
	// Launch.Prompt), not passed as an argument: a long multi-line prompt
	// argument got new crew killed before they rendered.
	return Launch{Args: args, Dir: b.Dir, Conversation: conv, Prompt: InitialPrompt(b), Notes: []string{note}}, nil
}

func (c *Cursor) Resume(dir, conversation string) (Launch, error) {
	if conversation == "" {
		return Launch{}, errors.New("no cursor chat id recorded for this crew, so it can't be resumed")
	}
	bin, err := c.bin()
	if err != nil {
		return Launch{}, err
	}
	// VERIFIED: --resume <id> reopens an earlier chat interactively.
	return Launch{Args: []string{bin, "--resume", conversation}, Dir: dir, Conversation: conversation}, nil
}

// createChat asks the CLI for a chat id up front. Any failure just means the
// crew can't be resumed later; the agent still starts. The note says what
// happened, for the exec log.
func (c *Cursor) createChat(bin string) (id, note string) {
	ctx, cancel := context.WithTimeout(context.Background(), cursorCreateChatTimeout)
	defer cancel()
	ran := bin + " " + strings.Join(cursorCreateChatArgs, " ")
	start := time.Now()
	out, err := c.run(ctx, bin, cursorCreateChatArgs...)
	took := time.Since(start).Round(100 * time.Millisecond)
	if err != nil {
		detail := err.Error()
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			detail += ": " + strings.TrimSpace(string(ee.Stderr))
		}
		return "", fmt.Sprintf("ran %s: failed after %s (%s); starting without a chat id, so this crew can't be resumed", ran, took, detail)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if chatIDShape.MatchString(last) {
		return last, fmt.Sprintf("ran %s: chat %s (%s)", ran, last, took)
	}
	return "", fmt.Sprintf("ran %s: no chat id in its output (last line %q); starting without one, so this crew can't be resumed", ran, last)
}

// VERIFIED (cursor-agent --help): -p/--print runs one prompt headless with
// every tool, --output-format text (the default) prints the final message,
// --force runs commands unless explicitly denied, --trust skips the
// workspace-trust prompt nobody is there to answer, and --model picks the
// model. GUESS: the orchestrator's default model name is one Cursor accepts
// (cursor-agent --list-models lists them).
var cursorPrintArgs = []string{"-p", "--force", "--trust", "--output-format", "text"}

// Orchestrate runs a headless orchestrator with Cursor's print mode.
func (c *Cursor) Orchestrate(prompt, dir, model string) (Launch, error) {
	bin, err := c.bin()
	if err != nil {
		return Launch{}, err
	}
	args := append([]string{bin}, cursorPrintArgs...)
	if model != "" {
		args = append(args, "--model", model)
	}
	return Launch{Args: append(args, prompt), Dir: dir}, nil
}
