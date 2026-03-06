package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/ide"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/spf13/cobra"
)

func newUpgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "upgrade",
		Short:             "Pull latest config and set up new repos",
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}

			root := ctx.WS.Root
			configPath := filepath.Join(root, config.FileName)

			// Snapshot old config
			oldCfg, err := config.Parse(configPath)
			if err != nil {
				return err
			}

			// Enable workspace git, pull, then disable
			workspace.EnableWorkspaceGit(root)

			newCfg, pullErr := pullAndParse(root, configPath)
			if pullErr != nil {
				workspace.DisableWorkspaceGit(root)
				return pullErr
			}

			workspace.DisableWorkspaceGit(root)

			// Diff configs
			diff := config.Diff(oldCfg, newCfg)

			if diff.IsEmpty() {
				fmt.Fprintln(os.Stderr, "Already up to date.")
				return nil
			}

			// Print summary
			printDiffSummary(diff)

			// Clone new repos
			if len(diff.Added) > 0 {
				// Rebuild context with new config so repo list includes added repos
				newCtx, err := LoadContextFromDir(root)
				if err != nil {
					return err
				}

				if err := newCtx.WS.EnsureReposDir(); err != nil {
					return err
				}

				fmt.Fprintln(os.Stderr)
				clonedRepos, err := cloneRepos(newCtx.WS, diff.Added, os.Stderr)
				if err != nil {
					return err
				}
				runAfterCreateHooks(newCtx.WS, clonedRepos, os.Stderr, os.Stderr)
			}

			// Resync IDE workspace files in case config changed
			refreshCtx, err := LoadContextFromDir(root)
			if err == nil {
				if err := ide.Regenerate(refreshCtx.WS.Root, refreshCtx.WS.Boarded, refreshCtx.WS.DisplayNames, refreshCtx.WS.Org); err != nil {
					fmt.Fprintf(os.Stderr, "  %s workspace files: %v\n", ui.Orange.Render("⚠"), err)
				}
			}

			fmt.Fprintln(os.Stderr, "\nUpgrade complete.")
			return nil
		},
	}
}

// pullAndParse fetches the latest remote config and resets tracked files to match.
// Untracked files (like ws.local.toml) are preserved.
func pullAndParse(root, configPath string) (*config.Config, error) {
	if err := workspace.GitFetchOrigin(root); err != nil {
		return nil, fmt.Errorf("git fetch: %w", err)
	}
	if err := workspace.GitResetHard(root, "origin/HEAD"); err != nil {
		return nil, fmt.Errorf("git reset: %w", err)
	}

	cfg, err := config.Parse(configPath)
	if err != nil {
		return nil, fmt.Errorf("parsing updated config: %w", err)
	}

	return cfg, nil
}

func printDiffSummary(diff config.ConfigDiff) {
	if len(diff.Added) > 0 {
		fmt.Fprintf(os.Stderr, "  %s %d new %s (%s)\n",
			ui.Green.Render("+"),
			len(diff.Added),
			pluralize(len(diff.Added), "repo", "repos"),
			strings.Join(diff.Added, ", "))
	}
	if len(diff.Removed) > 0 {
		fmt.Fprintf(os.Stderr, "  %s %d removed %s (%s)\n",
			ui.Orange.Render("-"),
			len(diff.Removed),
			pluralize(len(diff.Removed), "repo", "repos"),
			strings.Join(diff.Removed, ", "))
		for _, name := range diff.Removed {
			fmt.Fprintf(os.Stderr, "    %s is now orphaned — remove when ready\n", name)
		}
	}
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
