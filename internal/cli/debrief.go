package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/ide"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
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

	stepNames := []string{"Scanning capsules", "Fetching PRs"}
	op := func(name string) (bool, error) {
		switch name {
		case "Scanning capsules":
			capsules = ctx.WS.FindAllCapsules(days, repoFilter)
			return false, nil
		case "Fetching PRs":
			if len(capsules) == 0 {
				return true, nil
			}
			prsByBranch, mergedBranches = fetchPRsByBranch(ctx, capsules)
			return false, nil
		}
		return false, nil
	}

	if ui.IsInteractive() {
		m := newOperationModel(stepNames, nil, op, false, true)
		p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
		if _, err := p.Run(); err != nil {
			return err
		}
	} else {
		results := runOperationSync(stepNames, nil, op, true)
		fprintResults(os.Stderr, results)
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
