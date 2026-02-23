package cli

import (
	"slices"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
)

// --- row types ---

type rowKind int

const (
	rowRepoHeader rowKind = iota
	rowWorktree
	rowGhostPR
)

type mcRow struct {
	kind      rowKind
	repo      string
	wt        string
	branch    string
	dirty     bool
	ahead     int
	behind    int
	loaded    bool
	pr        *github.PR
	isBoarded bool
	merged    bool
}

// --- detail tier 2 data ---

type detailData struct {
	commits    []string
	diffStat   string
	stashCount int
	prTitle    string
	prBody     string
	checks     []github.CheckRun
	loaded     bool
}

// --- filter flags ---

type filterFlag uint8

const (
	filterLocal     filterFlag = 1 << iota // hide ghost PRs
	filterMine                             // only my PRs
	filterReviewReq                        // only REVIEW_REQUIRED
	filterDirty                            // only dirty or ahead
)

// --- model ---

type mcModel struct {
	ws     *workspace.Workspace
	cwd    string
	rows   []mcRow
	cursor int
	width  int
	height int

	listVP   viewport.Model
	detailVP viewport.Model

	wtTotal  int
	wtDone   int
	prTotal  int
	prDone   int
	prErrors int

	repos []mcRepoData

	detail    detailData
	detailFor int
	detailSeq int

	jumpPath      string
	confirmIdx    int
	actionSpinner int

	showHelp bool

	filterInput  textinput.Model
	filterActive bool

	paletteActive bool
	paletteInput  textinput.Model
	paletteCursor int

	activeFilters filterFlag
	ghUser        string
}

type mcRepoData struct {
	name      string
	boarded   []string
	worktrees []string
	err       error
	prs       map[string]*github.PR
	prsLoaded bool
}

// --- message types ---

type mcWtStatusMsg struct {
	repo string
	wt   workspace.WorktreeStatus
}

type mcPRsMsg struct {
	repo string
	prs  []github.PR
	err  error
}

type mcDetailTickMsg struct {
	seq int
}

type mcDetailDataMsg struct {
	rowIdx int
	data   detailData
}

type mcWorktreeCreatedMsg struct {
	rowIdx int
	repo   string
	branch string
	err    error
}

type mcWorktreeDeletedMsg struct {
	rowIdx int
	repo   string
	branch string
	err    error
}

type mcGhUserMsg struct {
	login string
}

type mcMergedMsg struct {
	repo     string
	branches map[string]bool
}

// --- constructor ---

func newMCModel(ws *workspace.Workspace, cwd string) mcModel {
	outlines := ws.StatusOutline(true)
	repos := make([]mcRepoData, len(outlines))
	var rows []mcRow
	wtTotal := 0
	prTotal := 0

	for i, o := range outlines {
		repos[i] = mcRepoData{
			name:      o.Name,
			boarded:   o.Boarded,
			worktrees: o.Worktrees,
			err:       o.Err,
		}

		rows = append(rows, mcRow{kind: rowRepoHeader, repo: o.Name})

		if o.Err != nil {
			continue
		}

		for _, wt := range o.Worktrees {
			rows = append(rows, mcRow{
				kind:      rowWorktree,
				repo:      o.Name,
				wt:        wt,
				isBoarded: slices.Contains(o.Boarded, wt),
			})
			wtTotal++
		}
		prTotal++
	}

	// Place cursor on the worktree matching the user's PWD, or fall back to first worktree row.
	cursor := 0
	detectedRepo, detectedWt, detected := workspace.DetectRepo(ws.Root, cwd)
	if detected {
		for i, r := range rows {
			if r.kind == rowWorktree && r.repo == detectedRepo && r.wt == detectedWt {
				cursor = i
				break
			}
		}
	}
	if cursor == 0 {
		for i, r := range rows {
			if r.kind != rowRepoHeader {
				cursor = i
				break
			}
		}
	}

	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 100

	pi := textinput.New()
	pi.Prompt = ""
	pi.CharLimit = 100

	m := mcModel{
		ws:            ws,
		cwd:           cwd,
		rows:          rows,
		cursor:        cursor,
		repos:         repos,
		wtTotal:       wtTotal,
		prTotal:       prTotal,
		detailFor:     -1,
		confirmIdx:    -1,
		actionSpinner: -1,
		filterInput:   ti,
		paletteInput:  pi,
	}

	// Load cached PR data for instant first render
	cacheDir := github.CacheDir()
	for _, repo := range repos {
		if repo.err != nil {
			continue
		}
		prs, _ := github.ReadPRCache(cacheDir, ws.Org, repo.name)
		if len(prs) > 0 {
			m.processPRs(repo.name, prs)
		}
	}

	return m
}
