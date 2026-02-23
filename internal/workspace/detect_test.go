package workspace

import "testing"

func TestDetectRepo(t *testing.T) {
	tests := []struct {
		name     string
		root     string
		cwd      string
		wantRepo string
		wantWT   string
		wantOK   bool
	}{
		{"inside worktree", "/ws", "/ws/repos/backend/main/src", "backend", "main", true},
		{"inside repo root", "/ws", "/ws/repos/frontend/feature-x", "frontend", "feature-x", true},
		{"at workspace root", "/ws", "/ws", "", "", false},
		{"in repos dir", "/ws", "/ws/repos", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, wt, ok := DetectRepo(tt.root, tt.cwd)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
			if wt != tt.wantWT {
				t.Errorf("worktree = %q, want %q", wt, tt.wantWT)
			}
		})
	}
}
