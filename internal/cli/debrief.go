package cli

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/ide"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func newDebriefCmd() *cobra.Command {
	var days int

	cmd := &cobra.Command{
		Use:   "debrief [repo]",
		Short: "Clean up landed capsules and report orbit status",
		Args:  cobra.MaximumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeRepoNames(cmd, args, toComplete)
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}

			var repoFilter string
			if len(args) == 1 {
				resolved, err := ctx.ResolveRepo(args[0])
				if err != nil {
					return err
				}
				repoFilter = resolved
			}

			return runDebrief(ctx, days, repoFilter)
		},
	}

	cmd.Flags().IntVar(&days, "days", 90, "Inactivity threshold in days")

	return cmd
}

func runDebrief(ctx *Context, days int, repoFilter string) error {
	var capsules []workspace.CapsuleInfo
	var prsByBranch map[string]*github.PR
	var mergedBranches map[string]bool

	repos := ctx.WS.RepoNames
	if repoFilter != "" {
		repos = []string{repoFilter}
	}

	if ui.IsInteractive() {
		m := newDebriefModel(ctx, repos, days, repoFilter)
		p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
		result, err := p.Run()
		if err != nil {
			return err
		}
		dm := result.(debriefModel)
		capsules = dm.capsules
		prsByBranch = dm.prsByBranch
		mergedBranches = dm.mergedBranches
	} else {
		// Non-interactive: parallel fetch, then sequential steps.
		fetchResults := make([]workspace.FetchResult, len(repos))
		var wg sync.WaitGroup
		for i, repo := range repos {
			wg.Add(1)
			go func(idx int, name string) {
				defer wg.Done()
				fetchResults[idx] = ctx.WS.FetchRepo(name)
			}(i, repo)
		}
		wg.Wait()
		for i, repo := range repos {
			if fetchResults[i].Err == nil {
				workspace.GitFFMerge(ctx.WS.MainWorktree(repo), "origin/"+ctx.WS.DefaultBranch)
			}
		}
		capsules = ctx.WS.FindAllCapsules(days, repoFilter)
		if len(capsules) > 0 {
			prsByBranch, mergedBranches = fetchPRsByBranch(ctx, capsules)
		}
	}

	if len(capsules) == 0 {
		fmt.Fprintln(os.Stderr, "\nAll clear — no capsules in orbit.")
		return nil
	}

	// Cross-reference: mark capsules as merged if they have a merged PR
	for i := range capsules {
		if !capsules[i].Merged && mergedBranches[capsules[i].Branch] {
			capsules[i].Merged = true
		}
	}

	fmt.Fprintln(os.Stderr)

	// Compute column widths for aligned output.
	var maxRepoW, maxTagW int
	for _, c := range capsules {
		if w := lipgloss.Width(ctx.WS.FormatRepoName(c.Repo)); w > maxRepoW {
			maxRepoW = w
		}
		if w := lipgloss.Width(ui.TagDim.Render(c.Name)); w > maxTagW {
			maxTagW = w
		}
	}

	var debriefed, skipped, inOrbit int
	boardChanged := false
	siloChanged := false

	for _, c := range capsules {
		repoName := ctx.WS.FormatRepoName(c.Repo)
		tag := ui.TagDim.Render(c.Name)
		repoPad := strings.Repeat(" ", maxRepoW-lipgloss.Width(repoName))
		tagPad := strings.Repeat(" ", maxTagW-lipgloss.Width(tag))

		if c.Merged || c.Inactive {
			if c.Dirty {
				fmt.Fprintf(os.Stderr, "  %s %s%s %s%s %s, but %d uncommitted files — skipped\n",
					ui.Red.Render("✗"), repoName, repoPad, tag, tagPad,
					debriefReason(c), c.DirtyCount,
				)
				skipped++
			} else {
				if ctx.WS.IsBoarded(c.Repo, c.Name) {
					ctx.WS.Unboard(c.Repo, c.Name)
					boardChanged = true
				}

				if err := ctx.WS.RemoveWorktree(c.Repo, c.Name); err != nil {
					fmt.Fprintf(os.Stderr, "  %s %s%s %s%s failed to remove: %v\n",
						ui.Red.Render("✗"), repoName, repoPad, tag, tagPad, err,
					)
					skipped++
					continue
				}

				// If this capsule was a silo target, repoint to .ground
				if target, ok := ctx.WS.Silo[c.Repo]; ok && target == c.Name {
					ctx.WS.Silo[c.Repo] = workspace.GroundDir
					siloDir := ctx.WS.SiloWorktree(c.Repo)
					groundDir := ctx.WS.MainWorktree(c.Repo)
					if _, err := workspace.FullSync(groundDir, siloDir); err != nil {
						fmt.Fprintf(os.Stderr, "  %s silo re-sync failed: %v\n", ui.Orange.Render("⚠"), err)
					} else {
						siloChanged = true
						fmt.Fprintf(os.Stderr, "  %s Silo repointed to .ground\n", ui.Green.Render("✓"))
					}
				}

				fmt.Fprintf(os.Stderr, "  %s %s%s %s%s %s, clean — removed\n",
					ui.Green.Render("✓"), repoName, repoPad, tag, tagPad, debriefReason(c),
				)
				debriefed++
			}
		} else {
			extra := orbitExtra(c, prsByBranch)
			fmt.Fprintf(os.Stderr, "  %s %s%s %s%s still in orbit%s\n",
				ui.Dim.Render("-"), repoName, repoPad, tag, tagPad, extra,
			)
			inOrbit++
		}
	}

	if boardChanged {
		config.SaveBoarded(ctx.WS.Root, ctx.WS.Boarded)
		if err := ide.Regenerate(ctx.WS.Root, ctx.WS.Boarded, ctx.WS.DisplayNames, ctx.WS.Org); err != nil {
			fmt.Fprintf(os.Stderr, "\n  %s workspace files: %v\n", ui.Orange.Render("⚠"), err)
		}
	}
	if siloChanged {
		config.SaveSilo(ctx.WS.Root, ctx.WS.Silo)
	}

	fmt.Fprintln(os.Stderr)
	summary := fmt.Sprintf("Debriefed %d capsule", debriefed)
	if debriefed != 1 {
		summary += "s"
	}
	summary += "."
	if skipped > 0 {
		summary += fmt.Sprintf(" %d skipped.", skipped)
	}
	if inOrbit > 0 {
		summary += fmt.Sprintf(" %d still in orbit.", inOrbit)
	}
	fmt.Fprintln(os.Stderr, summary)

	return nil
}

