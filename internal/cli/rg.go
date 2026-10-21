package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/brudil/workspace/internal/workspace"
	"github.com/spf13/cobra"
)

// ExitCodeError asks main to exit with a specific code without printing
// anything. ws rg uses it to pass ripgrep's exit status through unchanged
// (0 matches, 1 no matches, 2 error).
type ExitCodeError struct {
	Code int
}

func (e *ExitCodeError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

// rgOptions are the ws-side flags of `ws rg`. Everything else goes to ripgrep.
type rgOptions struct {
	repos  []string // raw --repo selectors: "name" or "name/worktree"
	groups []string // --group names; their repos are added as selectors
	ground bool
	all    bool
	roots  bool
	help   bool
}

func newRgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rg [--repo <sel>]... [--group <name>]... [--ground|--all] [--roots] [--] <ripgrep args>",
		Short: "Run ripgrep across the workspace with sensible scope",
		Long: `Runs ripgrep over the right worktree of each repo, so every match comes
back once instead of once per capsule. Every ripgrep flag works unchanged.

Scope (default is "where I stand"):
  Inside a capsule or .ground, that worktree is searched for its repo and
  .ground is searched for every other repo. Outside any worktree, .ground
  everywhere. .bare and .silo are never searched unless named explicitly.
  Repos in hidden groups are skipped unless named, selected with --group, or
  the one you are standing in.

Scope flags (long form only; ripgrep owns the short flags):
  --repo <sel>    restrict to these repos; repeatable or comma-separated.
                  <sel> is a name, alias, or unambiguous fuzzy match, with
                  an optional /worktree (e.g. fe, fe/login-fix, fe/.ground).
  --group <name>  add every repo in these groups, hidden or not; repeatable
                  or comma-separated. Combines with --repo.
  --ground        search .ground for every selected repo
  --all           search .ground and every capsule (expect duplicates)
  --roots         print the directories that would be searched and exit

Output paths are absolute, so they say which repo and worktree each match
came from. Narrow with --repo and -g, not with paths: positional paths are
passed to ripgrep relative to the workspace root in addition to the roots.
Globs containing a slash are anchored to each root, as they would be in a
single-repo search. Ripgrep's exit code is passed through.

Examples:
  ws rg useAuth                        # my capsule for this repo, ground elsewhere
  ws rg --repo fe,api useAuth          # two repos by alias
  ws rg --repo fe/login-fix TODO       # one specific capsule
  ws rg --group platform Ingress       # a whole group, hidden or not
  ws rg --ground -t go 'func Load'     # baseline only
  ws rg --all FEATURE_FLAG_X           # every worktree
  ws rg -g 'src/**/*.ts' -l needle     # globs anchored per root
  ws rg --roots                        # show scope, run nothing`,
		DisableFlagParsing: true,
		SilenceUsage:       true,
		SilenceErrors:      true,
		ValidArgsFunction:  completeRgArgs,
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		err := runRg(cmd, args)
		var exit *ExitCodeError
		if err != nil && !errors.As(err, &exit) {
			fmt.Fprintf(os.Stderr, "ws rg: %v\n", err)
		}
		return err
	}
	return cmd
}

