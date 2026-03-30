package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const siloManifestName = ".silo-manifest"

// GitSyncableFiles returns all files eligible for silo sync in the given directory.
// This includes both git-tracked files and untracked files that are not gitignored.
// Uses -z for null-separated output to handle filenames with special characters.
func GitSyncableFiles(dir string) ([]string, error) {
	out, err := runGitOutput(dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimRight(out, "\x00")
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\x00"), nil
}

// IsGitIgnored returns true if the given relative path is ignored by git in dir.
// Uses git check-ignore which respects .gitignore, .git/info/exclude, and global excludes.
func IsGitIgnored(dir, relPath string) bool {
	err := runGit(dir, "check-ignore", "-q", relPath)
	return err == nil
}

// FullSync copies all syncable files (tracked + untracked non-ignored) from srcDir to dstDir.
// Previously synced files not in the new source set are removed.
// Non-tracked files in dstDir are left alone.
// A manifest file tracks what was synced so cleanup doesn't depend on the
// silo's git index (which is detached and stale).
// Individual file errors are collected and returned but do not abort the sync.
func FullSync(srcDir, dstDir string) (int, error) {
	srcFiles, err := GitSyncableFiles(srcDir)
	if err != nil {
		return 0, fmt.Errorf("listing source files: %w", err)
	}

	srcSet := make(map[string]bool, len(srcFiles))
	for _, f := range srcFiles {
		srcSet[f] = true
	}

	// Copy all syncable source files to dest.
	// Continue past individual errors so one missing file doesn't block the rest.
	var synced []string
	var errs []string
	for _, f := range srcFiles {
		src := filepath.Join(srcDir, f)
		dst := filepath.Join(dstDir, f)

		data, err := os.ReadFile(src)
		if err != nil {
			errs = append(errs, f)
			continue
		}
		info, err := os.Stat(src)
		if err != nil {
			errs = append(errs, f)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			errs = append(errs, f)
			continue
		}
		if err := os.WriteFile(dst, data, info.Mode()); err != nil {
			errs = append(errs, f)
			continue
		}
		synced = append(synced, f)
	}

	// Remove files that were in the previous sync but are no longer in the source.
	prevFiles := readManifest(dstDir)
	for _, f := range prevFiles {
		if !srcSet[f] {
			os.Remove(filepath.Join(dstDir, f))
		}
	}

	// Write manifest with successfully synced files
	writeManifest(dstDir, synced)

	if len(errs) > 0 {
		return len(srcFiles), fmt.Errorf("failed to sync %d file(s): %s", len(errs), strings.Join(errs, ", "))
	}
	return len(srcFiles), nil
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
