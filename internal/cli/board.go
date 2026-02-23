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
	return &cobra.Command{
		Use:   "board <repo> <capsule>",
		Short: "Add a capsule to IDE workspace files",
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

			if err := ctx.WS.Board(repo, capsule); err != nil {
				return err
			}

			if err := config.SaveBoarded(ctx.WS.Root, ctx.WS.Boarded); err != nil {
				return fmt.Errorf("saving board state: %w", err)
			}

			if err := ide.Regenerate(ctx.WS.Root, ctx.WS.Boarded, ctx.WS.DisplayNames, ctx.WS.Org); err != nil {
				fmt.Fprintf(os.Stderr, "  %s workspace files: %v\n", ui.Orange.Render("⚠"), err)
			}

			fmt.Fprintf(os.Stderr, "  %s Boarded %s/%s\n", ui.Green.Render("✓"), ctx.WS.FormatRepoName(repo), capsule)
			return nil
		},
	}
}

func newUnboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unboard <repo> <capsule>",
		Short: "Remove a capsule from IDE workspace files",
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

			if err := ctx.WS.Unboard(repo, capsule); err != nil {
				return err
			}

			if err := config.SaveBoarded(ctx.WS.Root, ctx.WS.Boarded); err != nil {
				return fmt.Errorf("saving board state: %w", err)
			}

			if err := ide.Regenerate(ctx.WS.Root, ctx.WS.Boarded, ctx.WS.DisplayNames, ctx.WS.Org); err != nil {
				fmt.Fprintf(os.Stderr, "  %s workspace files: %v\n", ui.Orange.Render("⚠"), err)
			}

			fmt.Fprintf(os.Stderr, "  %s Unboarded %s/%s\n", ui.Green.Render("✓"), ctx.WS.FormatRepoName(repo), capsule)
			return nil
		},
	}
}
