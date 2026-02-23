package config

import (
	"testing"
)

func TestDiff_AddedRepos(t *testing.T) {
	old := &Config{Repos: map[string]RepoConfig{"a": {}}}
	new := &Config{Repos: map[string]RepoConfig{"a": {}, "b": {}, "c": {}}}

	d := Diff(old, new)

	if len(d.Added) != 2 {
		t.Fatalf("added = %d, want 2", len(d.Added))
	}
	if d.Added[0] != "b" || d.Added[1] != "c" {
		t.Errorf("added = %v, want [b c]", d.Added)
	}
}

func TestDiff_RemovedRepos(t *testing.T) {
	old := &Config{Repos: map[string]RepoConfig{"a": {}, "b": {}, "c": {}}}
	new := &Config{Repos: map[string]RepoConfig{"a": {}}}

	d := Diff(old, new)

	if len(d.Removed) != 2 {
		t.Fatalf("removed = %d, want 2", len(d.Removed))
	}
	if d.Removed[0] != "b" || d.Removed[1] != "c" {
		t.Errorf("removed = %v, want [b c]", d.Removed)
	}
}

func TestDiff_NoChanges(t *testing.T) {
	old := &Config{Repos: map[string]RepoConfig{"a": {}, "b": {}}}
	new := &Config{Repos: map[string]RepoConfig{"a": {}, "b": {}}}

	d := Diff(old, new)

	if !d.IsEmpty() {
		t.Errorf("expected empty diff, got added=%v removed=%v", d.Added, d.Removed)
	}
}

func TestDiff_BothAddedAndRemoved(t *testing.T) {
	old := &Config{Repos: map[string]RepoConfig{"a": {}, "b": {}}}
	new := &Config{Repos: map[string]RepoConfig{"b": {}, "c": {}}}

	d := Diff(old, new)

	if len(d.Added) != 1 || d.Added[0] != "c" {
		t.Errorf("added = %v, want [c]", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0] != "a" {
		t.Errorf("removed = %v, want [a]", d.Removed)
	}
}

func TestIsEmpty_Empty(t *testing.T) {
	d := ConfigDiff{}
	if !d.IsEmpty() {
		t.Error("expected empty")
	}
}

func TestIsEmpty_NotEmpty(t *testing.T) {
	d := ConfigDiff{Added: []string{"x"}}
	if d.IsEmpty() {
		t.Error("expected not empty")
	}
}
