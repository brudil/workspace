package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestParse(t *testing.T) {
	content := `
[workspace]
org = "my-org"
default_branch = "main"

[repos]
repo-a = {}
repo-b = {}
`
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ws.toml")
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Workspace.Org != "my-org" {
		t.Errorf("org = %q, want %q", cfg.Workspace.Org, "my-org")
	}
	if cfg.Workspace.DefaultBranch != "main" {
		t.Errorf("default_branch = %q, want %q", cfg.Workspace.DefaultBranch, "main")
	}
	if len(cfg.Repos) != 2 {
		t.Fatalf("repos count = %d, want 2", len(cfg.Repos))
	}
	if _, ok := cfg.Repos["repo-a"]; !ok {
		t.Error("missing repo-a")
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte("[workspace]\norg=\"x\"\ndefault_branch=\"main\"\n[repos]\n"), 0644)
	nested := filepath.Join(root, "repos", "my-repo", "main", "src")
	os.MkdirAll(nested, 0755)

	found, err := Discover(nested)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != root {
		t.Errorf("discovered %q, want %q", found, root)
	}
}

func TestParseDisplayNameAndAliases(t *testing.T) {
	content := `
[workspace]
org = "my-org"
default_branch = "main"

[repos]
backend = { display_name = "API Server", aliases = ["api", "be"] }
simple-repo = {}
`
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ws.toml")
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	repo := cfg.Repos["backend"]
	if repo.DisplayName != "API Server" {
		t.Errorf("display_name = %q, want %q", repo.DisplayName, "API Server")
	}
	if len(repo.Aliases) != 2 || repo.Aliases[0] != "api" || repo.Aliases[1] != "be" {
		t.Errorf("aliases = %v, want [api be]", repo.Aliases)
	}

	simple := cfg.Repos["simple-repo"]
	if simple.DisplayName != "" {
		t.Errorf("simple-repo display_name = %q, want empty", simple.DisplayName)
	}
	if len(simple.Aliases) != 0 {
		t.Errorf("simple-repo aliases = %v, want empty", simple.Aliases)
	}
}

func TestParseWorkspaceDisplayName(t *testing.T) {
	content := `
[workspace]
org = "my-org"
default_branch = "main"
display_name = "My Workspace"

[repos]
repo-a = {}
`
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ws.toml")
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Workspace.DisplayName != "My Workspace" {
		t.Errorf("display_name = %q, want %q", cfg.Workspace.DisplayName, "My Workspace")
	}
}

func TestParseWorkspaceDisplayNameOptional(t *testing.T) {
	content := `
[workspace]
org = "my-org"
default_branch = "main"

[repos]
repo-a = {}
`
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ws.toml")
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Workspace.DisplayName != "" {
		t.Errorf("display_name = %q, want empty", cfg.Workspace.DisplayName)
	}
}

func TestParseColor(t *testing.T) {
	content := `
[workspace]
org = "my-org"
default_branch = "main"

[repos]
repo-a = { color = "#ff5f87" }
repo-b = { color = "168" }
repo-c = {}
`
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ws.toml")
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Repos["repo-a"].Color != "#ff5f87" {
		t.Errorf("repo-a color = %q, want %q", cfg.Repos["repo-a"].Color, "#ff5f87")
	}
	if cfg.Repos["repo-b"].Color != "168" {
		t.Errorf("repo-b color = %q, want %q", cfg.Repos["repo-b"].Color, "168")
	}
	if cfg.Repos["repo-c"].Color != "" {
		t.Errorf("repo-c color = %q, want empty", cfg.Repos["repo-c"].Color)
	}
}

func TestDiscoverNotFound(t *testing.T) {
	tmp := t.TempDir()
	_, err := Discover(tmp)
	if err == nil {
		t.Error("expected error when ws.toml not found")
	}
}

