package cli

import (
	"fmt"
	"os"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/ide"
	"github.com/brudil/workspace/internal/ui"
	"github.com/spf13/cobra"
)

func newBurnCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "burn [repo] <branch>",
		Aliases: []string{"rm"},
		Short:   "Remove a worktree",
		Args:    cobra.RangeArgs(1, 2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			switch len(args) {
			case 0:
				return completeRepoNames(cmd, args, toComplete)
			case 1:
				return completeWorktreeNames(0)(cmd, args, toComplete)
			default:
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}

			var repoArg, branch string
			if len(args) == 2 {
				repoArg = args[0]
				branch = args[1]
			} else {
				branch = args[0]
			}

			repo, err := ctx.ResolveRepo(repoArg)
			if err != nil {
				return err
			}

			capsule, err := ctx.ResolveCapsule(repo, branch)
			if err != nil {
				return err
			}

			check, err := ctx.WS.CheckRemoveWorktree(repo, capsule)
			if err != nil {
				return err
			}

			if check.IsDirty {
				fmt.Fprintf(os.Stderr, "  %s Worktree %s/%s has uncommitted changes\n", ui.Orange.Render("⚠"), ctx.WS.FormatRepoName(repo), capsule)
				confirmed, err := ui.Confirm("Remove anyway?")
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Fprintf(os.Stderr, "  %s Aborted\n", ui.Dim.Render("·"))
					return nil
				}
			}

			if ctx.WS.IsBoarded(repo, capsule) {
				ctx.WS.Unboard(repo, capsule)
				config.SaveBoarded(ctx.WS.Root, ctx.WS.Boarded)
				if err := ide.Regenerate(ctx.WS.Root, ctx.WS.Boarded, ctx.WS.DisplayNames, ctx.WS.Org); err != nil {
					fmt.Fprintf(os.Stderr, "  %s workspace files: %v\n", ui.Orange.Render("⚠"), err)
				}
			}

			if err := ctx.WS.RemoveWorktree(repo, capsule); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "  %s Removed %s %s\n", ui.Green.Render("✓"), ctx.WS.FormatRepoName(repo), ui.TagDim.Render(capsule))
			return nil
		},
	}
}
