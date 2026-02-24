package cli

import (
	"testing"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/workspace"
)

type debriefStubClient struct {
	openPRs   map[string][]github.PR // repo -> open PRs
	mergedPRs map[string][]github.PR // repo -> merged PRs
}

func (s *debriefStubClient) PRsForRepo(_, repo string) ([]github.PR, error) {
	return s.openPRs[repo], nil
}

func (s *debriefStubClient) MergedPRsForRepo(_, repo string) ([]github.PR, error) {
	return s.mergedPRs[repo], nil
}

func (s *debriefStubClient) PRFromNumber(_, _ string, _ int) (*github.PR, error) {
	return nil, nil
}

func (s *debriefStubClient) PRDetail(_, _ string, _ int) (github.PRDetailResult, error) {
	return github.PRDetailResult{}, nil
}

func (s *debriefStubClient) WorkflowRuns(_, _, _ string, _ int) ([]github.WorkflowRun, error) {
	return nil, nil
}

func TestFetchPRsByBranch_ReturnsMergedBranches(t *testing.T) {
	gh := &debriefStubClient{
		openPRs: map[string][]github.PR{},
		mergedPRs: map[string][]github.PR{
			"repo-a": {
				{Number: 10, HeadRefName: "squash-merged-branch", State: "MERGED"},
			},
		},
	}

	ctx := &Context{
		WS: &workspace.Workspace{
			Org:       "test-org",
			RepoNames: []string{"repo-a"},
		},
		GitHub: gh,
	}

	capsules := []workspace.CapsuleInfo{
		{Repo: "repo-a", Name: "squash-merged-branch", Branch: "squash-merged-branch"},
	}

	_, mergedBranches := fetchPRsByBranch(ctx, capsules)

	if !mergedBranches["squash-merged-branch"] {
		t.Error("expected squash-merged-branch to be in mergedBranches")
	}
}

func TestDebrief_SquashMergedPR_DetectedAsLanded(t *testing.T) {
	gh := &debriefStubClient{
		openPRs: map[string][]github.PR{},
		mergedPRs: map[string][]github.PR{
			"repo-a": {
				{Number: 42, HeadRefName: "feat-squashed", State: "MERGED"},
			},
		},
	}

	ctx := &Context{
		WS: &workspace.Workspace{
			Org:       "test-org",
			RepoNames: []string{"repo-a"},
		},
		GitHub: gh,
	}

	// Capsule not detected as merged by git (Merged: false)
	capsules := []workspace.CapsuleInfo{
		{Repo: "repo-a", Name: "feat-squashed", Branch: "feat-squashed", Merged: false},
	}

	_, mergedBranches := fetchPRsByBranch(ctx, capsules)

	// Cross-reference: same logic as runDebrief
	for i := range capsules {
		if !capsules[i].Merged && mergedBranches[capsules[i].Branch] {
			capsules[i].Merged = true
		}
	}

	if !capsules[0].Merged {
		t.Error("expected capsule with squash-merged PR to be marked as merged")
	}
}
