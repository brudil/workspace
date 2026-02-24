package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:               "status",
		Short:             "Show repos, worktrees, and git status",
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}

			switch format {
			case "json":
				return runStatusJSON(ctx.WS, ctx.GitHub)
			case "llm":
				return runStatusLLM(ctx.WS, ctx.GitHub)
			}

			m := newStatusModel(ctx.WS, ctx.GitHub)
			p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
			_, err = p.Run()
			return err
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "", "Output format: json, llm")
	cmd.RegisterFlagCompletionFunc("format", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"json", "llm"}, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

// --- bubbletea model ---

type worktreeView struct {
	name   string
	branch string
	dirty  bool
	ahead  int
	behind int
	loaded bool
}

type repoView struct {
	name      string
	boarded   []string
	worktrees []worktreeView
	err       error
	prs       map[string]*github.PR // headRefName → PR, nil until loaded
	prsLoaded bool
}

type statusModel struct {
	ws       *workspace.Workspace
	gh       github.Client
	repos    []repoView
	total    int // total worktrees to query
	done     int // how many have come back
	prTotal  int // repo PR queries fired
	prDone   int // completed PR queries
	prErrors int // PR queries that failed
}

// message sent when a single worktree's git status is ready
type wtStatusMsg struct {
	repo string
	wt   workspace.WorktreeStatus
}

// message sent when PR data for a repo is ready
type repoPRsMsg struct {
	repo string
	prs  []github.PR
	err  error
}

func newStatusModel(ws *workspace.Workspace, gh github.Client) statusModel {
	outlines := ws.StatusOutline(false)
	repos := make([]repoView, len(outlines))
	total := 0
	prTotal := 0

	for i, o := range outlines {
		wts := make([]worktreeView, len(o.Worktrees))
		for j, name := range o.Worktrees {
			wts[j] = worktreeView{name: name}
			total++
		}
		repos[i] = repoView{
			name:      o.Name,
			boarded:   o.Boarded,
			worktrees: wts,
			err:       o.Err,
		}
		if o.Err == nil {
			prTotal++
		}
	}

	return statusModel{ws: ws, gh: gh, repos: repos, total: total, prTotal: prTotal}
}

func (m statusModel) Init() tea.Cmd {
	// Fire off one command per worktree + one PR query per repo
	var cmds []tea.Cmd
	for _, repo := range m.repos {
		if repo.err != nil {
			continue
		}
		for _, wt := range repo.worktrees {
			cmds = append(cmds, m.queryWorktree(repo.name, wt.name))
		}
		cmds = append(cmds, m.queryRepoPRs(repo.name))
	}
	return tea.Batch(cmds...)
}

func (m statusModel) queryWorktree(repo, wt string) tea.Cmd {
	repoDir := m.ws.RepoDir(repo)
	wtPath := filepath.Join(repoDir, wt)
	return func() tea.Msg {
		status := workspace.QueryWorktreeStatus(wtPath)
		return wtStatusMsg{repo: repo, wt: status}
	}
}

func (m statusModel) queryRepoPRs(repoName string) tea.Cmd {
	org := m.ws.Org
	return func() tea.Msg {
		prs, err := m.gh.PRsForRepo(org, repoName)
		return repoPRsMsg{repo: repoName, prs: prs, err: err}
	}
}

func (m statusModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case wtStatusMsg:
		for i := range m.repos {
			if m.repos[i].name != msg.repo {
				continue
			}
			for j := range m.repos[i].worktrees {
				if m.repos[i].worktrees[j].name == msg.wt.Name {
					m.repos[i].worktrees[j].branch = msg.wt.Branch
					m.repos[i].worktrees[j].dirty = msg.wt.Dirty
					m.repos[i].worktrees[j].ahead = msg.wt.Ahead
					m.repos[i].worktrees[j].behind = msg.wt.Behind
					m.repos[i].worktrees[j].loaded = true
					m.done++
					break
				}
			}
			break
		}
		if m.done >= m.total && m.prDone >= m.prTotal {
			return m, tea.Quit
		}
		return m, nil

	case repoPRsMsg:
		m.prDone++
		for i := range m.repos {
			if m.repos[i].name != msg.repo {
				continue
			}
			if msg.err == nil {
				m.repos[i].prs = make(map[string]*github.PR, len(msg.prs))
				for j := range msg.prs {
					m.repos[i].prs[msg.prs[j].HeadRefName] = &msg.prs[j]
				}
			} else {
				m.prErrors++
			}
			m.repos[i].prsLoaded = true
			break
		}
		if m.done >= m.total && m.prDone >= m.prTotal {
			return m, tea.Quit
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m statusModel) View() string {
	var b strings.Builder

	// Header
	b.WriteString(ui.Dim.Render(fmt.Sprintf("%s · %d repos", m.ws.Title(), len(m.repos))) + "\n\n")

	// First pass: build headers and find max width for consistent underlines
	headers := make([]string, len(m.repos))
	repoColors := make([]lipgloss.Color, len(m.repos))
	maxHeaderWidth := 0
	paletteIdx := 0
	for i, repo := range m.repos {
		if c, ok := m.ws.RepoColors[repo.name]; ok {
			repoColors[i] = lipgloss.Color(c)
		} else {
			repoColors[i] = repoPalette[paletteIdx%len(repoPalette)]
			paletteIdx++
		}
		nameStyle := lipgloss.NewStyle().Bold(true).Foreground(repoColors[i])
		repoName := nameStyle.Render(m.ws.DisplayNameFor(repo.name))
		if _, ok := m.ws.DisplayNames[repo.name]; ok {
			repoName += " " + ui.Dim.Render("("+repo.name+")")
		}
		headers[i] = repoName
		if w := lipgloss.Width(headers[i]); w > maxHeaderWidth {
			maxHeaderWidth = w
		}
	}

	// Counters for footer
	totalWorktrees := 0
	dirtyCount := 0
	behindCount := 0
	prCount := 0

	rule := strings.Repeat("─", maxHeaderWidth)

	for i, repo := range m.repos {
		borderColor := repoColors[i]

		if repo.err != nil {
			lines := []string{
				headers[i],
				rule,
				ui.Red.Render("error: " + repo.err.Error()),
			}
			b.WriteString(renderRepoBlock(lines, 1, borderColor) + "\n\n")
			continue
		}

		// Track totals
		for _, wt := range repo.worktrees {
			totalWorktrees++
			if wt.loaded {
				if wt.dirty {
					dirtyCount++
				}
				if wt.behind > 0 {
					behindCount++
				}
			}
		}
		prCount += len(repo.prs)

		cols := computeColumns(repo.worktrees)
		lines := []string{headers[i], rule}
		for _, wt := range repo.worktrees {
			var pr *github.PR
			if repo.prs != nil {
				pr = lookupPR(repo.prs, wt.branch, wt.name)
			}
			lines = append(lines, formatWorktreeLine(wt, slices.Contains(repo.boarded, wt.name), cols, pr))
		}
		b.WriteString(renderRepoBlock(lines, 1, borderColor) + "\n\n")
	}

	// Footer — only when fully loaded
	if m.done >= m.total {
		b.WriteString(formatFooter(len(m.repos), totalWorktrees, dirtyCount, behindCount, prCount, m.prErrors) + "\n")
	}

	return b.String()
}
