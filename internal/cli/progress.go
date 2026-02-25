package cli

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

type repoState int

const (
	repoPending repoState = iota
	repoRunning
	repoDone
	repoSkipped
	repoFailed
)

type repoEntry struct {
	name        string
	displayName string // formatted display name for UI
	state       repoState
	err         error
	skipped     bool // for setup: repo already existed
}

// repoResultMsg is sent when an operation on a single repo completes.
type repoResultMsg struct {
	name    string
	err     error
	skipped bool
}

// operationFunc runs an operation on a single repo and returns an error (nil on success).
type operationFunc func(name string) (skipped bool, err error)

type operationModel struct {
	repos       []repoEntry
	op          operationFunc
	parallel    bool
	stopOnError bool
	spinner     spinner.Model
	done        int
}

func formatDisplayName(name string, displayNames map[string]string) string {
	if d, ok := displayNames[name]; ok {
		return fmt.Sprintf("%s (%s)", d, name)
	}
	return name
}

func newOperationModel(repoNames []string, displayNames map[string]string, op operationFunc, parallel bool, stopOnError bool) operationModel {
	entries := make([]repoEntry, len(repoNames))
	for i, name := range repoNames {
		entries[i] = repoEntry{name: name, displayName: formatDisplayName(name, displayNames)}
	}

	s := spinner.New()
	s.Spinner = spinner.Dot

	return operationModel{
		repos:       entries,
		op:          op,
		parallel:    parallel,
		stopOnError: stopOnError,
		spinner:     s,
	}
}

func (m operationModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick}

	if m.parallel {
		// Launch all repos concurrently
		for _, r := range m.repos {
			cmds = append(cmds, m.runRepo(r.name))
		}
		for i := range m.repos {
			m.repos[i].state = repoRunning
		}
	} else {
		// Launch only the first repo
		if len(m.repos) > 0 {
			m.repos[0].state = repoRunning
			cmds = append(cmds, m.runRepo(m.repos[0].name))
		}
	}

	return tea.Batch(cmds...)
}

func (m operationModel) runRepo(name string) tea.Cmd {
	op := m.op
	return func() tea.Msg {
		skipped, err := op(name)
		return repoResultMsg{name: name, err: err, skipped: skipped}
	}
}

func (m operationModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case repoResultMsg:
		for i := range m.repos {
			if m.repos[i].name != msg.name {
				continue
			}
			switch {
			case msg.err != nil:
				m.repos[i].state = repoFailed
				m.repos[i].err = msg.err
			case msg.skipped:
				m.repos[i].state = repoSkipped
			default:
				m.repos[i].state = repoDone
			}
			m.done++
			break
		}

		if m.done >= len(m.repos) {
			return m, tea.Quit
		}

		// Sequential mode: stop on failure or start the next pending step.
		if !m.parallel {
			if msg.err != nil && m.stopOnError {
				return m, tea.Quit
			}
			for i := range m.repos {
				if m.repos[i].state == repoPending {
					m.repos[i].state = repoRunning
					return m, m.runRepo(m.repos[i].name)
				}
			}
		}

		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m operationModel) View() string {
	var b strings.Builder

	for _, r := range m.repos {
		switch r.state {
		case repoPending:
			b.WriteString(fmt.Sprintf("  %s %s\n", ui.Dim.Render("·"), ui.Dim.Render(r.displayName)))
		case repoRunning:
			b.WriteString(fmt.Sprintf("  %s %s\n", m.spinner.View(), r.displayName))
		case repoDone:
			b.WriteString(fmt.Sprintf("  %s %s\n", ui.Green.Render("✓"), r.displayName))
		case repoSkipped:
			b.WriteString(fmt.Sprintf("  %s %s\n", ui.Dim.Render("·"), ui.Dim.Render(r.displayName+" already exists")))
		case repoFailed:
			b.WriteString(fmt.Sprintf("  %s %s: %v\n", ui.Red.Render("✗"), r.displayName, r.err))
		}
	}

	return b.String()
}

func (m operationModel) HasFailed() bool {
	for _, r := range m.repos {
		if r.state == repoFailed {
			return true
		}
	}
	return false
}

// runOperationSync runs the operation without bubbletea (for non-TTY contexts).
func runOperationSync(repoNames []string, displayNames map[string]string, op operationFunc, stopOnError bool) []repoEntry {
	results := make([]repoEntry, len(repoNames))
	for i, name := range repoNames {
		results[i] = repoEntry{name: name, displayName: formatDisplayName(name, displayNames)}
		skipped, err := op(name)
		switch {
		case err != nil:
			results[i].state = repoFailed
			results[i].err = err
			if stopOnError {
				return results[:i+1]
			}
		case skipped:
			results[i].state = repoSkipped
		default:
			results[i].state = repoDone
		}
	}
	return results
}

// fprintResults prints sync results to the given writer.
func fprintResults(w io.Writer, results []repoEntry) {
	for _, r := range results {
		switch r.state {
		case repoDone:
			fmt.Fprintf(w, "  %s %s\n", ui.Green.Render("✓"), r.displayName)
		case repoSkipped:
			fmt.Fprintf(w, "  %s %s\n", ui.Dim.Render("·"), ui.Dim.Render(r.displayName+" already exists"))
		case repoFailed:
			fmt.Fprintf(w, "  %s %s: %v\n", ui.Red.Render("✗"), r.displayName, r.err)
		}
	}
}

// cloneRepos runs SetupRepo for each repo with interactive or sync progress display.
// Returns the list of repos that were newly cloned.
func cloneRepos(ws *workspace.Workspace, repoNames []string, output io.Writer) ([]string, error) {
	var mu sync.Mutex
	var clonedRepos []string

	op := func(name string) (bool, error) {
		r := ws.SetupRepo(name)
		if r.Cloned {
			mu.Lock()
			clonedRepos = append(clonedRepos, name)
			mu.Unlock()
		}
		return r.Skipped, r.Err
	}

	if ui.IsInteractive() {
		m := newOperationModel(repoNames, ws.DisplayNames, op, false, false)
		p := tea.NewProgram(m, tea.WithOutput(output))
		if _, err := p.Run(); err != nil {
			return nil, err
		}
	} else {
		results := runOperationSync(repoNames, ws.DisplayNames, op, false)
		fprintResults(output, results)
	}

	return clonedRepos, nil
}

// runAfterCreateHooks runs after_create hooks for the given repos.
func runAfterCreateHooks(ws *workspace.Workspace, repos []string, stdout, stderr io.Writer) {
	for _, repo := range repos {
		if hook, ok := ws.AfterCreateHooks[repo]; ok {
			fmt.Fprintf(stderr, "\n  Running after_create hook for %s...\n", ws.FormatRepoName(repo))
			if err := workspace.RunHook(ws.MainWorktree(repo), hook, stdout, stderr); err != nil {
				fmt.Fprintf(stderr, "  %s after_create hook failed: %v\n", ui.Orange.Render("⚠"), err)
			}
		}
	}
}
