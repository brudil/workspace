package cli

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var gitProtocol string

	cmd := &cobra.Command{
		Use:               "init <repo-url> [directory]",
		Short:             "Initialize a new workspace from a config repo",
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoURL := args[0]

			// Validate --git flag
			if gitProtocol != "" && gitProtocol != "ssh" && gitProtocol != "https" {
				return fmt.Errorf("invalid --git value %q: must be \"ssh\" or \"https\"", gitProtocol)
			}

			var dirName string
			if len(args) >= 2 {
				dirName = args[1]
			} else {
				dirName = dirNameFromURL(repoURL)
				if dirName == "" {
					return fmt.Errorf("could not derive directory name from URL; specify one explicitly")
				}
			}

			absDir, err := filepath.Abs(dirName)
			if err != nil {
				return err
			}

			if _, err := os.Stat(absDir); err == nil {
				return fmt.Errorf("directory %q already exists", absDir)
			}

			// Clone the workspace config repo
			fmt.Fprintf(os.Stderr, "Cloning workspace config into %s...\n", dirName)
			if err := workspace.GitClone(repoURL, absDir); err != nil {
				return fmt.Errorf("failed to clone config repo: %w", err)
			}

			// Write ws.local.toml with git protocol if specified
			if gitProtocol != "" {
				if err := writeLocalGitConfig(absDir, gitProtocol); err != nil {
					return fmt.Errorf("failed to write ws.local.toml: %w", err)
				}
			}

			// Load context from the cloned directory
			ctx, err := LoadContextFromDir(absDir)
			if err != nil {
				return fmt.Errorf("failed to load workspace config: %w", err)
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

			fmt.Fprintf(os.Stderr, "\nWorkspace ready. %d repos cloned.\n", len(clonedRepos))

			// Print the workspace path to stdout for shell integration: cd $(ws init ...)
			fmt.Printf("cd %s\n", shellQuote(absDir))
			return nil
		},
	}

	cmd.Flags().StringVar(&gitProtocol, "git", "", `git clone protocol: "ssh" or "https" (default https)`)
	return cmd
}

func writeLocalGitConfig(root, gitProtocol string) error {
	return config.UpdateLocal(root, func(local *config.LocalConfig) {
		local.Git = gitProtocol
	})
}

// dirNameFromURL derives a directory name from a git remote URL.
func dirNameFromURL(rawURL string) string {
	// Handle SCP-like SSH URLs: git@github.com:org/repo.git
	if strings.Contains(rawURL, ":") && !strings.Contains(rawURL, "://") {
		parts := strings.SplitN(rawURL, ":", 2)
		return stripDotGit(path.Base(parts[1]))
	}

	// Handle HTTPS URLs
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return stripDotGit(path.Base(u.Path))
}

func stripDotGit(name string) string {
	return strings.TrimSuffix(name, ".git")
}