func TestLoad(t *testing.T) {
	root := t.TempDir()
	content := `[workspace]
org = "test-org"
default_branch = "main"

[repos]
repo-a = {}
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)

	// Create a nested directory to load from
	nested := filepath.Join(root, "repos", "repo-a", "main")
	os.MkdirAll(nested, 0755)

	cfg, foundRoot, err := Load(nested)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if foundRoot != root {
		t.Errorf("root = %q, want %q", foundRoot, root)
	}
	if cfg.Workspace.Org != "test-org" {
		t.Errorf("org = %q, want %q", cfg.Workspace.Org, "test-org")
	}
}

func TestLoad_NotFound(t *testing.T) {
	tmp := t.TempDir()
	_, _, err := Load(tmp)
	if err == nil {
		t.Error("expected error when ws.toml not found")
	}
}

func TestMerge_AppendsAliases(t *testing.T) {
	base := &Config{
		Repos: map[string]RepoConfig{
			"repo-a": {Aliases: []string{"a", "alpha"}},
		},
	}
	local := &Config{
		Repos: map[string]RepoConfig{
			"repo-a": {Aliases: []string{"my-a"}},
		},
	}
	merged := Merge(base, local)
	got := merged.Repos["repo-a"].Aliases
	want := []string{"a", "alpha", "my-a"}
	if len(got) != len(want) {
		t.Fatalf("aliases = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("aliases[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Verify base was not mutated
	if len(base.Repos["repo-a"].Aliases) != 2 {
		t.Errorf("base was mutated: aliases = %v", base.Repos["repo-a"].Aliases)
	}
}

func TestMerge_ReplacesPostCreate(t *testing.T) {
	base := &Config{
		Repos: map[string]RepoConfig{
			"repo-a": {PostCreate: "npm install"},
		},
	}
	local := &Config{
		Repos: map[string]RepoConfig{
			"repo-a": {PostCreate: "bun install"},
		},
	}
	merged := Merge(base, local)
	if got := merged.Repos["repo-a"].PostCreate; got != "bun install" {
		t.Errorf("post_create = %q, want %q", got, "bun install")
	}
}

func TestMerge_ReplacesColor(t *testing.T) {
	base := &Config{
		Repos: map[string]RepoConfig{
			"repo-a": {Color: "#ff0000"},
		},
	}
	local := &Config{
		Repos: map[string]RepoConfig{
			"repo-a": {Color: "#00ff00"},
		},
	}
	merged := Merge(base, local)
	if got := merged.Repos["repo-a"].Color; got != "#00ff00" {
		t.Errorf("color = %q, want %q", got, "#00ff00")
	}
}

func TestMerge_ReplacesDisplayName(t *testing.T) {
	base := &Config{
		Repos: map[string]RepoConfig{
			"repo-a": {DisplayName: "Repo A"},
		},
	}
	local := &Config{
		Repos: map[string]RepoConfig{
			"repo-a": {DisplayName: "My Repo A"},
		},
	}
	merged := Merge(base, local)
	if got := merged.Repos["repo-a"].DisplayName; got != "My Repo A" {
		t.Errorf("display_name = %q, want %q", got, "My Repo A")
	}
}

func TestMerge_IgnoresUnknownRepos(t *testing.T) {
	base := &Config{
		Repos: map[string]RepoConfig{
			"repo-a": {Color: "#ff0000"},
		},
	}
	local := &Config{
		Repos: map[string]RepoConfig{
			"repo-a":   {Color: "#00ff00"},
			"repo-new": {Color: "#0000ff", PostCreate: "make"},
		},
	}
	merged := Merge(base, local)
	if _, ok := merged.Repos["repo-new"]; ok {
		t.Error("unknown repo 'repo-new' should not appear in merged config")
	}
	if len(merged.Repos) != 1 {
		t.Errorf("repos count = %d, want 1", len(merged.Repos))
	}
}

func TestMerge_EmptyFieldsNoOverride(t *testing.T) {
	base := &Config{
		Repos: map[string]RepoConfig{
			"repo-a": {
				DisplayName: "Original",
				Color:       "#ff0000",
				PostCreate:  "npm install",
				Aliases:     []string{"a"},
			},
		},
	}
	local := &Config{
		Repos: map[string]RepoConfig{
			"repo-a": {}, // all fields empty/zero
		},
	}
	merged := Merge(base, local)
	repo := merged.Repos["repo-a"]
	if repo.DisplayName != "Original" {
		t.Errorf("display_name = %q, want %q", repo.DisplayName, "Original")
	}
	if repo.Color != "#ff0000" {
		t.Errorf("color = %q, want %q", repo.Color, "#ff0000")
	}
	if repo.PostCreate != "npm install" {
		t.Errorf("post_create = %q, want %q", repo.PostCreate, "npm install")
	}
	if len(repo.Aliases) != 1 || repo.Aliases[0] != "a" {
		t.Errorf("aliases = %v, want [a]", repo.Aliases)
	}
}

func TestLoad_WithLocalConfig(t *testing.T) {
	root := t.TempDir()
	base := `[workspace]
org = "test-org"
default_branch = "main"

[repos.repo-a]
color = "#ff0000"
post_create = "npm install"
aliases = ["a"]
`
	local := `[repos.repo-a]
color = "#00ff00"
post_create = "bun install"
aliases = ["my-a"]
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(base), 0644)
	os.WriteFile(filepath.Join(root, "ws.local.toml"), []byte(local), 0644)

	cfg, foundRoot, err := Load(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if foundRoot != root {
		t.Errorf("root = %q, want %q", foundRoot, root)
	}
	repo := cfg.Repos["repo-a"]
	if repo.Color != "#00ff00" {
		t.Errorf("color = %q, want %q", repo.Color, "#00ff00")
	}
	if repo.PostCreate != "bun install" {
		t.Errorf("post_create = %q, want %q", repo.PostCreate, "bun install")
	}
	want := []string{"a", "my-a"}
	if len(repo.Aliases) != len(want) {
		t.Fatalf("aliases = %v, want %v", repo.Aliases, want)
	}
	for i := range want {
		if repo.Aliases[i] != want[i] {
			t.Errorf("aliases[%d] = %q, want %q", i, repo.Aliases[i], want[i])
		}
	}
}

