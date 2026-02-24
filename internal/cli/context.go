package cli

import (
	"fmt"
	"os"
	"sort"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
)

// Context bridges config and the core workspace for CLI commands.
type Context struct {
	Config *config.Config
	WS     *workspace.Workspace
	GitHub github.Client
}

var ctxOverride *Context

func SetContextOverride(ctx *Context) {
	ctxOverride = ctx
}

func ClearContextOverride() {
	ctxOverride = nil
}

// LoadContext discovers config from the current working directory and builds a workspace.
func LoadContext() (*Context, error) {
	if ctxOverride != nil {
		return ctxOverride, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return LoadContextFromDir(cwd)
}

// LoadContextFromDir discovers config from the given directory and builds a workspace.
func LoadContextFromDir(dir string) (*Context, error) {
	cfg, root, err := config.Load(dir)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(cfg.Repos))
	for name := range cfg.Repos {
		names = append(names, name)
	}
	sort.Strings(names)

	// Build alias map, display names, and custom colors
	aliasMap := make(map[string]string)
	displayNames := make(map[string]string)
	repoColors := make(map[string]string)
	afterCreateHooks := make(map[string]string)

	for name, rc := range cfg.Repos {
		if rc.DisplayName != "" {
			displayNames[name] = rc.DisplayName
		}
		if rc.Color != "" {
			repoColors[name] = rc.Color
		}
		if rc.AfterCreate != "" {
			afterCreateHooks[name] = rc.AfterCreate
		}
		for _, alias := range rc.Aliases {
			if _, isCanonical := cfg.Repos[alias]; isCanonical {
				return nil, fmt.Errorf("alias %q for repo %q collides with a canonical repo name", alias, name)
			}
			if existing, ok := aliasMap[alias]; ok {
				return nil, fmt.Errorf("alias %q is used by both %q and %q", alias, existing, name)
			}
			aliasMap[alias] = name
		}
	}

	ws := &workspace.Workspace{
		Root:             root,
		Org:              cfg.Workspace.Org,
		DefaultBranch:    cfg.Workspace.DefaultBranch,
		GitProtocol:      cfg.Git,
		Name:             cfg.Workspace.DisplayName,
		RepoNames:        names,
		AliasMap:         aliasMap,
		DisplayNames:     displayNames,
		RepoColors:       repoColors,
		AfterCreateHooks: afterCreateHooks,
		Boarded:          cfg.Boarded,
	}

	return &Context{Config: cfg, WS: ws, GitHub: github.LiveClient{}}, nil
}

// ResolveRepo figures out which repo the user means.
// "." means infer from cwd (error if not in a repo).
// "" means try cwd, fall back to interactive picker.
// Anything else is a repo name lookup.
func (c *Context) ResolveRepo(arg string) (string, error) {
	if arg != "" && arg != "." {
		// Exact name or alias match
		if canonical, ok := c.WS.ResolveAlias(arg); ok {
			return canonical, nil
		}
		// Fuzzy match
		matches := c.WS.FuzzyMatchRepos(arg)
		switch len(matches) {
		case 1:
			return matches[0], nil
		case 0:
			return "", fmt.Errorf("unknown repo %q (not in ws.toml)", arg)
		default:
			return ui.PickRepo(matches, c.WS.DisplayNames)
		}
	}

	cwd, _ := os.Getwd()
	if repo, _, ok := workspace.DetectRepo(c.WS.Root, cwd); ok {
		if _, exists := c.Config.Repos[repo]; exists {
			return repo, nil
		}
	}

	if arg == "." {
		return "", fmt.Errorf("not inside a known repo directory")
	}

	return ui.PickRepo(c.WS.RepoNames, c.WS.DisplayNames)
}

// ResolveCapsule figures out which capsule the user means within a repo.
// Does exact match, then fuzzy match. Single fuzzy match is used directly;
// multiple matches prompt an interactive picker; zero matches return an error.
func (c *Context) ResolveCapsule(repo, arg string) (string, error) {
	capsules, err := workspace.ListWorktrees(c.WS.RepoDir(repo))
	if err != nil {
		return "", fmt.Errorf("listing capsules for %s: %w", repo, err)
	}

	// Exact match
	for _, name := range capsules {
		if name == arg {
			return name, nil
		}
	}

	// Fuzzy match
	var matches []string
	for _, name := range capsules {
		if workspace.FuzzyMatch(arg, name) {
			matches = append(matches, name)
		}
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no capsule matching %q in %s", arg, repo)
	default:
		return ui.PickWorktree(matches)
	}
}
