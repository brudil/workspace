package testutil

import "github.com/brudil/workspace/internal/github"

// StubClient is a test double for github.Client.
type StubClient struct {
	PRsForRepoFn   func(org, repo string) ([]github.PR, error)
	PRFromNumberFn func(org, repo string, number int) (*github.PR, error)
	PRDetailFn     func(org, repo string, number int) (github.PRDetailResult, error)
}

func (s *StubClient) PRsForRepo(org, repo string) ([]github.PR, error) {
	if s.PRsForRepoFn != nil {
		return s.PRsForRepoFn(org, repo)
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