func TestLoad_WithBoardedSection(t *testing.T) {
	root := t.TempDir()
	base := `[workspace]
org = "test-org"
default_branch = "main"

[repos.repo-a]
color = "#ff0000"
`
	local := `[repos.repo-a]
color = "#00ff00"

[boarded]
repo-a = ["main", "feature-x"]
repo-b = ["main"]
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(base), 0644)
	os.WriteFile(filepath.Join(root, "ws.local.toml"), []byte(local), 0644)

	cfg, _, err := Load(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Repos["repo-a"].Color != "#00ff00" {
		t.Errorf("color = %q, want %q", cfg.Repos["repo-a"].Color, "#00ff00")
	}
	if len(cfg.Boarded) != 2 {
		t.Fatalf("boarded count = %d, want 2", len(cfg.Boarded))
	}
	if len(cfg.Boarded["repo-a"]) != 2 || cfg.Boarded["repo-a"][0] != "main" || cfg.Boarded["repo-a"][1] != "feature-x" {
		t.Errorf("boarded[repo-a] = %v, want [main feature-x]", cfg.Boarded["repo-a"])
	}
	if len(cfg.Boarded["repo-b"]) != 1 || cfg.Boarded["repo-b"][0] != "main" {
		t.Errorf("boarded[repo-b] = %v, want [main]", cfg.Boarded["repo-b"])
	}
}

func TestLoad_NoBoardedSection(t *testing.T) {
	root := t.TempDir()
	content := `[workspace]
org = "test-org"
default_branch = "main"

[repos.repo-a]
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)

	cfg, _, err := Load(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Boarded == nil {
		t.Error("Boarded should be initialized (empty map), not nil")
	}
	if len(cfg.Boarded) != 0 {
		t.Errorf("boarded count = %d, want 0", len(cfg.Boarded))
	}
}

