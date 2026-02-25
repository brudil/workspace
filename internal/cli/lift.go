package cli

import (
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

			return runCapsuleCreate(ctx, repo, branch, func() (string, error) {
				return ctx.WS.CreateLiftWorktree(repo, branch, base)
			}, "Lift off!")
		},
	}
}
