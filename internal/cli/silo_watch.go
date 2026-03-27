package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

const maxSyncHistory = 3

type siloWatchModel struct {
	repos      []siloRepoView
	spinner    spinner.Model
	syncCh     <-chan workspace.SyncEvent
	onResync   func() // triggers FullResyncAll in a goroutine
	resyncing  bool
	lastResync time.Time
}

type siloRepoView struct {
	name        string
	displayName string
	capsule     string
	history     []syncEntry
}

type syncEntry struct {
	fileCount int
	time      time.Time
}

type syncEventMsg workspace.SyncEvent
type resyncDoneMsg struct{}

func newSiloWatchModel(ws *workspace.Workspace, syncCh <-chan workspace.SyncEvent, onResync func()) siloWatchModel {
	var repos []siloRepoView
	for _, name := range ws.RepoNames {
		capsule, ok := ws.Silo[name]
		if !ok {
			continue
		}
		repos = append(repos, siloRepoView{
			name:        name,
			displayName: ws.FormatRepoName(name),
			capsule:     capsule,
		})
	}
	s := spinner.New()
	s.Spinner = spinner.Dot
	return siloWatchModel{
		repos:    repos,
		spinner:  s,
		syncCh:   syncCh,
		onResync: onResync,
	}
}

func (m siloWatchModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.waitForSync())
}

func (m siloWatchModel) waitForSync() tea.Cmd {
	ch := m.syncCh
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return tea.Quit()
		}
		return syncEventMsg(ev)
	}
}

func (m siloWatchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case syncEventMsg:
		for i := range m.repos {
			if m.repos[i].name == msg.Repo {
				m.repos[i].capsule = msg.Capsule
				entry := syncEntry{fileCount: msg.FileCount, time: msg.Time}
				m.repos[i].history = append(m.repos[i].history, entry)
				if len(m.repos[i].history) > maxSyncHistory {
					m.repos[i].history = m.repos[i].history[len(m.repos[i].history)-maxSyncHistory:]
				}
				break
			}
		}
		return m, m.waitForSync()

	case resyncDoneMsg:
		m.resyncing = false
		m.lastResync = time.Now()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "r":
			if !m.resyncing {
				m.resyncing = true
				fn := m.onResync
				return m, func() tea.Msg {
					fn()
					return resyncDoneMsg{}
				}
			}
		}
	}
	return m, nil
}

func (m siloWatchModel) View() string {
	var b strings.Builder

	header := fmt.Sprintf("  %s Watching", m.spinner.View())
	if m.resyncing {
		header += fmt.Sprintf("  %s", ui.Orange.Render("resyncing..."))
	}
	b.WriteString(header + "\n")
	b.WriteString(fmt.Sprintf("  %s\n\n", ui.Dim.Render("r to resync, q to quit")))

	for _, repo := range m.repos {
		b.WriteString(fmt.Sprintf("  %s %s %s\n",
			ui.Bold.Render(repo.displayName),
			ui.Dim.Render("→"),
			ui.TagDim.Render(repo.capsule)))

		if len(repo.history) == 0 {
			b.WriteString(fmt.Sprintf("    %s\n", ui.Dim.Render("waiting for changes...")))
		} else {
			for _, entry := range repo.history {
				ts := entry.time.Format("15:04:05")
				b.WriteString(fmt.Sprintf("    %s %s %d file(s)\n",
					ui.Dim.Render(ts),
					ui.Green.Render("✓"),
					entry.fileCount))
			}
		}
		b.WriteByte('\n')
	}

	return b.String()
}
