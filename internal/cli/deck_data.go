package cli

import (
	"path/filepath"
	"slices"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/workspace"
)

const deckLogTail = 8

// deckMission is a mission plus everything the deck shows about it.
type deckMission struct {
	missionView
	Brief  string
	Events []mission.Event
	Git    map[mission.Capsule]workspace.WorktreeStatus
}

// deckCapsule is a capsule that belongs to no mission.
type deckCapsule struct {
	Repo, Name, Path string
	Git              workspace.WorktreeStatus
	PR               *github.PR
}

// deckData is one snapshot of everything the deck renders.
type deckData struct {
	Missions []deckMission
	Loose    []deckCapsule
}

// deckStateOrder is the sidebar order: live work first, finished work last.
var deckStateOrder = map[mission.State]int{
	mission.Briefing: -1, // needs you: answer the briefing crew's questions
	mission.Active:   0,
	mission.Review:   1,
	mission.Draft:    2,
	mission.Parked:   3,
	mission.Landed:   4,
	mission.Archived: 5,
}

// loadDeckData reads missions and git state. PRs come from the given index,
// so callers decide how often to hit GitHub.
func loadDeckData(ws *workspace.Workspace, prs prIndex) (deckData, error) {
	store := mission.Store{Root: ws.Root}
	missions, err := store.List()
	if err != nil {
		return deckData{}, err
	}

	var d deckData
	views := buildMissionViewsFrom(ws, prs, missions)
	owned := map[mission.Capsule]bool{}
	for _, v := range views {
		dm := deckMission{missionView: v, Git: map[mission.Capsule]workspace.WorktreeStatus{}}
		dm.Brief, _ = store.ReadBrief(v.M.Slug)
		dm.Events, _ = store.ReadLog(v.M.Slug, deckLogTail)
		for _, c := range v.Capsules {
			owned[mission.Capsule{Repo: c.Repo, Name: c.Name}] = true
		}
		d.Missions = append(d.Missions, dm)
	}
	slices.SortStableFunc(d.Missions, func(a, b deckMission) int {
		return deckStateOrder[a.State] - deckStateOrder[b.State]
	})

	for _, repo := range ws.ListedRepoNames() {
		names, err := workspace.ListWorktrees(ws.RepoDir(repo))
		if err != nil {
			continue
		}
		for _, name := range names {
			if owned[mission.Capsule{Repo: repo, Name: name}] {
				continue
			}
			d.Loose = append(d.Loose, deckCapsule{Repo: repo, Name: name, Path: filepath.Join(ws.RepoDir(repo), name)})
		}
	}

	// Git status is the slow part: one `git status` per capsule, in parallel.
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := range d.Missions {
		for _, c := range d.Missions[i].Capsules {
			if !c.Exists {
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				st := workspace.QueryWorktreeStatus(c.Path)
				mu.Lock()
				d.Missions[i].Git[mission.Capsule{Repo: c.Repo, Name: c.Name}] = st
				mu.Unlock()
			}()
		}
	}
	for i := range d.Loose {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Loose[i].Git = workspace.QueryWorktreeStatus(d.Loose[i].Path)
		}()
	}
	wg.Wait()

	for i, c := range d.Loose {
		r := prs[c.Repo]
		d.Loose[i].PR = lookupPR(r.open, c.Git.Branch, c.Name)
	}
	return d, nil
}

// deckRepos is every repo the deck may need PRs for.
func deckRepos(ws *workspace.Workspace) []string {
	return ws.ListedRepoNames()
}

type deckLoadedMsg struct {
	data deckData
	err  error
}

type deckPRsMsg struct{ prs prIndex }

func loadDeckCmd(ws *workspace.Workspace, prs prIndex) tea.Cmd {
	return func() tea.Msg {
		d, err := loadDeckData(ws, prs)
		return deckLoadedMsg{data: d, err: err}
	}
}

func fetchDeckPRsCmd(ws *workspace.Workspace, gh github.Client) tea.Cmd {
	if gh == nil {
		return nil
	}
	return func() tea.Msg {
		return deckPRsMsg{prs: fetchPRIndex(ws, gh, deckRepos(ws))}
	}
}
