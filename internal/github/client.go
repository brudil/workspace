package github

// Client abstracts GitHub API access for testability.
type Client interface {
	PRsForRepo(org, repo string) ([]PR, error)
	MergedPRsForRepo(org, repo string) ([]PR, error)
	PRFromNumber(org, repo string, number int) (*PR, error)
	PRDetail(org, repo string, number int) (PRDetailResult, error)
	WorkflowRuns(org, repo, branch string, limit int) ([]WorkflowRun, error)
}

// LiveClient calls the real gh CLI.
type LiveClient struct{}

func (LiveClient) PRsForRepo(org, repo string) ([]PR, error) {
	return PRsForRepo(org, repo)
}

func (LiveClient) MergedPRsForRepo(org, repo string) ([]PR, error) {
	return MergedPRsForRepo(org, repo)
}

func (LiveClient) PRFromNumber(org, repo string, number int) (*PR, error) {
	return PRFromNumber(org, repo, number)
}

func (LiveClient) PRDetail(org, repo string, number int) (PRDetailResult, error) {
	return PRDetail(org, repo, number)
}

func (LiveClient) WorkflowRuns(org, repo, branch string, limit int) ([]WorkflowRun, error) {
	return WorkflowRuns(org, repo, branch, limit)
}
