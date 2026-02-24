package cli

import "github.com/brudil/workspace/internal/github"

func lookupPR(prs map[string]*github.PR, branch, name string) *github.PR {
	if branch != "" {
		if pr := prs[branch]; pr != nil {
			return pr
		}
	}
	return prs[name]
}
