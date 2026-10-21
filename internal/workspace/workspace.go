package workspace

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Workspace represents a resolved workspace with config-derived state.
// This is the core type — all business logic operates on this.
type Workspace struct {
	Root             string
	Org              string
	DefaultBranch    string
	GitProtocol      string              // "ssh" or "" (defaults to https)
	Name             string              // optional display name for the workspace
	RepoNames        []string            // sorted canonical names, every repo including hidden ones
	RepoGroups       map[string]string   // canonical name → group (ungrouped repos are absent)
	HiddenGroups     map[string]bool     // groups left out of commands that cover every repo
	AliasMap         map[string]string   // alias → canonical name
	DisplayNames     map[string]string   // canonical name → display name
	RepoColors       map[string]string   // canonical name → custom color (256-color or hex)
	AfterCreateHooks map[string]string   // canonical name → shell command
	Boarded          map[string][]string // repo → boarded capsule names (from ws.local.toml)
	Silo             map[string]string   // repo → capsule name the silo points at (from ws.local.toml)

	selectedGroups map[string]bool // set by SelectGroups; nil means "every visible group"
}

func (w *Workspace) ReposDir() string {
	return filepath.Join(w.Root, "repos")
}

func (w *Workspace) RepoDir(name string) string {
	return filepath.Join(w.ReposDir(), name)
}

const GroundDir = ".ground"
const SiloDir = ".silo"

func (w *Workspace) MainWorktree(name string) string {
	return filepath.Join(w.RepoDir(name), GroundDir)
}

func (w *Workspace) SiloWorktree(name string) string {
	return filepath.Join(w.RepoDir(name), SiloDir)
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

// GroupOf returns the repo's group, or "" if it has none.
func (w *Workspace) GroupOf(repo string) string {
	return w.RepoGroups[repo]
}

// IsHidden reports whether the repo belongs to a hidden group.
func (w *Workspace) IsHidden(repo string) bool {
	return w.HiddenGroups[w.RepoGroups[repo]]
}

// GroupNames returns every group that has a repo or a visibility setting, sorted.
func (w *Workspace) GroupNames() []string {
	seen := make(map[string]bool)
	for _, g := range w.RepoGroups {
		seen[g] = true
	}
	for g := range w.HiddenGroups {
		seen[g] = true
	}
	return slices.Sorted(maps.Keys(seen))
}

// ReposInGroup returns the repos in a group, sorted, whether or not it is hidden.
func (w *Workspace) ReposInGroup(group string) []string {
	var repos []string
	for _, name := range w.RepoNames {
		if w.RepoGroups[name] == group {
			repos = append(repos, name)
		}
	}
	return repos
}

// SelectGroups narrows DefaultRepoNames to the named groups, hidden or not.
// It backs the --group flag. Unknown group names are an error.
func (w *Workspace) SelectGroups(groups []string) error {
	if len(groups) == 0 {
		return nil
	}
	known := w.GroupNames()
	w.selectedGroups = make(map[string]bool, len(groups))
	for _, g := range groups {
		if !slices.Contains(known, g) {
			if len(known) == 0 {
				return fmt.Errorf("unknown group %q (no groups in ws.toml)", g)
			}
			return fmt.Errorf("unknown group %q (groups: %s)", g, strings.Join(known, ", "))
		}
		w.selectedGroups[g] = true
	}
	return nil
}

// DefaultRepoNames returns the repos a command covers when none is named:
// every repo outside a hidden group, or exactly the --group selection.
func (w *Workspace) DefaultRepoNames() []string {
	var repos []string
	for _, name := range w.RepoNames {
		if w.selectedGroups != nil {
			if w.selectedGroups[w.RepoGroups[name]] {
				repos = append(repos, name)
			}
		} else if !w.IsHidden(name) {
			repos = append(repos, name)
		}
	}
	return repos
}

// ListedRepoNames is DefaultRepoNames plus any hidden repo that has a
// capsule. A capsule means you're working there, so status and cleanup keep
// showing it until it is debriefed. A --group selection is taken as is.
func (w *Workspace) ListedRepoNames() []string {
	if w.selectedGroups != nil {
		return w.DefaultRepoNames()
	}
	var repos []string
	for _, name := range w.RepoNames {
		if !w.IsHidden(name) || w.HasCapsules(name) {
			repos = append(repos, name)
		}
	}
	return repos
}

// HasCapsules reports whether the repo has any worktree besides .ground.
func (w *Workspace) HasCapsules(repo string) bool {
	capsules, err := ListWorktrees(w.RepoDir(repo))
	return err == nil && len(capsules) > 0
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

// FuzzyMatchPositions returns the indices in target where each pattern character
// matched (case-insensitive). Returns nil if no match.
func FuzzyMatchPositions(pattern, target string) []int {
	if pattern == "" {
		return nil
	}
	patternLower := []rune(strings.ToLower(pattern))
	targetLower := []rune(strings.ToLower(target))
	positions := make([]int, 0, len(patternLower))
	pi := 0
	for i, r := range targetLower {
		if r == patternLower[pi] {
			positions = append(positions, i)
			pi++
			if pi == len(patternLower) {
				return positions
			}
		}
	}
	return nil
}

// FuzzyMatchRepos returns canonical repo names that fuzzy-match the input.
// Matches against both canonical names and aliases, deduplicating results.
// Repos in hidden groups are only considered when no visible repo matches,
// so a hidden repo never makes a short pattern ambiguous.
func (w *Workspace) FuzzyMatchRepos(input string) []string {
	if input == "" {
		return nil
	}
	if matches := w.fuzzyMatchRepos(input, false); len(matches) > 0 {
		return matches
	}
	return w.fuzzyMatchRepos(input, true)
}

func (w *Workspace) fuzzyMatchRepos(input string, hidden bool) []string {
	seen := make(map[string]bool)
	var matches []string

	for _, name := range w.RepoNames {
		if w.IsHidden(name) == hidden && FuzzyMatch(input, name) && !seen[name] {
			seen[name] = true
			matches = append(matches, name)
		}
	}
	for alias, canonical := range w.AliasMap {
		if w.IsHidden(canonical) == hidden && FuzzyMatch(input, alias) && !seen[canonical] {
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
