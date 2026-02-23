package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/brudil/workspace/internal/config"
)

func TestDirNameFromURL_HTTPS(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/org/my-workspace.git", "my-workspace"},
		{"https://github.com/org/my-workspace", "my-workspace"},
		{"https://github.com/org/ws.git", "ws"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := dirNameFromURL(tt.url); got != tt.want {
				t.Errorf("dirNameFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestDirNameFromURL_SSH(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"git@github.com:org/my-workspace.git", "my-workspace"},
		{"git@github.com:org/my-workspace", "my-workspace"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := dirNameFromURL(tt.url); got != tt.want {
				t.Errorf("dirNameFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestStripDotGit(t *testing.T) {
	if got := stripDotGit("repo.git"); got != "repo" {
		t.Errorf("stripDotGit(%q) = %q, want %q", "repo.git", got, "repo")
	}
	if got := stripDotGit("repo"); got != "repo" {
		t.Errorf("stripDotGit(%q) = %q, want %q", "repo", got, "repo")
	}
}

func TestWriteLocalGitConfig_CreatesFile(t *testing.T) {
	root := t.TempDir()
	if err := writeLocalGitConfig(root, "ssh"); err != nil {
		t.Fatalf("writeLocalGitConfig() error: %v", err)
	}

	var local config.LocalConfig
	_, err := toml.DecodeFile(filepath.Join(root, "ws.local.toml"), &local)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if local.Git != "ssh" {
		t.Errorf("git = %q, want %q", local.Git, "ssh")
	}
}

func TestWriteLocalGitConfig_PreservesExisting(t *testing.T) {
	root := t.TempDir()
	existing := `[repos.repo-a]
color = "#ff0000"

[boarded]
repo-a = [".ground"]
`
	os.WriteFile(filepath.Join(root, "ws.local.toml"), []byte(existing), 0644)

	if err := writeLocalGitConfig(root, "ssh"); err != nil {
		t.Fatalf("writeLocalGitConfig() error: %v", err)
	}

	var local config.LocalConfig
	_, err := toml.DecodeFile(filepath.Join(root, "ws.local.toml"), &local)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if local.Git != "ssh" {
		t.Errorf("git = %q, want %q", local.Git, "ssh")
	}
	if local.Repos["repo-a"].Color != "#ff0000" {
		t.Errorf("repo color lost: got %q", local.Repos["repo-a"].Color)
	}
	if len(local.Boarded["repo-a"]) != 1 || local.Boarded["repo-a"][0] != ".ground" {
		t.Errorf("boarded lost: got %v", local.Boarded["repo-a"])
	}
}
