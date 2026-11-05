package cli

import (
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// clipboardCmd puts text on the clipboard two ways: through the terminal
// (OSC 52, which reaches your machine over SSH too, where the terminal
// allows it) and through pbcopy where there is one, since some terminals
// refuse OSC 52 by default (iTerm2 asks you to turn it on).
func clipboardCmd(text string) tea.Cmd {
	return tea.Batch(tea.SetClipboard(text), func() tea.Msg {
		if path, err := exec.LookPath("pbcopy"); err == nil {
			c := exec.Command(path)
			c.Stdin = strings.NewReader(text)
			_ = c.Run()
		}
		return nil
	})
}