func runRg(cmd *cobra.Command, args []string) error {
	opts, rest, err := parseRgArgs(args)
	if err != nil {
		return err
	}
	if opts.help {
		return cmd.Help()
	}

	ctx, err := LoadContext()
	if err != nil {
		return err
	}

	selectors, err := resolveRgSelectors(ctx, opts.repos)
	if err != nil {
		return err
	}
	groupSelectors, err := resolveRgGroups(ctx, opts.groups)
	if err != nil {
		return err
	}
	selectors = append(selectors, groupSelectors...)

	mode := workspace.ScopeHere
	switch {
	case opts.ground:
		mode = workspace.ScopeGround
	case opts.all:
		mode = workspace.ScopeAll
	}

	cwd, err := userDir()
	if err != nil {
		return err
	}
	roots, err := ctx.WS.SearchRoots(cwd, selectors, mode)
	if err != nil {
		return err
	}
	if len(roots) == 0 {
		return fmt.Errorf("nothing to search: no worktrees found")
	}

	if opts.roots {
		var b strings.Builder
		for _, r := range roots {
			b.WriteString(r.Path)
			b.WriteByte('\n')
		}
		_, err := fmt.Fprint(os.Stdout, b.String())
		return err
	}

	if len(rest) == 0 {
		return fmt.Errorf("missing ripgrep pattern (use --roots to inspect scope)")
	}

	rgPath, err := exec.LookPath("rg")
	if err != nil {
		return fmt.Errorf("ripgrep (rg) not found on PATH; install it from https://github.com/BurntSushi/ripgrep")
	}

	prefixes := make([]string, len(roots))
	rgArgs := make([]string, 0, len(rest)+len(roots))
	for i, r := range roots {
		prefixes[i] = r.RelPath()
	}
	rgArgs = append(rgArgs, rewriteGlobs(rest, prefixes)...)
	for _, r := range roots {
		rgArgs = append(rgArgs, r.Path)
	}

	// Run from the workspace root: ripgrep matches slash-containing globs
	// against paths relative to its cwd, so the rewritten globs only line up
	// from there. Roots are absolute so output paths are absolute.
	c := exec.Command(rgPath, rgArgs...)
	c.Dir = ctx.WS.Root
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return &ExitCodeError{Code: exitErr.ExitCode()}
		}
		return fmt.Errorf("running rg: %w", err)
	}
	return nil
}

// parseRgArgs consumes ws flags from the front of args. Parsing stops at the
// first token that is not a ws flag, or at "--" (which is dropped). Everything
// remaining is returned for ripgrep untouched.
func parseRgArgs(args []string) (rgOptions, []string, error) {
	var opts rgOptions
	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--":
			i++
			return opts, args[i:], nil
		case arg == "--repo":
			if i+1 >= len(args) {
				return opts, nil, fmt.Errorf("--repo requires a value")
			}
			opts.repos = append(opts.repos, splitRepoList(args[i+1])...)
			i += 2
		case strings.HasPrefix(arg, "--repo="):
			opts.repos = append(opts.repos, splitRepoList(strings.TrimPrefix(arg, "--repo="))...)
			i++
		case arg == "--group":
			if i+1 >= len(args) {
				return opts, nil, fmt.Errorf("--group requires a value")
			}
			opts.groups = append(opts.groups, splitRepoList(args[i+1])...)
			i += 2
		case strings.HasPrefix(arg, "--group="):
			opts.groups = append(opts.groups, splitRepoList(strings.TrimPrefix(arg, "--group="))...)
			i++
		case arg == "--ground":
			opts.ground = true
			i++
		case arg == "--all":
			opts.all = true
			i++
		case arg == "--roots":
			opts.roots = true
			i++
		case arg == "--help" || arg == "-h":
			opts.help = true
			i++
		default:
			if opts.ground && opts.all {
				return opts, nil, fmt.Errorf("--ground and --all are mutually exclusive")
			}
			return opts, args[i:], nil
		}
	}
	if opts.ground && opts.all {
		return opts, nil, fmt.Errorf("--ground and --all are mutually exclusive")
	}
	return opts, nil, nil
}

func splitRepoList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// resolveRgSelectors turns raw --repo values into resolved selectors. A value
// is "name" or "name/worktree"; both halves resolve strictly (no pickers).
func resolveRgSelectors(ctx *Context, raw []string) ([]workspace.RepoSelector, error) {
	selectors := make([]workspace.RepoSelector, 0, len(raw))
	for _, v := range raw {
		repoArg, wtArg, hasWT := strings.Cut(v, "/")
		repo, err := ctx.ResolveRepoStrict(repoArg)
		if err != nil {
			return nil, err
		}
		if err := ensureCloned(ctx.WS, repo); err != nil {
			return nil, err
		}
		sel := workspace.RepoSelector{Repo: repo}
		if hasWT {
			wt, err := ctx.ResolveWorktreeStrict(repo, wtArg)
			if err != nil {
				return nil, err
			}
			sel.Worktree = wt
		}
		selectors = append(selectors, sel)
	}
	return selectors, nil
}

