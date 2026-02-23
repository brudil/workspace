package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/ide"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/spf13/cobra"
)

func newLiftCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lift <repo> <capsule-name> [base]",
		Short: "Create a new capsule",
		Long: `Create a new branch and worktree for fresh work. Use "." as the repo to
infer from the current directory. Base defaults to origin/<default-branch>.

Examples:
  ws lift frontend my-feature
  ws lift . my-feature
  ws lift frontend my-feature develop`,
		Args: cobra.RangeArgs(2, 3),
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

			repo, err := ctx.ResolveRepo(args[0])
			if err != nil {
				return err
			}

			branch := args[1]

			base := "origin/" + ctx.WS.DefaultBranch
			if len(args) == 3 {
				base = args[2]
			}

			capsule, err := ctx.WS.LiftWorktree(repo, branch, base)
			if err != nil {
				return err
			}

			wtPath := filepath.Join(ctx.WS.RepoDir(repo), capsule)

			fmt.Fprintf(os.Stderr, "  %s Lifted %s %s\n", ui.Green.Render("✓"), ctx.WS.DisplayNameFor(repo), ui.TagDim.Render(capsule))

			if hook, ok := ctx.WS.PostCreateHooks[repo]; ok {
				fmt.Fprintf(os.Stderr, "  Running post_create hook for %s...\n", ctx.WS.FormatRepoName(repo))
				if err := workspace.RunHook(wtPath, hook, os.Stderr, os.Stderr); err != nil {
					fmt.Fprintf(os.Stderr, "  %s post_create hook failed: %v\n", ui.Orange.Render("⚠"), err)
				}
			}

			if err := ctx.WS.Board(repo, capsule); err == nil {
				config.SaveBoarded(ctx.WS.Root, ctx.WS.Boarded)
				if err := ide.Regenerate(ctx.WS.Root, ctx.WS.Boarded, ctx.WS.DisplayNames, ctx.WS.Org); err != nil {
					fmt.Fprintf(os.Stderr, "  %s workspace files: %v\n", ui.Orange.Render("⚠"), err)
				}
			}

			fmt.Printf("cd %s\n", shellQuote(wtPath))
			return nil
		},
	}
}
