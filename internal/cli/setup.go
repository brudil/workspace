package cli

import (
	"fmt"
	"os"

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
			runAfterCreateHooks(ctx.WS, clonedRepos, os.Stderr, os.Stderr)

			fmt.Fprintln(os.Stderr, "\nSetup complete. Run 'ws open' to open in your editor.")
			return nil
		},
	}
}
