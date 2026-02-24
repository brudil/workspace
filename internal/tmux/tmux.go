package tmux

import (
	"os"
	"os/exec"
	"strings"
)

// InTmux returns true when the current process is running inside a tmux session.
func InTmux() bool {
	return os.Getenv("TMUX") != ""
}

// WindowName builds the canonical tmux window name for a capsule.
func WindowName(displayName, capsule string) string {
	return displayName + ":" + capsule
}

// ListWindows returns a map of windowName → windowID for the current tmux session.
// Returns an empty map if not in tmux or if the query fails.
func ListWindows() map[string]string {
	out, err := exec.Command("tmux", "list-windows", "-F", "#{window_name} #{window_id}").CombinedOutput()
	if err != nil {
		return nil
	}
	return parseListWindows(string(out))
}

func parseListWindows(output string) map[string]string {
	result := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		idx := strings.LastIndex(line, " ")
		if idx < 0 {
			continue
		}
		name := line[:idx]
		id := line[idx+1:]
		result[name] = id
	}
	return result
}

// Pane represents a single tmux pane.
type Pane struct {
	ID      string
	Command string
}

// ListPanes returns the panes for a given window ID.
func ListPanes(windowID string) []Pane {
	out, err := exec.Command("tmux", "list-panes", "-t", windowID, "-F", "#{pane_id} #{pane_current_command}").CombinedOutput()
	if err != nil {
		return nil
	}
	return parseListPanes(string(out))
}

func parseListPanes(output string) []Pane {
	var panes []Pane
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		before, after, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		panes = append(panes, Pane{ID: before, Command: after})
	}
	return panes
}

var idleShells = map[string]bool{
	"zsh": true, "bash": true, "fish": true,
	"sh": true, "dash": true, "ksh": true,
}

// FindIdlePane returns the ID of the first pane running an idle shell, if any.
func FindIdlePane(panes []Pane) (string, bool) {
	for _, p := range panes {
		if idleShells[p.Command] {
			return p.ID, true
		}
	}
	return "", false
}

// SelectWindow switches to an existing tmux window by ID.
func SelectWindow(id string) error {
	return exec.Command("tmux", "select-window", "-t", id).Run()
}

// SelectPane switches to an existing tmux pane by ID.
func SelectPane(id string) error {
	return exec.Command("tmux", "select-pane", "-t", id).Run()
}

// SplitWindow creates a new pane in an existing window, starting in path.
func SplitWindow(windowID, path string) error {
	return exec.Command("tmux", "split-window", "-t", windowID, "-c", path).Start()
}

// NewWindow creates a new tmux window with the given name, starting in path.
func NewWindow(name, path string) error {
	return exec.Command("tmux", "new-window", "-n", name, "-c", path).Start()
}

// KillWindow closes a tmux window by ID.
func KillWindow(id string) error {
	return exec.Command("tmux", "kill-window", "-t", id).Run()
}
