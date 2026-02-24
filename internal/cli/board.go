package cli

import (
	"fmt"
	"os"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/ide"
	"github.com/brudil/workspace/internal/ui"
	"github.com/spf13/cobra"
)

func newBoardCmd() *cobra.Command {
	return newBoardToggleCmd("board", "Add a capsule to IDE workspace files", "Boarded",
		func(ctx *Context, repo, capsule string) error { return ctx.WS.Board(repo, capsule) })
}

func newUnboardCmd() *cobra.Command {
	return newBoardToggleCmd("unboard", "Remove a capsule from IDE workspace files", "Unboarded",
		func(ctx *Context, repo, capsule string) error { return ctx.WS.Unboard(repo, capsule) })
}

func newBoardToggleCmd(use, short, verb string, op func(*Context, string, string) error) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <repo> <capsule>",
		Short: short,
		Args:  cobra.ExactArgs(2),
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

			repo, err := ctx.ResolveRepo(args[0])
			if err != nil {
				return err
			}
			capsule := args[1]

			if err := op(ctx, repo, capsule); err != nil {
				return err
			}

			if err := config.SaveBoarded(ctx.WS.Root, ctx.WS.Boarded); err != nil {
				return fmt.Errorf("saving board state: %w", err)
			}

			if err := ide.Regenerate(ctx.WS.Root, ctx.WS.Boarded, ctx.WS.DisplayNames, ctx.WS.Org); err != nil {
				fmt.Fprintf(os.Stderr, "  %s workspace files: %v\n", ui.Orange.Render("⚠"), err)
			}

			fmt.Fprintf(os.Stderr, "  %s %s %s %s\n", ui.Green.Render("✓"), verb, ctx.WS.FormatRepoName(repo), ui.TagDim.Render(capsule))
			return nil
		},
	}
}
