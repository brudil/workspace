package cli

import (
	"sort"

	"github.com/brudil/workspace/internal/workspace"
	"github.com/spf13/cobra"
)

// completeRepoNames returns repo names and aliases from ws.toml for shell completion.
func completeRepoNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx, err := LoadContext()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(ctx.WS.RepoNames)+len(ctx.WS.AliasMap))
	names = append(names, ctx.WS.RepoNames...)
	for alias := range ctx.WS.AliasMap {
		names = append(names, alias)
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeWorktreeNames returns a completion function that suggests worktree names
// for the repo specified at args[repoArgIndex].
func completeWorktreeNames(repoArgIndex int) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		ctx, err := LoadContext()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		if repoArgIndex >= len(args) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		repo := args[repoArgIndex]
		if canonical, ok := ctx.WS.ResolveAlias(repo); ok {
			repo = canonical
		}
		worktrees, err := workspace.ListAllWorktrees(ctx.WS.RepoDir(repo))
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return worktrees, cobra.ShellCompDirectiveNoFileComp
	}
}