func debriefReason(c workspace.CapsuleInfo) string {
	if c.Merged {
		return "landed"
	}
	return fmt.Sprintf("inactive (%d days)", int(workspace.DaysSince(c.LastCommit)))
}

func orbitExtra(c workspace.CapsuleInfo, prsByBranch map[string]*github.PR) string {
	var parts []string

	age := workspace.FormatAge(c.LastCommit)
	if age != "" {
		parts = append(parts, age)
	}

	if c.Ahead > 0 || c.Behind > 0 {
		parts = append(parts, fmt.Sprintf("%d↑ %d↓", c.Ahead, c.Behind))
	} else {
		parts = append(parts, "aligned with ground")
	}

	if pr, ok := prsByBranch[c.Branch]; ok && pr != nil {
		parts = append(parts, fmt.Sprintf("PR #%d open", pr.Number))
	}

	if len(parts) == 0 {
		return ""
	}
	var result strings.Builder
	result.WriteString(" (")
	for i, p := range parts {
		if i > 0 {
			result.WriteString(", ")
		}
		result.WriteString(p)
	}
	result.WriteString(")")
	return result.String()
}

// --- debrief bubbletea model ---

type debriefAlignMsg struct {
	name string
	err  error
}

type debriefScanMsg struct {
	capsules []workspace.CapsuleInfo
}

type debriefFetchPRsMsg struct {
	prsByBranch    map[string]*github.PR
	mergedBranches map[string]bool
}

type debriefModel struct {
	phase   int // 0=align, 1=scan, 2=fetchPRs, 3=done
	repos   []repoEntry
	spinner spinner.Model

	alignDone  int
	skippedPRs bool

	// Results extracted after Run().
	capsules       []workspace.CapsuleInfo
	prsByBranch    map[string]*github.PR
	mergedBranches map[string]bool

	// Config for scan/fetch phases.
	ctx        *Context
	repoNames  []string
	days       int
	repoFilter string
}

func newDebriefModel(ctx *Context, repos []string, days int, repoFilter string) debriefModel {
	entries := make([]repoEntry, len(repos))
	for i, name := range repos {
		entries[i] = repoEntry{
			name:        name,
			displayName: ctx.WS.FormatRepoName(name),
		}
	}
	s := spinner.New()
	s.Spinner = spinner.Dot
	return debriefModel{
		repos:      entries,
		spinner:    s,
		ctx:        ctx,
		repoNames:  repos,
		days:       days,
		repoFilter: repoFilter,
	}
}

func (m debriefModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick}
	for i := range m.repos {
		m.repos[i].state = repoRunning
		name := m.repos[i].name
		ctx := m.ctx
		cmds = append(cmds, func() tea.Msg {
			err := workspace.GitFetch(ctx.WS.BareDir(name))
			if err == nil {
				workspace.GitFFMerge(ctx.WS.MainWorktree(name), "origin/"+ctx.WS.DefaultBranch)
			}
			return debriefAlignMsg{name: name, err: err}
		})
	}
	return tea.Batch(cmds...)
}

