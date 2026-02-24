package cli

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/brudil/workspace/internal/github"
	"github.com/spf13/cobra"
)

var prURLRe = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/pull/(\d+)`)

// parsePRURL extracts org, repo, and PR number from a GitHub PR URL.
func parsePRURL(rawURL string) (org, repo string, number int, ok bool) {
	m := prURLRe.FindStringSubmatch(rawURL)
	if m == nil {
		return "", "", 0, false
	}
	n, err := strconv.Atoi(m[3])
	if err != nil || n <= 0 {
		return "", "", 0, false
	}
	return m[1], m[2], n, true
}

// isPRNumber returns the number if s is a positive integer (i.e. a PR number).
func isPRNumber(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// resolveFromPR fetches a PR by number and returns the head branch name.
func resolveFromPR(gh github.Client, org, repo string, number int) (string, error) {
	pr, err := gh.PRFromNumber(org, repo, number)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stderr, "  PR #%d: %s\n", pr.Number, pr.Title)
	return pr.HeadRefName, nil
}

func newDockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dock <repo> <branch | PR# | PR-URL>",
		Short: "Check out an existing capsule",
		Long: `Create a worktree for an existing remote branch. Use "." as the repo to
infer from the current directory.

Examples:
  ws dock frontend feature-branch
  ws dock . feature-branch
  ws dock frontend 1234
  ws dock https://github.com/org/repo/pull/1234`,
		Args: cobra.RangeArgs(1, 2),
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

			var repo, branch string

			if len(args) == 2 {
				repo, err = ctx.ResolveRepo(args[0])
				if err != nil {
					return err
				}

				if prNum, ok := isPRNumber(args[1]); ok {
					branch, err = resolveFromPR(ctx.GitHub, ctx.WS.Org, repo, prNum)
					if err != nil {
						return err
					}
				} else {
					branch = args[1]
				}
			} else {
				arg := args[0]

				if urlOrg, urlRepo, prNum, ok := parsePRURL(arg); ok {
					if urlOrg != ctx.WS.Org {
						return fmt.Errorf("PR URL org %q does not match workspace org %q", urlOrg, ctx.WS.Org)
					}
					canonical, found := ctx.WS.ResolveAlias(urlRepo)
					if !found {
						return fmt.Errorf("repo %q (from PR URL) is not in this workspace", urlRepo)
					}
					repo = canonical
					branch, err = resolveFromPR(ctx.GitHub, urlOrg, urlRepo, prNum)
					if err != nil {
						return err
					}
				} else {
					return fmt.Errorf("usage: ws dock <repo> <branch | PR# | PR-URL>")
				}
			}

			return runCapsuleCreate(ctx, repo, func() (string, error) {
				return ctx.WS.CreateDockWorktree(repo, branch)
			}, "Docked!")
		},
	}
}
