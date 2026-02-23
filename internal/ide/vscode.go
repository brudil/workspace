package ide

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// GenerateVSCode updates the folders in an existing .code-workspace file.
// No-op if the file doesn't exist.
func GenerateVSCode(root string, boarded map[string][]string, displayNames map[string]string) error {
	path := filepath.Join(root, "workspace.code-workspace")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}

	// Read existing file
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Parse into generic map to preserve unknown keys.
	// JSONC (comments, trailing commas) is not supported — fail with a clear message.
	var workspace map[string]any
	if err := json.Unmarshal(data, &workspace); err != nil {
		return fmt.Errorf("failed to parse %s: %w\nOnly standard JSON is supported (no comments or trailing commas)", path, err)
	}

	// Build folders from boarded state
	var folders []map[string]string
	repos := make([]string, 0, len(boarded))
	for repo := range boarded {
		repos = append(repos, repo)
	}
	sort.Strings(repos)

	for _, repo := range repos {
		displayName := repo
		if dn, ok := displayNames[repo]; ok {
			displayName = dn
		}
		for _, capsule := range boarded[repo] {
			folders = append(folders, map[string]string{
				"name": displayName + " (" + capsule + ")",
				"path": filepath.Join("repos", repo, capsule),
			})
		}
	}

	workspace["folders"] = folders

	out, err := json.MarshalIndent(workspace, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0644)
}
