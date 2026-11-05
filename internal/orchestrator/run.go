package orchestrator

import (
	"bytes"
	"context"
	"fmt"
	"github.com/brudil/workspace/internal/execlog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/brudil/workspace/internal/crew"
	"github.com/brudil/workspace/internal/deckserver"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
)

const (
	// LogEvents is how many recent log events the orchestrator is shown.
	LogEvents       = 40
	transcriptLines = 200
	outputKeep      = 1500 // bytes of the orchestrator's final message kept in the log
)

var (
	// Timeout bounds one run; tests shorten it.
	Timeout = 10 * time.Minute
	// WaitDelay is how long to wait for output after killing a run that
	// timed out.
	WaitDelay = 5 * time.Second
)

// Run runs the orchestrator on a mission with its role, returning what it
// printed. It runs in its own process group so a timeout kills everything it
// started (a shell, a `gh pr checks --watch`), not just the agent: anything
// left holding its output would keep Run waiting forever.
func Run(l harness.Launch, root, slug string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	c := exec.CommandContext(ctx, l.Args[0], l.Args[1:]...)
	c.Dir = l.Dir
	c.Env = append(crew.Env(root, slug, ""), mission.EnvRole+"="+mission.RoleOrchestrator)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = WaitDelay
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	who := "orchestrator " + slug
	start := time.Now()
	if err := c.Start(); err != nil {
		execlog.Start(root, who, c.Args, c.Dir, c.Env, 0)
		execlog.Note(root, who, "failed to start: %v", err)
		return "", err
	}
	execlog.Start(root, who, c.Args, c.Dir, c.Env, c.Process.Pid)
	err := c.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("timed out after %s", Timeout)
	}
	result := "exit 0"
	if err != nil {
		result = err.Error()
	}
	execlog.End(root, who, c.Process.Pid, time.Since(start), fmt.Sprintf("%s · %d bytes of output", result, out.Len()))
	return out.String(), err
}

// PromptFile is where a run's prompt is written for the agent to read.
func PromptFile(store mission.Store, slug string) string {
	return filepath.Join(store.Dir(), slug, "orchestrator-prompt.md")
}

// WritePrompt writes the prompt where the agent will read it, and returns
// the one line that goes on the agent's command line instead. A long,
// multi-line prompt passed as an argument gets cursor-agent killed (as it
// did crew before their prompts were typed in), and could pass the
// argument size limit; a file has neither problem.
func WritePrompt(store mission.Store, slug, prompt string) (string, error) {
	path := PromptFile(store, slug)
	if err := os.WriteFile(path, []byte(prompt), 0o644); err != nil {
		return "", err
	}
	execlog.Note(store.Root, "orchestrator "+slug, "wrote its prompt (%d bytes) to %s", len(prompt), path)
	return "You are the orchestrator for the ws mission " + slug + ". Read " + path + " first: it has your instructions and the mission's context. Then follow it.", nil
}

// Summary is what's logged of a run: the end of its final message, or why
// it failed.
func Summary(out string, err error) string {
	s := clipTail(strings.TrimSpace(out), outputKeep, "…")
	if err != nil {
		if s != "" {
			return "orchestrator failed: " + err.Error() + " · " + s
		}
		return "orchestrator failed: " + err.Error()
	}
	return s
}

// CrewOutput is the last lines crew showed: from the deck server if it's
// still running there, else from its saved transcript.
func CrewOutput(root string, store mission.Store, slug, crewID string) string {
	text := ""
	if c, err := deckserver.Dial(root); err == nil {
		text, _ = c.Transcript(slug + "/" + crewID)
		c.Close()
	}
	if text == "" {
		if data, err := os.ReadFile(store.TranscriptPath(slug, crewID)); err == nil {
			text = string(data)
		}
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > transcriptLines {
		lines = lines[len(lines)-transcriptLines:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// DiffStat summarises a capsule's changes against its base.
func DiffStat(dir, base string) string {
	out, err := exec.Command("git", "-C", dir, "diff", "--stat", base+"...HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}
