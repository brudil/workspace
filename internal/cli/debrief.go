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

	cmd.Flags().IntVar(&days, "days", 14, "Inactivity threshold in days")

	return cmd
}

func runDebrief(ctx *Context, days int, repoFilter string) error {
	fmt.Fprintln(os.Stderr, "Scanning capsules...")
	fmt.Fprintln(os.Stderr)

	capsules := ctx.WS.FindAllCapsules(days, repoFilter)
	if len(capsules) == 0 {
		fmt.Fprintln(os.Stderr, "All clear — no capsules in orbit.")
		return nil
	}

	// Batch-fetch open PRs per repo for "still in orbit" annotations
	prsByBranch := fetchPRsByBranch(ctx, capsules)

	var debriefed, skipped, inOrbit int
	boardChanged := false

	for _, c := range capsules {
		label := fmt.Sprintf("%s/%s", ctx.WS.FormatRepoName(c.Repo), c.Name)

		if c.Merged || c.Inactive {
			if c.Dirty {
				// Landed/inactive but dirty — skip
				fmt.Fprintf(os.Stderr, "%-40s %s %s, but %d uncommitted files — skipped\n",
					label,
					ui.Red.Render("✗"),
					debriefReason(c),
					c.DirtyCount,
				)
				skipped++
			} else {
				// Landed/inactive and clean — remove
				if ctx.WS.IsBoarded(c.Repo, c.Name) {
					ctx.WS.Unboard(c.Repo, c.Name)
					boardChanged = true
				}

				if err := ctx.WS.RemoveWorktree(c.Repo, c.Name); err != nil {
					fmt.Fprintf(os.Stderr, "%-40s %s failed to remove: %v\n",
						label,
						ui.Red.Render("✗"),
						err,
					)
					skipped++
					continue
				}

				fmt.Fprintf(os.Stderr, "%-40s %s %s, clean — removing\n",
					label,
					ui.Green.Render("✓"),
					debriefReason(c),
				)
				debriefed++
			}
		} else {
			// Still in orbit
			extra := orbitExtra(c, prsByBranch)
			fmt.Fprintf(os.Stderr, "%-40s %s still in orbit%s\n",
				label,
				ui.Dim.Render("-"),
				extra,
			)
			inOrbit++
		}
	}

	// Save board state and regen IDE files once at the end
	if boardChanged {
		config.SaveBoarded(ctx.WS.Root, ctx.WS.Boarded)
		if err := ide.Regenerate(ctx.WS.Root, ctx.WS.Boarded, ctx.WS.DisplayNames, ctx.WS.Org); err != nil {
			fmt.Fprintf(os.Stderr, "\n  %s workspace files: %v\n", ui.Orange.Render("⚠"), err)
		}
	}

	// Summary
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

func fetchPRsByBranch(ctx *Context, capsules []workspace.CapsuleInfo) map[string]*github.PR {
	result := make(map[string]*github.PR)

	// Collect unique repos that have in-orbit capsules
	repoSet := make(map[string]bool)
	for _, c := range capsules {
		if !c.Merged && !c.Inactive {
			repoSet[c.Repo] = true
		}
	}

	for repo := range repoSet {
		prs, err := ctx.GitHub.PRsForRepo(ctx.WS.Org, repo)
		if err != nil {
			continue // gracefully degrade
		}
		for i := range prs {
			result[prs[i].HeadRefName] = &prs[i]
		}
	}

	return result
}
