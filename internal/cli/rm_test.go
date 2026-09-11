package cli

import (
	"path/filepath"
	"testing"
)

func TestDirContains(t *testing.T) {
	root := t.TempDir()
	capsule := filepath.Join(root, "repos", "repo-a", "my-feature")

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"capsule itself", capsule, true},
		{"nested dir", filepath.Join(capsule, "src", "pkg"), true},
		{"sibling capsule", filepath.Join(root, "repos", "repo-a", "other"), false},
		{"sibling with shared prefix", capsule + "-2", false},
		{"parent dir", filepath.Join(root, "repos", "repo-a"), false},
		{"workspace root", root, false},
		{"dotdot-prefixed child", filepath.Join(capsule, "..hidden"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dirContains(capsule, tt.path); got != tt.want {
				t.Errorf("dirContains(%q, %q) = %v, want %v", capsule, tt.path, got, tt.want)
			}
		})
	}
}
