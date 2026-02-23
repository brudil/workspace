package ide

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// GenerateIDEA updates .idea/modules.xml, .idea/modules/*.iml, .idea/vcs.xml,
// and .idea/jb-workspace.xml.
// No-op if .idea/ directory doesn't exist.
func GenerateIDEA(root string, boarded map[string][]string, org string) error {
	ideaDir := filepath.Join(root, ".idea")
	if _, err := os.Stat(ideaDir); os.IsNotExist(err) {
		return nil
	}

	modulesDir := filepath.Join(ideaDir, "modules")
	os.MkdirAll(modulesDir, 0755)

	// Sort repos for deterministic output
	repos := make([]string, 0, len(boarded))
	for repo := range boarded {
		repos = append(repos, repo)
	}
	sort.Strings(repos)

	// Track which .iml files we write so we can clean stale ones
	activeIMLs := make(map[string]bool)

	// Count total entries for pre-allocation
	totalEntries := 0
	for _, capsules := range boarded {
		totalEntries += len(capsules)
	}

	moduleEntries := make([]string, 0, totalEntries)
	vcsEntries := make([]string, 0, totalEntries)

	for _, repo := range repos {
		// Find an existing .iml for this repo to use as a template (preserves SDK/facet config)
		repoTemplate := findRepoIMLTemplate(modulesDir, repo)

		for _, capsule := range boarded[repo] {
			imlName := repo + "-" + capsule + ".iml"
			activeIMLs[imlName] = true

			// Module entry for modules.xml
			moduleEntries = append(moduleEntries, fmt.Sprintf(
				`      <module fileurl="file://$PROJECT_DIR$/.idea/modules/%s" filepath="$PROJECT_DIR$/.idea/modules/%s" />`,
				imlName, imlName))

			// Write .iml file — use existing template for this repo if available
			contentURL := fmt.Sprintf("file://$MODULE_DIR$/../../repos/%s/%s", repo, capsule)
			iml := imlFromTemplate(repoTemplate, contentURL)
			if err := os.WriteFile(filepath.Join(modulesDir, imlName), []byte(iml), 0644); err != nil {
				return err
			}

			// VCS entry
			vcsEntries = append(vcsEntries, fmt.Sprintf(
				`    <mapping directory="$PROJECT_DIR$/repos/%s/%s" vcs="Git" />`,
				repo, capsule))
		}
	}

	// Write modules.xml
	modulesXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<project version="4">
  <component name="ProjectModuleManager">
    <modules>
%s
    </modules>
  </component>
</project>
`, strings.Join(moduleEntries, "\n"))
	if err := os.WriteFile(filepath.Join(ideaDir, "modules.xml"), []byte(modulesXML), 0644); err != nil {
		return err
	}

	// Write vcs.xml
	vcsXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<project version="4">
  <component name="VcsDirectoryMappings">
%s
  </component>
</project>
`, strings.Join(vcsEntries, "\n"))
	if err := os.WriteFile(filepath.Join(ideaDir, "vcs.xml"), []byte(vcsXML), 0644); err != nil {
		return err
	}

	// Write jb-workspace.xml (JetBrains linked-projects workspace file)
	projectEntries := make([]string, 0, totalEntries)
	for _, repo := range repos {
		remoteURL := fmt.Sprintf("https://github.com/%s/%s.git", org, repo)
		for _, capsule := range boarded[repo] {
			projectEntries = append(projectEntries, fmt.Sprintf(
				"    <project name=%q path=\"$PROJECT_DIR$/repos/%s/%s\">\n      <vcs id=\"Git\" remoteUrl=%q />\n    </project>",
				capsule, repo, capsule, remoteURL))
		}
	}
	jbWorkspaceXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<project version="4">
  <component name="WorkspaceSettings">
%s
    <option name="workspace" value="true" />
  </component>
</project>`, strings.Join(projectEntries, "\n"))
	if err := os.WriteFile(filepath.Join(ideaDir, "jb-workspace.xml"), []byte(jbWorkspaceXML), 0644); err != nil {
		return err
	}

	// Clean stale .iml files
	entries, _ := os.ReadDir(modulesDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".iml") && !activeIMLs[e.Name()] {
			os.Remove(filepath.Join(modulesDir, e.Name()))
		}
	}

	return nil
}

var contentURLRe = regexp.MustCompile(`<content url="[^"]*"`)

// findRepoIMLTemplate looks for any existing .iml file belonging to the given repo.
// Returns its contents if found, empty string otherwise.
func findRepoIMLTemplate(modulesDir, repo string) string {
	prefix := repo + "-"
	entries, _ := os.ReadDir(modulesDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".iml") {
			data, err := os.ReadFile(filepath.Join(modulesDir, e.Name()))
			if err == nil {
				return string(data)
			}
		}
	}
	return ""
}

// imlFromTemplate produces an .iml file with the given content URL.
// If template is non-empty, replaces the content URL in it. Otherwise generates a bare default.
func imlFromTemplate(template, contentURL string) string {
	if template != "" {
		return contentURLRe.ReplaceAllString(template, fmt.Sprintf(`<content url="%s"`, contentURL))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<module type="WEB_MODULE" version="4">
  <component name="NewModuleRootManager">
    <content url="%s" />
    <orderEntry type="inheritedJdk" />
    <orderEntry type="sourceFolder" forTests="false" />
  </component>
</module>
`, contentURL)
}
