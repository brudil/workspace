package cli

import (
	"path/filepath"
	"sync"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/workspace"
)

type statusData struct {
	statuses  [][]workspace.WorktreeStatus
	prsByRepo map[string]map[string]*github.PR
}

func collectStatusData(ws *workspace.Workspace, gh github.Client, outlines []workspace.RepoOutline) statusData {
	var mu sync.Mutex
	var wg sync.WaitGroup

	result := statusData{
		statuses:  make([][]workspace.WorktreeStatus, len(outlines)),
		prsByRepo: make(map[string]map[string]*github.PR),
	}

	for i, o := range outlines {
		if o.Err != nil {
			continue
		}

		result.statuses[i] = make([]workspace.WorktreeStatus, len(o.Worktrees))

		for j, wtName := range o.Worktrees {
			wg.Add(1)
			go func(ri, wi int, repoName, wtName string) {
				defer wg.Done()
				wtPath := filepath.Join(ws.RepoDir(repoName), wtName)
				st := workspace.QueryWorktreeStatus(wtPath)
				mu.Lock()
				result.statuses[ri][wi] = st
				mu.Unlock()
			}(i, j, o.Name, wtName)
		}

		wg.Add(1)
		go func(repoName string) {
			defer wg.Done()
			prs, err := gh.PRsForRepo(ws.Org, repoName)
			if err != nil {
				return
			}
			m := make(map[string]*github.PR, len(prs))
			for k := range prs {
				m[prs[k].HeadRefName] = &prs[k]
			}
			mu.Lock()
			result.prsByRepo[repoName] = m
			mu.Unlock()
		}(o.Name)
	}

	wg.Wait()
	return result
}
