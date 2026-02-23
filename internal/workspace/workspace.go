package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Workspace represents a resolved workspace with config-derived state.
// This is the core type — all business logic operates on this.
type Workspace struct {
	Root            string
	Org             string
	DefaultBranch   string
	GitProtocol     string              // "ssh" or "" (defaults to https)
	Name            string              // optional display name for the workspace
	RepoNames       []string            // sorted canonical names
	AliasMap        map[string]string   // alias → canonical name
	DisplayNames    map[string]string   // canonical name → display name
	RepoColors      map[string]string   // canonical name → custom color (256-color or hex)
	PostCreateHooks map[string]string   // canonical name → shell command
	Boarded         map[string][]string // repo → boarded capsule names (from ws.local.toml)
}

func (w *Workspace) ReposDir() string {
	return filepath.Join(w.Root, "repos")
}

func (w *Workspace) RepoDir(name string) string {
	return filepath.Join(w.ReposDir(), name)
}

const GroundDir = ".ground"

func (w *Workspace) MainWorktree(name string) string {
	return filepath.Join(w.RepoDir(name), GroundDir)
}

func (w *Workspace) BareDir(name string) string {
	return filepath.Join(w.RepoDir(name), ".bare")
}

// ResolveAlias returns the canonical repo name for an alias or canonical name.
func (w *Workspace) ResolveAlias(input string) (string, bool) {
	for _, name := range w.RepoNames {
		if name == input {
			return name, true
		}
	}
	if canonical, ok := w.AliasMap[input]; ok {
		return canonical, true
	}
	return "", false
}

// FuzzyMatch returns true if every character in pattern appears in target
// in order (case-insensitive). Like fzf's subsequence matching.
func FuzzyMatch(pattern, target string) bool {
	pattern = strings.ToLower(pattern)
	target = strings.ToLower(target)
	patternRunes := []rune(pattern)
	if len(patternRunes) == 0 {
		return true
	}
	pi := 0
	for _, r := range target {
		if r == patternRunes[pi] {
			pi++
			if pi == len(patternRunes) {
				return true
			}
		}
	}
	return false
}

// FuzzyMatchRepos returns canonical repo names that fuzzy-match the input.
// Matches against both canonical names and aliases, deduplicating results.
func (w *Workspace) FuzzyMatchRepos(input string) []string {
	if input == "" {
		return nil
	}
	seen := make(map[string]bool)
	var matches []string

	for _, name := range w.RepoNames {
		if FuzzyMatch(input, name) && !seen[name] {
			seen[name] = true
			matches = append(matches, name)
		}
	}
	for alias, canonical := range w.AliasMap {
		if FuzzyMatch(input, alias) && !seen[canonical] {
			seen[canonical] = true
			matches = append(matches, canonical)
		}
	}
	return matches
}

// Title returns the workspace display name if set, otherwise the org.
func (w *Workspace) Title() string {
	if w.Name != "" {
		return w.Name
	}
	return w.Org
}

// DisplayNameFor returns the display name if set, otherwise the canonical name.
func (w *Workspace) DisplayNameFor(name string) string {
	if dn, ok := w.DisplayNames[name]; ok {
		return dn
	}
	return name
}

// IsBoarded returns true if the given capsule is boarded for the repo.
func (w *Workspace) IsBoarded(repo, capsule string) bool {
	return slices.Contains(w.Boarded[repo], capsule)
}

// CapsuleName returns the directory name for a capsule from a branch name.
// Strips everything before the last slash to avoid creating subdirectories.
func CapsuleName(branch string) string {
	if i := strings.LastIndex(branch, "/"); i >= 0 {
		return branch[i+1:]
	}
	return branch
}

// UniqueCapsuleName returns a capsule name that doesn't collide with
// existing directories in repoDir.
func UniqueCapsuleName(repoDir, branch string) string {
	base := CapsuleName(branch)
	candidate := base
	for n := 2; ; n++ {
		if _, err := os.Stat(filepath.Join(repoDir, candidate)); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, n)
	}
}

// FormatRepoName returns "Display Name (canonical)" if a display name is set,
// otherwise just the canonical name.
func (w *Workspace) FormatRepoName(name string) string {
	if dn, ok := w.DisplayNames[name]; ok {
		return fmt.Sprintf("%s (%s)", dn, name)
	}
	return name
}
