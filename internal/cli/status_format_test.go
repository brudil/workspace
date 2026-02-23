package cli

import (
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/ui"
	"github.com/charmbracelet/lipgloss"
)

func TestComputeColumns(t *testing.T) {
	tests := []struct {
		name         string
		worktrees    []worktreeView
		wantMaxWidth int
		wantHasDirty bool
	}{
		{
			name:         "empty list",
			worktrees:    nil,
			wantMaxWidth: 0,
			wantHasDirty: false,
		},
		{
			name: "single worktree",
			worktrees: []worktreeView{
				{name: "main", loaded: true},
			},
			wantMaxWidth: 4,
			wantHasDirty: false,
		},
		{
			name: "mix of loaded and dirty",
			worktrees: []worktreeView{
				{name: "main", loaded: true, dirty: false},
				{name: "feature-branch", loaded: true, dirty: true},
				{name: "fix", loaded: true, dirty: false},
			},
			wantMaxWidth: 14,
			wantHasDirty: true,
		},
		{
			name: "dirty but not loaded should not set hasDirty",
			worktrees: []worktreeView{
				{name: "main", loaded: false, dirty: true},
				{name: "dev", loaded: false, dirty: true},
			},
			wantMaxWidth: 4,
			wantHasDirty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols := computeColumns(tt.worktrees)
			if cols.maxNameWidth != tt.wantMaxWidth {
				t.Errorf("maxNameWidth = %d, want %d", cols.maxNameWidth, tt.wantMaxWidth)
			}
			if cols.hasDirty != tt.wantHasDirty {
				t.Errorf("hasDirty = %v, want %v", cols.hasDirty, tt.wantHasDirty)
			}
		})
	}
}

func TestFormatAheadBehind(t *testing.T) {
	tests := []struct {
		ahead, behind int
		want          string
	}{
		{0, 0, ""},
		{3, 0, "↑3"},
		{0, 2, "↓2"},
		{3, 2, "↑3↓2"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatAheadBehind(tt.ahead, tt.behind)
			if got != tt.want {
				t.Errorf("formatAheadBehind(%d, %d) = %q, want %q", tt.ahead, tt.behind, got, tt.want)
			}
		})
	}
}

func TestFormatPRInfo(t *testing.T) {
	tests := []struct {
		name     string
		pr       *github.PR
		contains []string
		isEmpty  bool
	}{
		{
			name:    "nil PR returns empty",
			pr:      nil,
			isEmpty: true,
		},
		{
			name:     "PR with success status",
			pr:       &github.PR{Number: 42, StatusRollup: "success"},
			contains: []string{"#42", "✓"},
		},
		{
			name:     "PR with failure status",
			pr:       &github.PR{Number: 10, StatusRollup: "failure"},
			contains: []string{"#10", "✗"},
		},
		{
			name:     "PR with pending status",
			pr:       &github.PR{Number: 7, StatusRollup: "pending"},
			contains: []string{"#7", "◌"},
		},
		{
			name:     "PR with APPROVED review",
			pr:       &github.PR{Number: 1, ReviewDecision: "APPROVED"},
			contains: []string{"#1", "cleared"},
		},
		{
			name:     "PR with CHANGES_REQUESTED review",
			pr:       &github.PR{Number: 2, ReviewDecision: "CHANGES_REQUESTED"},
			contains: []string{"#2", "changes req"},
		},
		{
			name:     "PR with REVIEW_REQUIRED review",
			pr:       &github.PR{Number: 3, ReviewDecision: "REVIEW_REQUIRED"},
			contains: []string{"#3", "review needed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatPRInfo(tt.pr)
			if tt.isEmpty {
				if got != "" {
					t.Errorf("expected empty string, got %q", got)
				}
				return
			}
			for _, s := range tt.contains {
				if !strings.Contains(got, s) {
					t.Errorf("formatPRInfo() = %q, want it to contain %q", got, s)
				}
			}
		})
	}
}

