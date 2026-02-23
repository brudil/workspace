package workspace

import (
	"path/filepath"
	"strings"
)

func DetectRepo(root, cwd string) (string, string, bool) {
	reposDir := filepath.Join(root, "repos")
	rel, err := filepath.Rel(reposDir, cwd)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return "", "", false
	}

	parts := strings.SplitN(rel, string(filepath.Separator), 3)
	if len(parts) < 2 {
		return "", "", false
	}

	return parts[0], parts[1], true
}
