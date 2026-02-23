package ide

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateIDEA_Basic(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".idea"), 0755)

	boarded := map[string][]string{
		"repo-a": {"main", "feature-x"},
	}

	if err := GenerateIDEA(root, boarded, "test-org"); err != nil {
		t.Fatalf("GenerateIDEA() error: %v", err)
	}

	// Check modules.xml exists and has 2 modules
	data, err := os.ReadFile(filepath.Join(root, ".idea", "modules.xml"))
	if err != nil {
		t.Fatalf("modules.xml not created: %v", err)
	}
	if !strings.Contains(string(data), "repo-a-main.iml") {
		t.Error("modules.xml missing repo-a-main.iml")
	}
	if !strings.Contains(string(data), "repo-a-feature-x.iml") {
		t.Error("modules.xml missing repo-a-feature-x.iml")
	}

	// Check .iml files exist
	if _, err := os.Stat(filepath.Join(root, ".idea", "modules", "repo-a-main.iml")); err != nil {
		t.Error("repo-a-main.iml not created")
	}
	if _, err := os.Stat(filepath.Join(root, ".idea", "modules", "repo-a-feature-x.iml")); err != nil {
		t.Error("repo-a-feature-x.iml not created")
	}

	// Check vcs.xml exists
	if _, err := os.Stat(filepath.Join(root, ".idea", "vcs.xml")); err != nil {
		t.Error("vcs.xml not created")
	}

	// Check jb-workspace.xml
	jbData, err := os.ReadFile(filepath.Join(root, ".idea", "jb-workspace.xml"))
	if err != nil {
		t.Fatalf("jb-workspace.xml not created: %v", err)
	}
	jb := string(jbData)
	if !strings.Contains(jb, `path="$PROJECT_DIR$/repos/repo-a/main"`) {
		t.Error("jb-workspace.xml missing repo-a/main project entry")
	}
	if !strings.Contains(jb, `path="$PROJECT_DIR$/repos/repo-a/feature-x"`) {
		t.Error("jb-workspace.xml missing repo-a/feature-x project entry")
	}
	if !strings.Contains(jb, `remoteUrl="https://github.com/test-org/repo-a.git"`) {
		t.Error("jb-workspace.xml missing remote URL")
	}
	if !strings.Contains(jb, `<option name="workspace" value="true" />`) {
		t.Error("jb-workspace.xml missing workspace option")
	}
}

func TestGenerateIDEA_NoIdeaDir(t *testing.T) {
	root := t.TempDir()
	// No .idea/ directory — should be a no-op
	boarded := map[string][]string{"repo-a": {"main"}}
	if err := GenerateIDEA(root, boarded, "test-org"); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".idea")); !os.IsNotExist(err) {
		t.Error(".idea/ should not have been created")
	}
}

func TestGenerateIDEA_CleansStaleIML(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".idea", "modules"), 0755)
	// Write a stale .iml file
	os.WriteFile(filepath.Join(root, ".idea", "modules", "old-repo-stale.iml"), []byte("<module/>"), 0644)

	boarded := map[string][]string{"repo-a": {"main"}}
	GenerateIDEA(root, boarded, "test-org")

	// Stale file should be gone
	if _, err := os.Stat(filepath.Join(root, ".idea", "modules", "old-repo-stale.iml")); !os.IsNotExist(err) {
		t.Error("stale .iml file should have been removed")
	}
	// New file should exist
	if _, err := os.Stat(filepath.Join(root, ".idea", "modules", "repo-a-main.iml")); err != nil {
		t.Error("repo-a-main.iml should exist")
	}
}

func TestGenerateIDEA_IMLTemplateFromExisting(t *testing.T) {
	root := t.TempDir()
	modulesDir := filepath.Join(root, ".idea", "modules")
	os.MkdirAll(modulesDir, 0755)

	// Pre-create an .iml with Python facet (simulates user SDK config via IDE)
	existingIML := `<?xml version="1.0" encoding="UTF-8"?>
<module type="WEB_MODULE" version="4">
  <component name="FacetManager">
    <facet type="Python" name="Python">
      <configuration sdkName="Python 3.14 (my-workspace)" />
    </facet>
  </component>
  <component name="NewModuleRootManager" inherit-compiler-output="true">
    <exclude-output />
    <content url="file://$MODULE_DIR$/../../repos/my-repo/main" />
    <orderEntry type="inheritedJdk" />
    <orderEntry type="sourceFolder" forTests="false" />
    <orderEntry type="library" name="Python 3.14 interpreter library" level="application" />
  </component>
</module>
`
	os.WriteFile(filepath.Join(modulesDir, "my-repo-main.iml"), []byte(existingIML), 0644)

	// Board a new capsule for the same repo
	boarded := map[string][]string{"my-repo": {"main", "feature-y"}}
	if err := GenerateIDEA(root, boarded, "test-org"); err != nil {
		t.Fatalf("GenerateIDEA() error: %v", err)
	}

	// The new capsule's .iml should have the Python facet from the template
	newIML, err := os.ReadFile(filepath.Join(modulesDir, "my-repo-feature-y.iml"))
	if err != nil {
		t.Fatalf("my-repo-feature-y.iml not created: %v", err)
	}
	content := string(newIML)
	if !strings.Contains(content, "FacetManager") {
		t.Error("new .iml missing FacetManager from template")
	}
	if !strings.Contains(content, `sdkName="Python 3.14 (my-workspace)"`) {
		t.Error("new .iml missing Python SDK from template")
	}
	// Content URL should point to the new capsule, not the template's
	if !strings.Contains(content, "repos/my-repo/feature-y") {
		t.Error("new .iml has wrong content URL")
	}
	if strings.Contains(content, "repos/my-repo/main") {
		t.Error("new .iml still has template's content URL")
	}
}

func TestGenerateIDEA_DoesNotTouchWorkspaceXML(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".idea"), 0755)
	os.WriteFile(filepath.Join(root, ".idea", "workspace.xml"), []byte("<user-state/>"), 0644)

	boarded := map[string][]string{"repo-a": {"main"}}
	GenerateIDEA(root, boarded, "test-org")

	content, _ := os.ReadFile(filepath.Join(root, ".idea", "workspace.xml"))
	if string(content) != "<user-state/>" {
		t.Error("workspace.xml was modified")
	}
}