func TestLoad_WithoutLocalConfig(t *testing.T) {
	root := t.TempDir()
	content := `[workspace]
org = "test-org"
default_branch = "main"

[repos.repo-a]
color = "#ff0000"
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)
	// No ws.local.toml

	cfg, _, err := Load(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Repos["repo-a"].Color != "#ff0000" {
		t.Errorf("color = %q, want %q", cfg.Repos["repo-a"].Color, "#ff0000")
	}
}

func TestSaveBoarded_CreatesFile(t *testing.T) {
	root := t.TempDir()
	boarded := map[string][]string{
		"repo-a": {"main", "feature-x"},
		"repo-b": {"main"},
	}
	if err := SaveBoarded(root, boarded); err != nil {
		t.Fatalf("SaveBoarded() error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(root, "ws.local.toml"))
	if err != nil {
		t.Fatalf("reading ws.local.toml: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "[boarded]") {
		t.Error("expected [boarded] section in output")
	}
	if !strings.Contains(s, "repo-a") {
		t.Error("expected repo-a in output")
	}
}

func TestSaveBoarded_PreservesExistingRepoOverrides(t *testing.T) {
	root := t.TempDir()
	existing := `[repos.repo-a]
color = "#ff0000"

[boarded]
repo-a = ["old-branch"]
`
	os.WriteFile(filepath.Join(root, "ws.local.toml"), []byte(existing), 0644)

	boarded := map[string][]string{
		"repo-a": {"main", "feature-x"},
	}
	if err := SaveBoarded(root, boarded); err != nil {
		t.Fatalf("SaveBoarded() error: %v", err)
	}

	// Reload and verify both repo overrides and boarded section
	cfg := &LocalConfig{}
	_, err := toml.DecodeFile(filepath.Join(root, "ws.local.toml"), cfg)
	if err != nil {
		t.Fatalf("re-parse error: %v", err)
	}
	if cfg.Repos["repo-a"].Color != "#ff0000" {
		t.Errorf("color was lost: got %q", cfg.Repos["repo-a"].Color)
	}
	if len(cfg.Boarded["repo-a"]) != 2 || cfg.Boarded["repo-a"][0] != "main" {
		t.Errorf("boarded = %v, want [main feature-x]", cfg.Boarded["repo-a"])
	}
}

func TestSaveBoarded_EmptyMap(t *testing.T) {
	root := t.TempDir()
	if err := SaveBoarded(root, map[string][]string{}); err != nil {
		t.Fatalf("SaveBoarded() error: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(root, "ws.local.toml"))
	s := string(content)
	if !strings.Contains(s, "[boarded]") {
		t.Error("expected [boarded] section even when empty")
	}
}

func TestLoad_WithGitProtocol(t *testing.T) {
	root := t.TempDir()
	base := `[workspace]
org = "test-org"
default_branch = "main"

[repos.repo-a]
`
	local := `git = "ssh"

[repos.repo-a]
color = "#00ff00"
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(base), 0644)
	os.WriteFile(filepath.Join(root, "ws.local.toml"), []byte(local), 0644)

	cfg, _, err := Load(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Git != "ssh" {
		t.Errorf("git = %q, want %q", cfg.Git, "ssh")
	}
}

func TestLoad_GitProtocolDefault(t *testing.T) {
	root := t.TempDir()
	content := `[workspace]
org = "test-org"
default_branch = "main"

[repos.repo-a]
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)

	cfg, _, err := Load(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Git != "" {
		t.Errorf("git = %q, want empty", cfg.Git)
	}
}

func TestLoad_InvalidGitProtocol(t *testing.T) {
	root := t.TempDir()
	base := `[workspace]
org = "test-org"
default_branch = "main"

[repos.repo-a]
`
	local := `git = "ftp"
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(base), 0644)
	os.WriteFile(filepath.Join(root, "ws.local.toml"), []byte(local), 0644)

	_, _, err := Load(root)
	if err == nil {
		t.Error("expected error for invalid git protocol")
	}
}

func TestSaveBoarded_PreservesGitField(t *testing.T) {
	root := t.TempDir()
	existing := `git = "ssh"

[repos.repo-a]
color = "#ff0000"

[boarded]
repo-a = ["old-branch"]
`
	os.WriteFile(filepath.Join(root, "ws.local.toml"), []byte(existing), 0644)

	boarded := map[string][]string{
		"repo-a": {".ground"},
	}
	if err := SaveBoarded(root, boarded); err != nil {
		t.Fatalf("SaveBoarded() error: %v", err)
	}

	cfg := &LocalConfig{}
	_, err := toml.DecodeFile(filepath.Join(root, "ws.local.toml"), cfg)
	if err != nil {
		t.Fatalf("re-parse error: %v", err)
	}
	if cfg.Git != "ssh" {
		t.Errorf("git field was lost: got %q", cfg.Git)
	}
}
