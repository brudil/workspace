package cli

import (
	"errors"
	"testing"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/workspace"
)

func TestFormatLLMRepoHeader(t *testing.T) {
	tests := []struct {
		name        string
		canonical   string
		displayName string
		aliases     []string
		want        string
	}{
		{
			name:      "canonical only",
			canonical: "my-repo",
			want:      "my-repo",
		},
		{
			name:        "with display name",
			canonical:   "my-repo",
			displayName: "My Repo",
			want:        `my-repo ("My Repo")`,
		},
		{
			name:      "with aliases",
			canonical: "my-repo",
			aliases:   []string{"mr", "repo"},
			want:      "my-repo (aliases: mr, repo)",
		},
		{
			name:        "with display name and aliases",
			canonical:   "my-repo",
			displayName: "My Repo",
			aliases:     []string{"mr"},
			want:        `my-repo ("My Repo" — aliases: mr)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatLLMRepoHeader(tt.canonical, tt.displayName, tt.aliases)
			if got != tt.want {
				t.Errorf("formatLLMRepoHeader() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatLLMWorktreeLine(t *testing.T) {
	tests := []struct {
		name    string
		wtName  string
		status  workspace.WorktreeStatus
		pr      *github.PR
		boarded bool
		want    string
	}{
		{
			name:   "ground with branch",
			wtName: ".ground",
			status: workspace.WorktreeStatus{Branch: "main"},
			want:   ".ground",
		},
		{
			name:   "detached non-ground",
			wtName: "feature",
			status: workspace.WorktreeStatus{Branch: ""},
			want:   "feature (detached)",
		},
		{
			name:    "boarded",
			wtName:  "feature",
			status:  workspace.WorktreeStatus{Branch: "feature"},
			boarded: true,
			want:    "feature [boarded]",
		},
		{
			name:   "dirty with ahead/behind",
			wtName: "feature",
			status: workspace.WorktreeStatus{Branch: "feature", Dirty: true, Ahead: 3, Behind: 1},
			want:   "feature dirty ↑3 ↓1",
		},
		{
			name:   "with PR",
			wtName: "feature",
			status: workspace.WorktreeStatus{Branch: "feature"},
			pr:     &github.PR{Number: 42, State: "OPEN", StatusRollup: "success", ReviewDecision: "APPROVED"},
			want:   "feature PR #42 OPEN checks:pass review:approved",
		},
		{
			name:   "ground detached is not marked detached",
			wtName: ".ground",
			status: workspace.WorktreeStatus{Branch: ""},
			want:   ".ground",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatLLMWorktreeLine(tt.wtName, tt.status, tt.pr, tt.boarded)
			if got != tt.want {
				t.Errorf("formatLLMWorktreeLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatLLMPR(t *testing.T) {
	tests := []struct {
		name string
		pr   *github.PR
		want string
	}{
		{
			name: "open with success checks and approved",
			pr:   &github.PR{Number: 10, State: "OPEN", StatusRollup: "success", ReviewDecision: "APPROVED"},
			want: "PR #10 OPEN checks:pass review:approved",
		},
		{
			name: "failure checks and changes requested",
			pr:   &github.PR{Number: 20, State: "OPEN", StatusRollup: "failure", ReviewDecision: "CHANGES_REQUESTED"},
			want: "PR #20 OPEN checks:fail review:changes-requested",
		},
		{
			name: "pending checks and review required",
			pr:   &github.PR{Number: 30, State: "MERGED", StatusRollup: "pending", ReviewDecision: "REVIEW_REQUIRED"},
			want: "PR #30 MERGED checks:pending review:required",
		},
		{
			name: "no checks no review",
			pr:   &github.PR{Number: 5, State: "CLOSED"},
			want: "PR #5 CLOSED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatLLMPR(tt.pr)
			if got != tt.want {
				t.Errorf("formatLLMPR() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSimplifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "no such file or directory",
			err:  errors.New("stat /foo/bar: no such file or directory"),
			want: "repo not found",
		},
		{
			name: "other error",
			err:  errors.New("permission denied"),
			want: "permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := simplifyError(tt.err)
			if got != tt.want {
				t.Errorf("simplifyError() = %q, want %q", got, tt.want)
			}
		})
	}
}
