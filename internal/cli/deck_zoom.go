package cli

import tea "charm.land/bubbletea/v2"

// zoom is filled in by the raw passthrough work.
func (m deckModel) zoom(r *deckCrewRun) (tea.Model, tea.Cmd) {
	m.flash = "zoom isn't available yet"
	return m, nil
}
