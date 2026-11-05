// Package testgit keeps tests' git commands independent of the machine's
// git setup.
package testgit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NoSigning points git, for this process and everything it starts, at a
// global config that includes the real one (so user.name, url rewrites and
// the rest still apply) but never signs commits or tags. Tests make many
// commits, and with commit.gpgsign on, each one asks a signing agent
// (1Password, gpg): slow, and failing outright when it's locked. Call it
// from TestMain; it returns the cleanup.
func NoSigning() func() {
	dir, err := os.MkdirTemp("", "ws-testgit-")
	if err != nil {
		return func() {}
	}
	var includes []string
	if g := os.Getenv("GIT_CONFIG_GLOBAL"); g != "" {
		includes = append(includes, g)
	} else if home, err := os.UserHomeDir(); err == nil {
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".config")
		}
		// git reads these two in this order; a missing one is skipped.
		includes = append(includes, filepath.Join(xdg, "git", "config"), filepath.Join(home, ".gitconfig"))
	}
	var b strings.Builder
	for _, p := range includes {
		fmt.Fprintf(&b, "[include]\n\tpath = %s\n", p)
	}
	b.WriteString("[commit]\n\tgpgsign = false\n[tag]\n\tgpgsign = false\n")
	path := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		os.RemoveAll(dir)
		return func() {}
	}
	old, had := os.LookupEnv("GIT_CONFIG_GLOBAL")
	os.Setenv("GIT_CONFIG_GLOBAL", path)
	return func() {
		if had {
			os.Setenv("GIT_CONFIG_GLOBAL", old)
		} else {
			os.Unsetenv("GIT_CONFIG_GLOBAL")
		}
		os.RemoveAll(dir)
	}
}
