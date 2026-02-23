package ide

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateVSCode_Basic(t *testing.T) {
	root := t.TempDir()
	// Create existing workspace file with settings
	existing := `{
  "folders": [{"name": "old", "path": "old"}],
  "settings": {"editor.fontSize": 14}
}`
	os.WriteFile(filepath.Join(root, "workspace.code-workspace"), []byte(existing), 0644)

	boarded := map[string][]string{
		"repo-a": {"main", "feature-x"},
	}
	displayNames := map[string]string{}

	if err := GenerateVSCode(root, boarded, displayNames); err != nil {
		t.Fatalf("GenerateVSCode() error: %v", err)
	}

	content, _ := os.ReadFile(filepath.Join(root, "workspace.code-workspace"))
	var result map[string]any
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Folders should be updated
	folders, ok := result["folders"].([]any)
	if !ok || len(folders) != 2 {
		t.Fatalf("folders = %v, want 2 entries", result["folders"])
	}
	f0 := folders[0].(map[string]any)
	if f0["name"] != "repo-a (main)" {
		t.Errorf("folder[0].name = %v, want %q", f0["name"], "repo-a (main)")
	}
	if f0["path"] != "repos/repo-a/main" {
		t.Errorf("folder[0].path = %v, want %q", f0["path"], "repos/repo-a/main")
	}

	// Settings should be preserved
	settings, ok := result["settings"].(map[string]any)
	if !ok {
		t.Fatal("settings were lost")
	}
	if settings["editor.fontSize"] != float64(14) {
		t.Errorf("settings lost: %v", settings)
	}
}

func TestGenerateVSCode_WithDisplayNames(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "workspace.code-workspace"), []byte("{}"), 0644)

	boarded := map[string][]string{"my-long-repo": {"main"}}
	displayNames := map[string]string{"my-long-repo": "Short Name"}

	GenerateVSCode(root, boarded, displayNames)

	content, _ := os.ReadFile(filepath.Join(root, "workspace.code-workspace"))
	var result map[string]any
	json.Unmarshal(content, &result)

	folders := result["folders"].([]any)
	f0 := folders[0].(map[string]any)
	if f0["name"] != "Short Name (main)" {
		t.Errorf("folder name = %v, want %q", f0["name"], "Short Name (main)")
	}
}

func TestGenerateVSCode_NoFile(t *testing.T) {
	root := t.TempDir()
	// No workspace.code-workspace — should be a no-op
	boarded := map[string][]string{"repo-a": {"main"}}
	if err := GenerateVSCode(root, boarded, nil); err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	// File should not have been created
	if _, err := os.Stat(filepath.Join(root, "workspace.code-workspace")); !os.IsNotExist(err) {
		t.Error("file should not have been created when it didn't exist")
	}
}

func TestGenerateVSCode_RepoOrder(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "workspace.code-workspace"), []byte("{}"), 0644)

	boarded := map[string][]string{
		"zebra": {"main"},
		"alpha": {"main"},
	}

	GenerateVSCode(root, boarded, nil)

	content, _ := os.ReadFile(filepath.Join(root, "workspace.code-workspace"))
	var result map[string]any
	json.Unmarshal(content, &result)

	folders := result["folders"].([]any)
	f0 := folders[0].(map[string]any)
	f1 := folders[1].(map[string]any)
	if f0["name"] != "alpha (main)" {
		t.Errorf("first folder = %v, want alpha", f0["name"])
	}
	if f1["name"] != "zebra (main)" {
		t.Errorf("second folder = %v, want zebra", f1["name"])
	}
}

func TestGenerateVSCode_JSONCReturnsError(t *testing.T) {
	root := t.TempDir()
	jsonc := `{
  "folders": [],
  "extensions": {
    "recommendations": [
      // this is a comment
      "some.extension"
    ]
  }
}`
	os.WriteFile(filepath.Join(root, "workspace.code-workspace"), []byte(jsonc), 0644)

	boarded := map[string][]string{"repo-a": {"main"}}
	err := GenerateVSCode(root, boarded, nil)
	if err == nil {
		t.Fatal("expected error for JSONC file, got nil")
	}
	if !strings.Contains(err.Error(), "Only standard JSON is supported") {
		t.Errorf("error message = %q, want it to mention standard JSON", err.Error())
	}
}
