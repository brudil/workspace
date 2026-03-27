package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const siloManifestName = ".silo-manifest"

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
// Previously synced files not in the new source set are removed.
// Non-tracked files in dstDir are left alone.
// A manifest file tracks what was synced so cleanup doesn't depend on the
// silo's git index (which is detached and stale).
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

	// Remove files that were in the previous sync but are no longer in the source.
	// We use a manifest file instead of git ls-files on the silo because the
	// silo's git index is detached and doesn't reflect what we've been copying.
	prevFiles := readManifest(dstDir)
	for _, f := range prevFiles {
		if !srcSet[f] {
			os.Remove(filepath.Join(dstDir, f))
		}
	}

	// Write updated manifest
	writeManifest(dstDir, srcFiles)

	return nil
}

// readManifest reads the list of previously synced files from the manifest.
func readManifest(siloDir string) []string {
	data, err := os.ReadFile(filepath.Join(siloDir, siloManifestName))
	if err != nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// writeManifest writes the list of synced files to the manifest.
func writeManifest(siloDir string, files []string) {
	sorted := make([]string, len(files))
	copy(sorted, files)
	sort.Strings(sorted)
	os.WriteFile(filepath.Join(siloDir, siloManifestName), []byte(strings.Join(sorted, "\n")+"\n"), 0644)
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
