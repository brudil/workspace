package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/spf13/cobra"
)

func newJumpCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "jump [repo] [worktree]",
		Aliases: []string{"j"},
		Short:   "Output cd command for shell navigation",
		Long: `Outputs a cd command to stdout for use with shell eval.

Setup: add to your .zshrc / .bashrc:
  j() { eval "$(ws jump "$@")" }

Usage:
  j frontend          # jump to frontend repo (shows worktree picker)
  j frontend feature  # jump directly to worktree
  j fe                # aliases work too
  j ~                 # jump to workspace root
  j                   # interactive repo + worktree picker`,
		Args:          cobra.RangeArgs(0, 2),
		SilenceErrors: true,
		SilenceUsage:  true,
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

			var repoArg, worktreeArg string
			switch len(args) {
			case 2:
				repoArg = args[0]
				worktreeArg = args[1]
			case 1:
				repoArg = args[0]
			}

			path, err := resolveJumpPath(ctx, repoArg, worktreeArg)
			if err != nil {
				return err
			}

			if path == "" {
				return nil
			}

			fmt.Printf("cd %s\n", shellQuote(path))
			return nil
		},
	}
}

// resolveJumpPath determines the target directory for a jump command.
// It handles the ~ shortcut, alias resolution, and interactive pickers.
func resolveJumpPath(ctx *Context, repoArg, worktreeArg string) (string, error) {
	// ~ -> workspace root
	if repoArg == "~" {
		return ctx.WS.Root, nil
	}

	// Resolve repo using the shared resolution pipeline
	repo, err := ctx.ResolveRepo(repoArg)
	if err != nil {
		return "", err
	}

	// Resolve worktree
	worktree := worktreeArg
	if worktree != "" {
		// Check exact match first
		target := filepath.Join(ctx.WS.RepoDir(repo), worktree)
		if _, err := os.Stat(target); err == nil {
			return target, nil
		}
		// Fuzzy match against worktree names
		worktrees, err := workspace.ListAllWorktrees(ctx.WS.RepoDir(repo))
		if err != nil {
			return "", fmt.Errorf("listing worktrees for %s: %w", repo, err)
		}
		var matches []string
		for _, wt := range worktrees {
			if workspace.FuzzyMatch(worktree, wt) {
				matches = append(matches, wt)
			}
		}
		switch len(matches) {
		case 0:
			return "", fmt.Errorf("no worktree matching %q in %s", worktree, repo)
		case 1:
			worktree = matches[0]
		default:
			picked, err := ui.PickWorktree(matches)
			if err != nil {
				return "", nil
			}
			worktree = picked
		}
	} else {
		worktrees, err := workspace.ListAllWorktrees(ctx.WS.RepoDir(repo))
		if err != nil {
			return "", fmt.Errorf("listing worktrees for %s: %w", repo, err)
		}
		picked, err := ui.PickWorktree(worktrees)
		if err != nil {
			return "", nil
		}
		worktree = picked
	}

	// Verify target exists
	target := filepath.Join(ctx.WS.RepoDir(repo), worktree)
	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("worktree %s/%s does not exist", repo, worktree)
		}
		return "", fmt.Errorf("checking worktree %s/%s: %w", repo, worktree, err)
	}

	return target, nil
}
