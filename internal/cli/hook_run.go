package cli

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// lineWriter is an io.Writer that splits written bytes into lines and sends
// each complete line to a channel. On Close, any remaining partial line is
// flushed.
type lineWriter struct {
	ch  chan string
	buf bytes.Buffer
	mu  sync.Mutex
}

func newLineWriter(ch chan string) *lineWriter {
	return &lineWriter{ch: ch}
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// No newline found — put the partial data back.
			w.buf.WriteString(line)
			break
		}
		w.ch <- strings.TrimRight(line, "\n")
	}
	return n, nil
}

func (w *lineWriter) Close() {
	w.mu.Lock()
	if w.buf.Len() > 0 {
		w.ch <- w.buf.String()
	}
	w.mu.Unlock()
	close(w.ch)
}

// Message types for the hook bubbletea model.
type hookLineMsg string

type hookDoneMsg struct{ err error }

// hookModel is a bubbletea model that shows a rolling window of hook output.
type hookModel struct {
	lines  []string
	done   bool
	err    error
	lineCh <-chan string
	doneCh <-chan error
}

func newHookModel(lineCh <-chan string, doneCh <-chan error) hookModel {
	return hookModel{
		lineCh: lineCh,
		doneCh: doneCh,
	}
}

// waitForHookOutput reads lines from lineCh. When lineCh closes, it reads
// the result from doneCh and returns hookDoneMsg.
func waitForHookOutput(lineCh <-chan string, doneCh <-chan error) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-lineCh
		if !ok {
			err := <-doneCh
			return hookDoneMsg{err: err}
		}
		return hookLineMsg(line)
	}
}

func (m hookModel) update(msg tea.Msg) (hookModel, tea.Cmd) {
	switch msg := msg.(type) {
	case hookLineMsg:
		m.lines = append(m.lines, string(msg))
		if len(m.lines) > 6 {
			m.lines = m.lines[len(m.lines)-6:]
		}
		return m, waitForHookOutput(m.lineCh, m.doneCh)

	case hookDoneMsg:
		m.done = true
		m.err = msg.err
		return m, nil
	}

	return m, nil
}

func (m hookModel) view(spin string) string {
	var b strings.Builder
	label := "Running hooks"

	if m.done {
		if m.err != nil {
			b.WriteString(fmt.Sprintf("  %s %s\n", ui.Red.Render("✗"), label))
			for _, line := range m.lines {
				b.WriteString(fmt.Sprintf("    │ %s\n", ui.Dim.Render(line)))
			}
		} else {
			b.WriteString(fmt.Sprintf("  %s %s\n", ui.Green.Render("✓"), label))
		}
	} else {
		b.WriteString(fmt.Sprintf("  %s %s\n", spin, label))
		for _, line := range m.lines {
			b.WriteString(fmt.Sprintf("    │ %s\n", ui.Dim.Render(line)))
		}
	}

	return b.String()
}

// capsuleCreateModel wraps operationModel and hookModel into a single
// bubbletea program so "Running hooks" shows as pending during earlier steps.
type capsuleCreateModel struct {
	op        operationModel
	hook      hookModel
	spinner   spinner.Model
	hasHook   bool
	phase     int // 0 = operations, 1 = hooks
	hookDirFn func() string
	hookCmd   string
}

func newCapsuleCreateModel(op operationModel, hasHook bool, hookDirFn func() string, hookCmd string) capsuleCreateModel {
	return capsuleCreateModel{
		op:        op,
		hasHook:   hasHook,
		spinner:   op.spinner,
		hookDirFn: hookDirFn,
		hookCmd:   hookCmd,
	}
}

func (m capsuleCreateModel) Init() tea.Cmd {
	return m.op.Init()
}

type startHookPhaseMsg struct{}

func (m capsuleCreateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case startHookPhaseMsg:
		m.phase = 1
		lineCh, doneCh := startHook(m.hookDirFn(), m.hookCmd)
		m.hook = newHookModel(lineCh, doneCh)
		return m, waitForHookOutput(m.hook.lineCh, m.hook.doneCh)

	case hookLineMsg, hookDoneMsg:
		var cmd tea.Cmd
		m.hook, cmd = m.hook.update(msg)
		if m.hook.done {
			return m, tea.Quit
		}
		return m, cmd

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.op.spinner = m.spinner
		return m, cmd

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil

	default:
		// Delegate to operation model (handles repoResultMsg).
		opModel, opCmd := m.op.Update(msg)
		m.op = opModel.(operationModel)

		// Check if operation phase just finished by inspecting model state
		// rather than calling opCmd() which would execute the next step.
		if m.op.done >= len(m.op.repos) || (m.op.stopOnError && m.op.HasFailed()) {
			if m.hasHook && !m.op.HasFailed() {
				return m, func() tea.Msg { return startHookPhaseMsg{} }
			}
			return m, tea.Quit
		}
		return m, opCmd
	}
}

func (m capsuleCreateModel) View() string {
	var b strings.Builder
	b.WriteString(m.op.View())

	if m.hasHook {
		if m.phase == 0 {
			b.WriteString(fmt.Sprintf("  %s %s\n", ui.Dim.Render("·"), ui.Dim.Render("Running hooks")))
		} else {
			b.WriteString(m.hook.view(m.spinner.View()))
		}
	}

	return b.String()
}

// HookErr returns the hook error from the final model state.
func (m capsuleCreateModel) HookErr() error {
	return m.hook.err
}

// startHook launches a hook command in a goroutine, returning channels for
// line-by-line output and the final result.
func startHook(dir, command string) (<-chan string, <-chan error) {
	lineCh := make(chan string, 64)
	doneCh := make(chan error, 1)
	w := newLineWriter(lineCh)

	go func() {
		err := workspace.RunHook(dir, command, w, w)
		w.Close()
		doneCh <- err
	}()

	return lineCh, doneCh
}
