package orchestrator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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

// Harness is the harness that runs the orchestrator: the default one if it
// can run headless, else Cursor.
func Harness() (harness.Orchestrator, error) {
	h, err := harness.Get(harness.Default())
	if err != nil {
		return nil, err
	}
	if o, ok := h.(harness.Orchestrator); ok {
		return o, nil
	}
	if h, err = harness.Get("cursor"); err == nil {
		if o, ok := h.(harness.Orchestrator); ok {
			return o, nil
		}
	}
	return nil, errors.New("no harness here can run a headless orchestrator")
}

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
	err := c.Run()
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("timed out after %s", Timeout)
	}
	return out.String(), err
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
