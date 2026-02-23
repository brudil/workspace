package config

import "sort"

// ConfigDiff describes repos added or removed between two configs.
type ConfigDiff struct {
	Added   []string
	Removed []string
}

// Diff compares two configs and returns the repos that were added or removed.
func Diff(old, new *Config) ConfigDiff {
	var added, removed []string
	for name := range new.Repos {
		if _, ok := old.Repos[name]; !ok {
			added = append(added, name)
		}
	}
	for name := range old.Repos {
		if _, ok := new.Repos[name]; !ok {
			removed = append(removed, name)
		}
	}

	sort.Strings(added)
	sort.Strings(removed)

	return ConfigDiff{Added: added, Removed: removed}
}

// IsEmpty returns true if there are no changes.
func (d ConfigDiff) IsEmpty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0
}
