package cli

import (
	"fmt"
	"os"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/ide"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "setup",
		Short:             "Clone all repos from ws.toml",
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}

			if err := ctx.WS.EnsureReposDir(); err != nil {
				return err
			}

			clonedRepos, err := cloneRepos(ctx.WS, ctx.WS.RepoNames, os.Stderr)
			if err != nil {
				return err
			}

			workspace.DisableWorkspaceGit(ctx.WS.Root)
			runPostCreateHooks(ctx.WS, clonedRepos, os.Stderr, os.Stderr)

			for _, name := range clonedRepos {
				ctx.WS.Board(name, workspace.GroundDir)
			}
			config.SaveBoarded(ctx.WS.Root, ctx.WS.Boarded)
			if err := ide.Regenerate(ctx.WS.Root, ctx.WS.Boarded, ctx.WS.DisplayNames, ctx.WS.Org); err != nil {
				fmt.Fprintf(os.Stderr, "  %s workspace files: %v\n", ui.Orange.Render("⚠"), err)
			}

			fmt.Fprintln(os.Stderr, "\nSetup complete. Run 'ws open' to open in your editor.")
			return nil
		},
	}
}
