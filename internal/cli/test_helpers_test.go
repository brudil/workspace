package cli

import tea "charm.land/bubbletea/v2"

// isQuitCmd returns true if cmd produces a tea.QuitMsg when invoked.
func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}