func (m debriefModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case debriefAlignMsg:
		for i := range m.repos {
			if m.repos[i].name != msg.name {
				continue
			}
			if msg.err != nil {
				m.repos[i].state = repoFailed
				m.repos[i].err = msg.err
			} else {
				m.repos[i].state = repoDone
			}
			m.alignDone++
			break
		}
		if m.alignDone >= len(m.repos) {
			m.phase = 1
			return m, m.runScan()
		}
		return m, nil

	case debriefScanMsg:
		m.capsules = msg.capsules
		if len(m.capsules) == 0 {
			m.phase = 3
			m.skippedPRs = true
			return m, tea.Quit
		}
		m.phase = 2
		return m, m.runFetchPRs()

	case debriefFetchPRsMsg:
		m.prsByBranch = msg.prsByBranch
		m.mergedBranches = msg.mergedBranches
		m.phase = 3
		return m, tea.Quit

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

func (m debriefModel) runScan() tea.Cmd {
	ctx := m.ctx
	days := m.days
	repoFilter := m.repoFilter
	return func() tea.Msg {
		return debriefScanMsg{capsules: ctx.WS.FindAllCapsules(days, repoFilter)}
	}
}

func (m debriefModel) runFetchPRs() tea.Cmd {
	ctx := m.ctx
	capsules := m.capsules
	return func() tea.Msg {
		prs, merged := fetchPRsByBranch(ctx, capsules)
		return debriefFetchPRsMsg{prsByBranch: prs, mergedBranches: merged}
	}
}

func (m debriefModel) View() string {
	var b strings.Builder

	// Aligning ground
	if m.phase == 0 {
		b.WriteString(fmt.Sprintf("  %s Aligning ground\n", m.spinner.View()))
		for _, r := range m.repos {
			switch r.state {
			case repoPending:
				b.WriteString(fmt.Sprintf("    %s %s\n", ui.Dim.Render("·"), ui.Dim.Render(r.displayName)))
			case repoRunning:
				b.WriteString(fmt.Sprintf("    %s %s\n", m.spinner.View(), r.displayName))
			case repoDone:
				b.WriteString(fmt.Sprintf("    %s %s\n", ui.Green.Render("✓"), r.displayName))
			case repoFailed:
				b.WriteString(fmt.Sprintf("    %s %s: %v\n", ui.Red.Render("✗"), r.displayName, r.err))
			}
		}
	} else {
		b.WriteString(fmt.Sprintf("  %s Aligning ground\n", ui.Green.Render("✓")))
	}

	// Scanning capsules
	switch {
	case m.phase < 1:
		b.WriteString(fmt.Sprintf("  %s %s\n", ui.Dim.Render("·"), ui.Dim.Render("Scanning capsules")))
	case m.phase == 1:
		b.WriteString(fmt.Sprintf("  %s Scanning capsules\n", m.spinner.View()))
	default:
		b.WriteString(fmt.Sprintf("  %s Scanning capsules\n", ui.Green.Render("✓")))
	}

	// Fetching PRs
	if m.skippedPRs {
		b.WriteString(fmt.Sprintf("  %s %s\n", ui.Dim.Render("·"), ui.Dim.Render("Fetching PRs")))
	} else {
		switch {
		case m.phase < 2:
			b.WriteString(fmt.Sprintf("  %s %s\n", ui.Dim.Render("·"), ui.Dim.Render("Fetching PRs")))
		case m.phase == 2:
			b.WriteString(fmt.Sprintf("  %s Fetching PRs\n", m.spinner.View()))
		default:
			b.WriteString(fmt.Sprintf("  %s Fetching PRs\n", ui.Green.Render("✓")))
		}
	}

	return b.String()
}

func fetchPRsByBranch(ctx *Context, capsules []workspace.CapsuleInfo) (map[string]*github.PR, map[string]bool) {
	result := make(map[string]*github.PR)
	mergedBranches := make(map[string]bool)

	// Collect unique repos that have any capsules
	allRepos := make(map[string]bool)
	for _, c := range capsules {
		allRepos[c.Repo] = true
	}

	for repo := range allRepos {
		prs, err := ctx.GitHub.PRsForRepo(ctx.WS.Org, repo)
		if err == nil {
			for i := range prs {
				result[prs[i].HeadRefName] = &prs[i]
			}
		}

		// Also check for merged PRs to detect squash/rebase merges
		merged, err := ctx.GitHub.MergedPRsForRepo(ctx.WS.Org, repo)
		if err == nil {
			for _, pr := range merged {
				mergedBranches[pr.HeadRefName] = true
			}
		}
	}

	return result, mergedBranches
}
