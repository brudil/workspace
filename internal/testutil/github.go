package testutil

import (
	"testing"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/workspace"
)

// StubGitHubAuth replaces the GitHub auth check with a no-op for the
// duration of the test, so doctor tests don't depend on real gh auth.
func StubGitHubAuth(t *testing.T) {
	t.Helper()
	orig := workspace.CheckGitHubAuthFunc
	workspace.CheckGitHubAuthFunc = func() workspace.CheckResult {
		return workspace.CheckResult{Name: "gh auth", Status: workspace.CheckOK}
	}
	t.Cleanup(func() { workspace.CheckGitHubAuthFunc = orig })
}

// StubClient is a test double for github.Client.
type StubClient struct {
	PRsForRepoFn       func(org, repo string) ([]github.PR, error)
	MergedPRsForRepoFn func(org, repo string) ([]github.PR, error)
	PRFromNumberFn     func(org, repo string, number int) (*github.PR, error)
	PRDetailFn         func(org, repo string, number int) (github.PRDetailResult, error)
	WorkflowRunsFn     func(org, repo, branch string, limit int) ([]github.WorkflowRun, error)
}

func (s *StubClient) PRsForRepo(org, repo string) ([]github.PR, error) {
	if s.PRsForRepoFn != nil {
		return s.PRsForRepoFn(org, repo)
	}
	return nil, nil
}

func (s *StubClient) MergedPRsForRepo(org, repo string) ([]github.PR, error) {
	if s.MergedPRsForRepoFn != nil {
		return s.MergedPRsForRepoFn(org, repo)
	}
	return nil, nil
}

func (s *StubClient) PRFromNumber(org, repo string, number int) (*github.PR, error) {
	if s.PRFromNumberFn != nil {
		return s.PRFromNumberFn(org, repo, number)
	}
	return nil, nil
}

func (s *StubClient) PRDetail(org, repo string, number int) (github.PRDetailResult, error) {
	if s.PRDetailFn != nil {
		return s.PRDetailFn(org, repo, number)
	}
	return github.PRDetailResult{}, nil
}

func (s *StubClient) WorkflowRuns(org, repo, branch string, limit int) ([]github.WorkflowRun, error) {
	if s.WorkflowRunsFn != nil {
		return s.WorkflowRunsFn(org, repo, branch, limit)
	}
	return nil, nil
}
