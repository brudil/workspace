package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadContextFromDir(t *testing.T) {
	root := t.TempDir()
	content := `[workspace]
org = "test-org"
default_branch = "main"
display_name = "Test WS"

[repos]
repo-a = { display_name = "Repo A", aliases = ["ra"], color = "#ff0000" }
repo-b = {}
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)

	ctx, err := LoadContextFromDir(root)
	if err != nil {
		t.Fatalf("LoadContextFromDir() error: %v", err)
	}

	if ctx.WS.Org != "test-org" {
		t.Errorf("Org = %q, want %q", ctx.WS.Org, "test-org")
	}
	if ctx.WS.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want %q", ctx.WS.DefaultBranch, "main")
	}
	if ctx.WS.Name != "Test WS" {
		t.Errorf("Name = %q, want %q", ctx.WS.Name, "Test WS")
	}
	if len(ctx.WS.RepoNames) != 2 {
		t.Fatalf("RepoNames = %v, want 2 entries", ctx.WS.RepoNames)
	}
	if ctx.WS.RepoNames[0] != "repo-a" || ctx.WS.RepoNames[1] != "repo-b" {
		t.Errorf("RepoNames = %v, want [repo-a repo-b]", ctx.WS.RepoNames)
	}
	if ctx.WS.DisplayNames["repo-a"] != "Repo A" {
		t.Errorf("DisplayNames[repo-a] = %q, want %q", ctx.WS.DisplayNames["repo-a"], "Repo A")
	}
	if ctx.WS.RepoColors["repo-a"] != "#ff0000" {
		t.Errorf("RepoColors[repo-a] = %q, want %q", ctx.WS.RepoColors["repo-a"], "#ff0000")
	}
}

func TestLoadContextFromDir_AliasMap(t *testing.T) {
	root := t.TempDir()
	content := `[workspace]
org = "org"
default_branch = "main"

[repos]
long-repo-name = { aliases = ["short", "lr"] }
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)

	ctx, err := LoadContextFromDir(root)
	if err != nil {
		t.Fatalf("LoadContextFromDir() error: %v", err)
	}

	if got, ok := ctx.WS.AliasMap["short"]; !ok || got != "long-repo-name" {
		t.Errorf("AliasMap[short] = (%q, %v), want (%q, true)", got, ok, "long-repo-name")
	}
	if got, ok := ctx.WS.AliasMap["lr"]; !ok || got != "long-repo-name" {
		t.Errorf("AliasMap[lr] = (%q, %v), want (%q, true)", got, ok, "long-repo-name")
	}
}

func TestLoadContextFromDir_AliasCollidesWithRepoName(t *testing.T) {
	root := t.TempDir()
	content := `[workspace]
org = "org"
default_branch = "main"

[repos]
repo-a = { aliases = ["repo-b"] }
repo-b = {}
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)

	_, err := LoadContextFromDir(root)
	if err == nil {
		t.Error("expected error when alias collides with canonical name")
	}
}

func TestLoadContextFromDir_DuplicateAlias(t *testing.T) {
	root := t.TempDir()
	content := `[workspace]
org = "org"
default_branch = "main"

[repos]
repo-a = { aliases = ["x"] }
repo-b = { aliases = ["x"] }
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)

	_, err := LoadContextFromDir(root)
	if err == nil {
		t.Error("expected error when alias is used by two repos")
	}
}

func TestLoadContextFromDir_BoardedState(t *testing.T) {
	root := t.TempDir()
	base := `[workspace]
org = "test-org"
default_branch = "main"

[repos.repo-a]
`
	local := `[boarded]
repo-a = ["main", "feature-x"]
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(base), 0644)
	os.WriteFile(filepath.Join(root, "ws.local.toml"), []byte(local), 0644)

	ctx, err := LoadContextFromDir(root)
	if err != nil {
		t.Fatalf("LoadContextFromDir() error: %v", err)
	}
	if len(ctx.WS.Boarded["repo-a"]) != 2 {
		t.Errorf("Boarded[repo-a] = %v, want [main feature-x]", ctx.WS.Boarded["repo-a"])
	}
}

func TestLoadContextFromDir_PostCreateHooks(t *testing.T) {
	root := t.TempDir()
	content := `[workspace]
org = "org"
default_branch = "main"

[repos]
repo-a = { post_create = "npm install" }
repo-b = {}
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)

	ctx, err := LoadContextFromDir(root)
	if err != nil {
		t.Fatalf("LoadContextFromDir() error: %v", err)
	}

	if hook := ctx.WS.PostCreateHooks["repo-a"]; hook != "npm install" {
		t.Errorf("PostCreateHooks[repo-a] = %q, want %q", hook, "npm install")
	}
	if _, ok := ctx.WS.PostCreateHooks["repo-b"]; ok {
		t.Error("repo-b should not have a post_create hook")
	}
}

func TestResolveCapsule_ExactMatch(t *testing.T) {
	root := t.TempDir()
	content := `[workspace]
org = "org"
default_branch = "main"

[repos]
repo-a = {}
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)
	repoDir := filepath.Join(root, "repos", "repo-a")
	os.MkdirAll(filepath.Join(repoDir, "support-go"), 0755)
	os.MkdirAll(filepath.Join(repoDir, "feature-x"), 0755)

	ctx, _ := LoadContextFromDir(root)
	got, err := ctx.ResolveCapsule("repo-a", "support-go")
	if err != nil {
		t.Fatalf("ResolveCapsule() error: %v", err)
	}
	if got != "support-go" {
		t.Errorf("ResolveCapsule() = %q, want %q", got, "support-go")
	}
}

func TestResolveCapsule_FuzzySingleMatch(t *testing.T) {
	root := t.TempDir()
	content := `[workspace]
org = "org"
default_branch = "main"

[repos]
repo-a = {}
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)
	repoDir := filepath.Join(root, "repos", "repo-a")
	os.MkdirAll(filepath.Join(repoDir, "support-go"), 0755)
	os.MkdirAll(filepath.Join(repoDir, "feature-x"), 0755)

	ctx, _ := LoadContextFromDir(root)
	got, err := ctx.ResolveCapsule("repo-a", "sg")
	if err != nil {
		t.Fatalf("ResolveCapsule() error: %v", err)
	}
	if got != "support-go" {
		t.Errorf("ResolveCapsule() = %q, want %q", got, "support-go")
	}
}

func TestResolveCapsule_NoMatch(t *testing.T) {
	root := t.TempDir()
	content := `[workspace]
org = "org"
default_branch = "main"

[repos]
repo-a = {}
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)
	repoDir := filepath.Join(root, "repos", "repo-a")
	os.MkdirAll(filepath.Join(repoDir, "support-go"), 0755)

	ctx, _ := LoadContextFromDir(root)
	_, err := ctx.ResolveCapsule("repo-a", "xyz")
	if err == nil {
		t.Error("expected error for no matching capsule")
	}
}
