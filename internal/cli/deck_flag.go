package cli

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// A flag is the orchestrator's question for you. ⏎ on a flagged mission
// offers its suggested answers, your own, launching crew instead, or
// dismissing it; an answer clears the flag and runs the orchestrator again
// with it, so the mission moves on.

// deckFlagChoiceMsg is a pick from offerAnswers' list.
type deckFlagChoiceMsg struct {
	slug   string
	choice int
}

// Fixed choices after a flag's own options.
const (
	flagChoiceOwn = iota
	flagChoiceCrew
	flagChoiceDismiss
)

func (m deckModel) offerAnswers(dm *deckMission) (tea.Model, tea.Cmd) {
	f, slug := dm.M.Flag, dm.M.Slug
	items := append([]string(nil), f.Options...)
	items = append(items, "Write my own answer…", "Launch crew instead", "Dismiss the question")
	m.openPick("The orchestrator asks: "+f.Text, items, func(i int) tea.Cmd {
		return func() tea.Msg { return deckFlagChoiceMsg{slug: slug, choice: i} }
	})
	return m, nil
}

func (m deckModel) handleFlagChoice(msg deckFlagChoiceMsg) (tea.Model, tea.Cmd) {
	dm, ok := m.selectedMission()
	if !ok || dm.M.Slug != msg.slug || dm.M.Flag == nil {
		return m, nil
	}
	options := dm.M.Flag.Options
	if msg.choice < len(options) {
		return m, m.answerFlag(msg.slug, options[msg.choice])
	}
	switch msg.choice - len(options) {
	case flagChoiceOwn:
		m.mode, m.answering, m.newBriefing = deckModeNewTitle, true, false
		m.input.SetValue("")
		m.input.CharLimit = 2000
		m.input.Placeholder = "what the orchestrator should do"
		return m, m.input.Focus()
	case flagChoiceCrew:
		return m.launchOrOfferCrew(dm)
	case flagChoiceDismiss:
		store, slug := m.store, msg.slug
		return m, func() tea.Msg {
			store.UserActed(slug)
			store.SetFlag(slug, "", "")
			return deckReloadMsg{flash: "question dismissed"}
		}
	}
	return m, nil
}

// answerFlag answers a mission's flag and hands the answer to the
// orchestrator, which acts on it.
func (m deckModel) answerFlag(slug, answer string) tea.Cmd {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return nil
	}
	store, root, off := m.store, m.ws.Root, m.orchestratorOff
	return func() tea.Msg {
		if _, err := store.AnswerFlag(slug, answer); err != nil {
			return deckReloadMsg{flash: "couldn't answer: " + err.Error()}
		}
		if off {
			return deckReloadMsg{flash: "answered; the orchestrator is off ([orchestrator] enabled = false), so act on it yourself"}
		}
		orchestrateSpawner(root, slug, "", "answer")
		return deckReloadMsg{flash: "answered: the orchestrator is acting on it"}
	}
}
