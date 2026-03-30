package cli

import (
	"testing"
	"time"
)

func TestSiloWatchModel_TargetsChanged_UpdatesCapsule(t *testing.T) {
	m := siloWatchModel{
		repoNames:      []string{"repo-a", "repo-b"},
		formatRepoName: func(name string) string { return name },
		repos: []siloRepoView{
			{name: "repo-a", displayName: "repo-a", capsule: "capsule-old",
				history: []syncEntry{{fileCount: 5, time: time.Now()}}},
		},
	}

	msg := targetsChangedMsg(map[string]string{
		"repo-a": "capsule-new",
		"repo-b": "capsule-b",
	})

	updated, _ := m.Update(msg)
	m = updated.(siloWatchModel)

	if len(m.repos) != 2 {
		t.Fatalf("repos count = %d, want 2", len(m.repos))
	}
	if m.repos[0].capsule != "capsule-new" {
		t.Errorf("repo-a capsule = %q, want %q", m.repos[0].capsule, "capsule-new")
	}
	// History cleared when capsule changes
	if len(m.repos[0].history) != 0 {
		t.Errorf("repo-a history len = %d, want 0", len(m.repos[0].history))
	}
	if m.repos[1].name != "repo-b" {
		t.Errorf("repos[1].name = %q, want %q", m.repos[1].name, "repo-b")
	}
	if m.repos[1].capsule != "capsule-b" {
		t.Errorf("repos[1].capsule = %q, want %q", m.repos[1].capsule, "capsule-b")
	}
}

func TestSiloWatchModel_TargetsChanged_RepoRemoved(t *testing.T) {
	m := siloWatchModel{
		repoNames:      []string{"repo-a", "repo-b"},
		formatRepoName: func(name string) string { return name },
		repos: []siloRepoView{
			{name: "repo-a", displayName: "repo-a", capsule: "capsule-a"},
			{name: "repo-b", displayName: "repo-b", capsule: "capsule-b"},
		},
	}

	msg := targetsChangedMsg(map[string]string{
		"repo-a": "capsule-a",
	})

	updated, _ := m.Update(msg)
	m = updated.(siloWatchModel)

	if len(m.repos) != 1 {
		t.Fatalf("repos count = %d, want 1", len(m.repos))
	}
	if m.repos[0].name != "repo-a" {
		t.Errorf("repos[0].name = %q, want %q", m.repos[0].name, "repo-a")
	}
}

func TestSiloWatchModel_TargetsChanged_OrderFollowsRepoNames(t *testing.T) {
	m := siloWatchModel{
		repoNames:      []string{"aaa", "bbb", "ccc"},
		formatRepoName: func(name string) string { return name },
		repos:          []siloRepoView{},
	}

	// Add repos in reverse order — output should follow repoNames order
	msg := targetsChangedMsg(map[string]string{
		"ccc": "capsule-c",
		"aaa": "capsule-a",
	})

	updated, _ := m.Update(msg)
	m = updated.(siloWatchModel)

	if len(m.repos) != 2 {
		t.Fatalf("repos count = %d, want 2", len(m.repos))
	}
	if m.repos[0].name != "aaa" {
		t.Errorf("repos[0].name = %q, want %q", m.repos[0].name, "aaa")
	}
	if m.repos[1].name != "ccc" {
		t.Errorf("repos[1].name = %q, want %q", m.repos[1].name, "ccc")
	}
}
