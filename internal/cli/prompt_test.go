package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupPromptWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	content := `[workspace]
org = "test-org"
default_branch = "main"
display_name = "Test WS"

[repos.my-repo]
display_name = "My Repo"
color = "#ff5f87"
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)

	// Create repo dir with a worktree
	wtDir := filepath.Join(root, "repos", "my-repo", "feature-x")
	os.MkdirAll(wtDir, 0755)

	// Mark feature-x as boarded in ws.local.toml
	localContent := `[boarded]
my-repo = ["feature-x"]
`
	os.WriteFile(filepath.Join(root, "ws.local.toml"), []byte(localContent), 0644)

	return root
}

func TestResolvePromptData_NotInWorkspace(t *testing.T) {
	_, ok := resolvePromptData("/tmp")
	if ok {
		t.Error("expected ok=false when not in a workspace")
	}
}

func TestResolvePromptData_InWorkspaceRootNotRepo(t *testing.T) {
	root := setupPromptWorkspace(t)

	_, ok := resolvePromptData(root)
	if ok {
		t.Error("expected ok=false when in workspace root but not a repo dir")
	}
}

func TestResolvePromptData_InRepoWorktree(t *testing.T) {
	root := setupPromptWorkspace(t)
	cwd := filepath.Join(root, "repos", "my-repo", "feature-x")

	data, ok := resolvePromptData(cwd)
	if !ok {
		t.Fatal("expected ok=true when in a repo worktree")
	}
	if data.RepoName != "my-repo" {
		t.Errorf("RepoName = %q, want %q", data.RepoName, "my-repo")
	}
	if data.CapsuleName != "feature-x" {
		t.Errorf("CapsuleName = %q, want %q", data.CapsuleName, "feature-x")
	}
	if data.WorkspaceDisplayName != "Test WS" {
		t.Errorf("WorkspaceDisplayName = %q, want %q", data.WorkspaceDisplayName, "Test WS")
	}
	if data.RepoDisplayName != "My Repo" {
		t.Errorf("RepoDisplayName = %q, want %q", data.RepoDisplayName, "My Repo")
	}
	if data.RepoColor != "#ff5f87" {
		t.Errorf("RepoColor = %q, want %q", data.RepoColor, "#ff5f87")
	}
	if !data.IsCapsuleBoarded {
		t.Error("expected IsCapsuleBoarded=true when worktree is boarded")
	}
}

func TestResolvePromptData_NotCurrentWorktree(t *testing.T) {
	root := setupPromptWorkspace(t)

	// Create another worktree
	otherWT := filepath.Join(root, "repos", "my-repo", "main")
	os.MkdirAll(otherWT, 0755)

	data, ok := resolvePromptData(otherWT)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if data.IsCapsuleBoarded {
		t.Error("expected IsCapsuleBoarded=false when worktree is not boarded")
	}
}

func TestResolvePromptData_FallsBackToOrg(t *testing.T) {
	root := t.TempDir()

	content := `[workspace]
org = "my-org"
default_branch = "main"

[repos]
my-repo = {}
`
	os.WriteFile(filepath.Join(root, "ws.toml"), []byte(content), 0644)
	os.MkdirAll(filepath.Join(root, "repos", "my-repo", "main"), 0755)

	data, ok := resolvePromptData(filepath.Join(root, "repos", "my-repo", "main"))
	if !ok {
		t.Fatal("expected ok=true")
	}
	if data.WorkspaceDisplayName != "my-org" {
		t.Errorf("WorkspaceDisplayName = %q, want %q (org fallback)", data.WorkspaceDisplayName, "my-org")
	}
	if data.RepoDisplayName != "my-repo" {
		t.Errorf("RepoDisplayName = %q, want %q (repo name fallback)", data.RepoDisplayName, "my-repo")
	}
	if data.RepoColor != "" {
		t.Errorf("RepoColor = %q, want empty", data.RepoColor)
	}
}

func TestFormatPrompt_Short(t *testing.T) {
	f := tempFile(t)
	defer f.Close()

	data := PromptData{WorkspaceDisplayName: "Test WS", RepoName: "my-repo", RepoDisplayName: "My Repo", CapsuleName: "feature-x"}
	formatPrompt(f, data, "short", "")

	got := readTempFile(t, f)
	if got != "Test WS / My Repo\n" {
		t.Errorf("short format = %q, want %q", got, "Test WS / My Repo\n")
	}
}

func TestFormatPrompt_JSON(t *testing.T) {
	f := tempFile(t)
	defer f.Close()

	data := PromptData{WorkspaceDisplayName: "Test WS", RepoName: "my-repo", RepoDisplayName: "My Repo", RepoColor: "#ff5f87", CapsuleName: "main", IsCapsuleBoarded: true}
	formatPrompt(f, data, "json", "")

	got := readTempFile(t, f)
	var parsed PromptData
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed.WorkspaceDisplayName != "Test WS" {
		t.Errorf("WorkspaceDisplayName = %q, want %q", parsed.WorkspaceDisplayName, "Test WS")
	}
	if parsed.RepoName != "my-repo" {
		t.Errorf("RepoName = %q, want %q", parsed.RepoName, "my-repo")
	}
	if !parsed.IsCapsuleBoarded {
		t.Error("expected IsCapsuleBoarded=true")
	}
}

func TestFormatPrompt_Template(t *testing.T) {
	f := tempFile(t)
	defer f.Close()

	data := PromptData{WorkspaceDisplayName: "WS", RepoName: "my-repo", RepoDisplayName: "My Repo", CapsuleName: "feat"}
	formatPrompt(f, data, "", "{{.WorkspaceDisplayName}}/{{.RepoName}}:{{.CapsuleName}}")

	got := readTempFile(t, f)
	if got != "WS/my-repo:feat\n" {
		t.Errorf("template format = %q, want %q", got, "WS/my-repo:feat\n")
	}
}

func TestFormatPrompt_InSubdirectory(t *testing.T) {
	root := setupPromptWorkspace(t)

	// Create a subdirectory inside the worktree
	subDir := filepath.Join(root, "repos", "my-repo", "feature-x", "src", "pkg")
	os.MkdirAll(subDir, 0755)

	data, ok := resolvePromptData(subDir)
	if !ok {
		t.Fatal("expected ok=true when in a subdirectory of a worktree")
	}
	if data.RepoName != "my-repo" {
		t.Errorf("RepoName = %q, want %q", data.RepoName, "my-repo")
	}
	if data.CapsuleName != "feature-x" {
		t.Errorf("CapsuleName = %q, want %q", data.CapsuleName, "feature-x")
	}
}

// helpers

func tempFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "prompt-test-*")
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func readTempFile(t *testing.T, f *os.File) string {
	t.Helper()
	f.Sync()
	content, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
