package cli

import (
	"testing"

	"github.com/brudil/workspace/internal/github"
)

func TestLookupPR(t *testing.T) {
	prByBranch := &github.PR{Number: 1, Title: "branch PR"}
	prByName := &github.PR{Number: 2, Title: "name PR"}

	tests := []struct {
		name   string
		prs    map[string]*github.PR
		branch string
		wtName string
		want   *github.PR
	}{
		{
			name:   "branch match",
			prs:    map[string]*github.PR{"feat": prByBranch, "my-wt": prByName},
			branch: "feat",
			wtName: "my-wt",
			want:   prByBranch,
		},
		{
			name:   "name fallback when branch empty",
			prs:    map[string]*github.PR{"my-wt": prByName},
			branch: "",
			wtName: "my-wt",
			want:   prByName,
		},
		{
			name:   "name fallback when branch not found",
			prs:    map[string]*github.PR{"my-wt": prByName},
			branch: "other",
			wtName: "my-wt",
			want:   prByName,
		},
		{
			name:   "nil map",
			prs:    nil,
			branch: "feat",
			wtName: "my-wt",
			want:   nil,
		},
		{
			name:   "no match",
			prs:    map[string]*github.PR{"unrelated": prByBranch},
			branch: "feat",
			wtName: "my-wt",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lookupPR(tt.prs, tt.branch, tt.wtName)
			if got != tt.want {
				t.Errorf("lookupPR() = %v, want %v", got, tt.want)
			}
		})
	}
}