// resolveRgGroups turns --group names into one selector per repo in those
// groups, cloning any that are missing: naming a group asks for its repos.
func resolveRgGroups(ctx *Context, groups []string) ([]workspace.RepoSelector, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	if err := ctx.WS.SelectGroups(groups); err != nil {
		return nil, err
	}
	var selectors []workspace.RepoSelector
	for _, repo := range ctx.WS.DefaultRepoNames() {
		if err := ensureCloned(ctx.WS, repo); err != nil {
			return nil, err
		}
		selectors = append(selectors, workspace.RepoSelector{Repo: repo})
	}
	if len(selectors) == 0 {
		return nil, fmt.Errorf("no repos in group %s", strings.Join(groups, ", "))
	}
	return selectors, nil
}

// rewriteGlobs expands ripgrep -g/--glob/--iglob values that contain a slash
// into one glob per search root, prefixed with the root's workspace-relative
// path. Ripgrep matches such globs against the printed path relative to its
// cwd, not relative to each root, so without this "-g 'src/*.ts'" would match
// nothing. Basename globs (no slash) are left alone. Rewriting stops at "--".
func rewriteGlobs(args []string, prefixes []string) []string {
	out := make([]string, 0, len(args))
	emit := func(flag, glob string) {
		for _, g := range expandGlob(glob, prefixes) {
			if flag == "" {
				out = append(out, g)
			} else {
				out = append(out, flag, g)
			}
		}
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			out = append(out, args[i:]...)
			return out
		case arg == "-g" || arg == "--glob" || arg == "--iglob":
			if i+1 < len(args) {
				emit(arg, args[i+1])
				i++
			} else {
				out = append(out, arg)
			}
		case strings.HasPrefix(arg, "--glob=") || strings.HasPrefix(arg, "--iglob="):
			flag, glob, _ := strings.Cut(arg, "=")
			for _, g := range expandGlob(glob, prefixes) {
				out = append(out, flag+"="+g)
			}
		case strings.HasPrefix(arg, "-g") && len(arg) > 2 && !strings.HasPrefix(arg, "--"):
			for _, g := range expandGlob(arg[2:], prefixes) {
				out = append(out, "-g"+g)
			}
		default:
			out = append(out, arg)
		}
	}
	return out
}

func expandGlob(glob string, prefixes []string) []string {
	neg := ""
	body := glob
	if strings.HasPrefix(body, "!") {
		neg, body = "!", body[1:]
	}
	if !strings.Contains(body, "/") {
		return []string{glob}
	}
	body = strings.TrimPrefix(body, "/")
	out := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, neg+p+"/"+body)
	}
	return out
}

// completeRgArgs completes --repo values (names, aliases, repo/worktree) and
// the ws flags themselves. Ripgrep's own flags are not completed.
func completeRgArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	wsFlags := []string{"--repo", "--group", "--ground", "--all", "--roots"}

	if (len(args) > 0 && args[len(args)-1] == "--group") || strings.HasPrefix(toComplete, "--group=") {
		groups, _ := completeGroupNames(cmd, args, toComplete)
		if strings.HasPrefix(toComplete, "--group=") {
			for i := range groups {
				groups[i] = "--group=" + groups[i]
			}
		}
		return groups, cobra.ShellCompDirectiveNoFileComp
	}

	prevIsRepo := len(args) > 0 && args[len(args)-1] == "--repo"
	value := toComplete
	prefix := ""
	if strings.HasPrefix(toComplete, "--repo=") {
		prevIsRepo = true
		prefix = "--repo="
		value = strings.TrimPrefix(toComplete, "--repo=")
	}
	if !prevIsRepo {
		if strings.HasPrefix(toComplete, "-") {
			return wsFlags, cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveDefault
	}

	ctx, err := LoadContext()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	if repoArg, _, hasSlash := strings.Cut(value, "/"); hasSlash {
		repo, err := ctx.ResolveRepoStrict(repoArg)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		wts, err := workspace.ListAllWorktrees(ctx.WS.RepoDir(repo))
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		for _, wt := range wts {
			out = append(out, prefix+repoArg+"/"+wt)
		}
		return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	}
	names, _ := completeRepoNames(cmd, args, value)
	for _, n := range names {
		out = append(out, prefix+n)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
