package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/spf13/cobra"
)

// addGroupFlag adds --group to a command that covers every repo by default.
func addGroupFlag(cmd *cobra.Command) {
	cmd.Flags().StringSlice("group", nil, "Only these groups, hidden or not (repeatable or comma-separated)")
	cmd.RegisterFlagCompletionFunc("group", completeGroupNames)
}

// applyGroupFlag narrows the workspace to the --group selection, if any.
// Commands without the flag are left alone.
func applyGroupFlag(cmd *cobra.Command, ws *workspace.Workspace) error {
	if cmd.Flags().Lookup("group") == nil {
		return nil
	}
	groups, err := cmd.Flags().GetStringSlice("group")
	if err != nil {
		return err
	}
	return ws.SelectGroups(groups)
}

func completeGroupNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx, err := LoadContext()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return ctx.WS.GroupNames(), cobra.ShellCompDirectiveNoFileComp
}

// ensureCloned clones a repo the first time a command needs its files.
// Repos in hidden groups are skipped by init and setup, so this is how they
// arrive. Progress goes to stderr, keeping stdout clean for jump and rg.
func ensureCloned(ws *workspace.Workspace, repo string) error {
	if _, err := os.Stat(ws.RepoDir(repo)); err == nil {
		return nil
	}
	if err := ws.EnsureReposDir(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s is not cloned yet, cloning it now.\n", ws.FormatRepoName(repo))
	cloned, err := cloneRepos(ws, []string{repo}, os.Stderr)
	if err != nil {
		return err
	}
	if len(cloned) == 0 {
		// SetupRepo creates the repo dir before cloning. Remove it if the
		// clone left it empty, or the next run would think it is cloned.
		os.Remove(ws.RepoDir(repo))
		return fmt.Errorf("could not clone %s", repo)
	}
	runAfterCreateHooks(ws, cloned, os.Stderr, os.Stderr)
	return nil
}

func newGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "group",
		Short: "List repo groups and choose which are hidden",
		Long: `Repos can belong to a group (group = "platform" in ws.toml). Hidden groups
are left out of commands that cover every repo, such as status, mc, rg and
debrief, unless you pass --group. Naming a hidden repo always works, and
it is cloned the first time a command needs it.

The team sets defaults in ws.toml under [groups.<name>]. show, hide and
reset change visibility for you only, in ws.local.toml.`,
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			return runGroupList(ctx)
		},
	}
	cmd.AddCommand(newGroupVisibilityCmd("show", "Show a group in commands that cover every repo, and clone its repos", false))
	cmd.AddCommand(newGroupVisibilityCmd("hide", "Hide a group from commands that cover every repo", true))
	cmd.AddCommand(newGroupResetCmd())
	return cmd
}

func runGroupList(ctx *Context) error {
	groups := ctx.WS.GroupNames()
	if len(groups) == 0 {
		fmt.Fprintln(os.Stderr, "No groups. Add group = \"<name>\" to repos in ws.toml.")
		return nil
	}
	width := 0
	for _, g := range groups {
		width = max(width, len(g))
	}
	for _, g := range groups {
		state := ui.Green.Render("shown ")
		if ctx.WS.HiddenGroups[g] {
			state = ui.Dim.Render("hidden")
		}
		repos := ctx.WS.ReposInGroup(g)
		list := ui.Dim.Render("(no repos)")
		if len(repos) > 0 {
			list = strings.Join(repos, ", ")
		}
		fmt.Fprintf(os.Stderr, "  %-*s  %s  %s\n", width, g, state, list)
	}
	return nil
}

func newGroupVisibilityCmd(use, short string, hidden bool) *cobra.Command {
	return &cobra.Command{
		Use:               use + " <group>",
		Short:             short,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeGroupArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			group, err := resolveGroup(ctx, args[0])
			if err != nil {
				return err
			}
			if err := config.SaveGroupVisibility(ctx.WS.Root, group, &hidden); err != nil {
				return fmt.Errorf("saving group visibility: %w", err)
			}
			if hidden {
				fmt.Fprintf(os.Stderr, "Hid %s.\n", group)
				return nil
			}
			fmt.Fprintf(os.Stderr, "Showing %s.\n", group)
			return cloneGroup(ctx.WS, group)
		},
	}
}

func newGroupResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "reset <group>",
		Short:             "Drop your visibility choice for a group and use the team default",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeGroupArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			group, err := resolveGroup(ctx, args[0])
			if err != nil {
				return err
			}
			if err := config.SaveGroupVisibility(ctx.WS.Root, group, nil); err != nil {
				return fmt.Errorf("saving group visibility: %w", err)
			}
			reloaded, err := LoadContextFromDir(ctx.WS.Root)
			if err != nil {
				return err
			}
			if reloaded.WS.HiddenGroups[group] {
				fmt.Fprintf(os.Stderr, "Reset %s to the team default: hidden.\n", group)
				return nil
			}
			fmt.Fprintf(os.Stderr, "Reset %s to the team default: shown.\n", group)
			return cloneGroup(reloaded.WS, group)
		},
	}
}

func completeGroupArg(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeGroupNames(cmd, args, toComplete)
}

func resolveGroup(ctx *Context, name string) (string, error) {
	if err := ctx.WS.SelectGroups([]string{name}); err != nil {
		return "", err
	}
	return name, nil
}

// cloneGroup clones any repos in the group that aren't cloned yet. A shown
// group behaves like the rest of the workspace, so its repos should be there.
func cloneGroup(ws *workspace.Workspace, group string) error {
	var missing []string
	for _, repo := range ws.ReposInGroup(group) {
		if _, err := os.Stat(ws.RepoDir(repo)); os.IsNotExist(err) {
			missing = append(missing, repo)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if err := ws.EnsureReposDir(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr)
	cloned, err := cloneRepos(ws, missing, os.Stderr)
	if err != nil {
		return err
	}
	runAfterCreateHooks(ws, cloned, os.Stderr, os.Stderr)
	return nil
}