func TestFormatWorktreeLine(t *testing.T) {
	tests := []struct {
		name     string
		wt       worktreeView
		boarded  bool
		cols     wtColumns
		pr       *github.PR
		contains []string
	}{
		{
			name:     "unloaded shows ellipsis",
			wt:       worktreeView{name: "feat", loaded: false},
			cols:     wtColumns{maxNameWidth: 4},
			contains: []string{"…"},
		},
		{
			name:     "dirty shows bullet",
			wt:       worktreeView{name: "feat", loaded: true, dirty: true, branch: "feat"},
			cols:     wtColumns{maxNameWidth: 4, hasDirty: true},
			contains: []string{"●"},
		},
		{
			name:     "boarded shows marker",
			wt:       worktreeView{name: "feat", loaded: true, branch: "feat"},
			boarded:  true,
			cols:     wtColumns{maxNameWidth: 4},
			contains: []string{ui.BoardedMarker},
		},
		{
			name:     "branch differs from name shows arrow",
			wt:       worktreeView{name: "feat", loaded: true, branch: "feature/other"},
			cols:     wtColumns{maxNameWidth: 4},
			contains: []string{"→", "feature/other"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatWorktreeLine(tt.wt, tt.boarded, tt.cols, tt.pr)
			for _, s := range tt.contains {
				if !strings.Contains(got, s) {
					t.Errorf("formatWorktreeLine() = %q, want it to contain %q", got, s)
				}
			}
		})
	}
}

func TestRenderRepoBlock(t *testing.T) {
	lines := []string{"header", "rule", "worktree1"}
	ruleIdx := 1
	color := lipgloss.Color("63")

	got := renderRepoBlock(lines, ruleIdx, color)
	rendered := strings.Split(got, "\n")

	if len(rendered) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(rendered))
	}

	// Line at ruleIdx should contain ├─
	if !strings.Contains(rendered[ruleIdx], "├─") {
		t.Errorf("line at ruleIdx should contain ├─, got %q", rendered[ruleIdx])
	}

	// Other lines should contain │
	for i, line := range rendered {
		if i != ruleIdx {
			if !strings.Contains(line, "│") {
				t.Errorf("line %d should contain │, got %q", i, line)
			}
		}
	}
}

func TestFormatFooter(t *testing.T) {
	tests := []struct {
		name                                                     string
		repoCount, wtCount, dirtyCount, behindCount, prCount, prErrors int
		contains                                                 []string
		notContains                                              []string
	}{
		{
			name:        "all clean",
			repoCount:   2,
			wtCount:     5,
			contains:    []string{"2 repos", "5 worktrees", "all clean"},
			notContains: []string{"dirty", "behind"},
		},
		{
			name:       "some dirty",
			repoCount:  2,
			wtCount:    5,
			dirtyCount: 3,
			contains:   []string{"2 repos", "5 worktrees", "3 dirty"},
			notContains: []string{"all clean"},
		},
		{
			name:        "some behind",
			repoCount:   1,
			wtCount:     3,
			behindCount: 1,
			contains:    []string{"1 repos", "3 worktrees", "1 behind"},
			notContains: []string{"all clean"},
		},
		{
			name:      "with PRs",
			repoCount: 2,
			wtCount:   4,
			prCount:   3,
			contains:  []string{"3 open PRs", "all clean"},
		},
		{
			name:      "with PR errors",
			repoCount: 1,
			wtCount:   2,
			prErrors:  1,
			contains:  []string{"PRs unavailable", "all clean"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatFooter(tt.repoCount, tt.wtCount, tt.dirtyCount, tt.behindCount, tt.prCount, tt.prErrors)
			for _, s := range tt.contains {
				if !strings.Contains(got, s) {
					t.Errorf("formatFooter() = %q, want it to contain %q", got, s)
				}
			}
			for _, s := range tt.notContains {
				if strings.Contains(got, s) {
					t.Errorf("formatFooter() = %q, should NOT contain %q", got, s)
				}
			}
		})
	}
}
