package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitLsFiles returns the list of git-tracked files in the given directory.
func GitLsFiles(dir string) ([]string, error) {
	out, err := runGitOutput(dir, "ls-files")
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

// IsGitTracked returns true if the given relative path is tracked by git in dir.
func IsGitTracked(dir, relPath string) bool {
	out, err := runGitOutput(dir, "ls-files", relPath)
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) != ""
}

// FullSync copies all git-tracked files from srcDir to dstDir.
// Files tracked in dstDir but not in srcDir are removed.
// Non-tracked files in dstDir are left alone.
func FullSync(srcDir, dstDir string) error {
	srcFiles, err := GitLsFiles(srcDir)
	if err != nil {
		return fmt.Errorf("listing source files: %w", err)
	}

	srcSet := make(map[string]bool, len(srcFiles))
	for _, f := range srcFiles {
		srcSet[f] = true
	}

	// Copy all tracked source files to dest
	for _, f := range srcFiles {
		src := filepath.Join(srcDir, f)
		dst := filepath.Join(dstDir, f)
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return fmt.Errorf("creating dir for %s: %w", f, err)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("reading %s: %w", f, err)
		}
		info, err := os.Stat(src)
		if err != nil {
			return fmt.Errorf("stat %s: %w", f, err)
		}
		if err := os.WriteFile(dst, data, info.Mode()); err != nil {
			return fmt.Errorf("writing %s: %w", f, err)
		}
	}

	// Remove files tracked in dest but absent from source
	dstFiles, err := GitLsFiles(dstDir)
	if err != nil {
		return nil // dest might not have git tracking yet
	}
	for _, f := range dstFiles {
		if !srcSet[f] {
			os.Remove(filepath.Join(dstDir, f))
		}
	}

	return nil
}

// SyncFile copies a single file from srcDir to dstDir (relative path).
func SyncFile(srcDir, dstDir, relPath string) error {
	src := filepath.Join(srcDir, relPath)
	dst := filepath.Join(dstDir, relPath)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode())
}

// RemoveSyncedFile removes a file from dstDir (relative path).
func RemoveSyncedFile(dstDir, relPath string) error {
	return os.Remove(filepath.Join(dstDir, relPath))
}
