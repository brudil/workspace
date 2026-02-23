package ide

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegenerate_BothIDEs(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "workspace.code-workspace"), []byte("{}"), 0644)
	os.MkdirAll(filepath.Join(root, ".idea"), 0755)

	boarded := map[string][]string{"repo-a": {"main"}}
	if err := Regenerate(root, boarded, nil, "test-org"); err != nil {
		t.Fatalf("Regenerate() error: %v", err)
	}

	// VS Code file updated
	if _, err := os.Stat(filepath.Join(root, "workspace.code-workspace")); err != nil {
		t.Error("workspace.code-workspace should exist")
	}
	// IDEA files updated
	if _, err := os.Stat(filepath.Join(root, ".idea", "modules.xml")); err != nil {
		t.Error("modules.xml should exist")
	}
}

func TestRegenerate_NeitherIDE(t *testing.T) {
	root := t.TempDir()
	boarded := map[string][]string{"repo-a": {"main"}}
	// No IDE files — should be a clean no-op
	if err := Regenerate(root, boarded, nil, "test-org"); err != nil {
		t.Fatalf("Regenerate() error: %v", err)
	}
}
