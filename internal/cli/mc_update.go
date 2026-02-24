package cli

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/brudil/workspace/internal/github"
	tmuxpkg "github.com/brudil/workspace/internal/tmux"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	gh "github.com/cli/go-gh/v2"
)

// --- Init ---

func (m mcModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, repo := range m.repos {
		if repo.err != nil {
			continue
		}
		for _, wt := range repo.worktrees {
			cmds = append(cmds, m.queryWorktree(repo.name, wt))
		}
		cmds = append(cmds, m.queryRepoPRs(repo.name))
		cmds = append(cmds, m.queryMergedBranches(repo.name))
	}
	cmds = append(cmds, m.scheduleDetailFetch())
	cmds = append(cmds, fetchGhUser())
	cmds = append(cmds, tea.SetWindowTitle("Mission Control"))
	if tmuxpkg.InTmux() {
		cmds = append(cmds, queryTmuxWindows())
	}
	return tea.Batch(cmds...)
}

func (m mcModel) queryWorktree(repo, wt string) tea.Cmd {
	repoDir := m.ws.RepoDir(repo)
	wtPath := filepath.Join(repoDir, wt)
	return func() tea.Msg {
		status := workspace.QueryWorktreeStatus(wtPath)
		return mcWtStatusMsg{repo: repo, wt: status}
	}
}

func (m mcModel) queryRepoPRs(repoName string) tea.Cmd {
	org := m.ws.Org
	gh := m.gh
	return func() tea.Msg {
		prs, err := gh.PRsForRepo(org, repoName)
		if err == nil {
			github.WritePRCache(github.CacheDir(), org, repoName, prs)
		}
		return mcPRsMsg{repo: repoName, prs: prs, err: err}
	}
}

func (m mcModel) queryMergedBranches(repoName string) tea.Cmd {
	bareDir := m.ws.BareDir(repoName)
	defaultBranch := m.ws.DefaultBranch
	return func() tea.Msg {
		merged := workspace.GitMergedBranches(bareDir, defaultBranch)
		set := make(map[string]bool, len(merged))
		for _, b := range merged {
			set[b] = true
		}
		return mcMergedMsg{repo: repoName, branches: set}
	}
}

func fetchGhUser() tea.Cmd {
	return func() tea.Msg {
		cacheDir := github.CacheDir()
		if login := github.ReadUserCache(cacheDir); login != "" {
			return mcGhUserMsg{login: login}
		}
		stdOut, _, err := gh.Exec("api", "user", "-q", ".login")
		if err != nil {
			return mcGhUserMsg{}
		}
		login := strings.TrimSpace(stdOut.String())
		if login != "" {
			github.WriteUserCache(cacheDir, login)
		}
		return mcGhUserMsg{login: login}
	}
}

// --- Update ---

func (m mcModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	prevCursor := m.cursor
	m, cmd := m.handleMsg(msg)

	// Centralized detail scheduling: whenever cursor moves to a new row,
	// clear stale detail and schedule a debounced fetch.
	if m.cursor != prevCursor && m.cursor != m.detailFor {
		m.detail = detailData{}
		m.detailFor = -1
		m.detailSeq++
		m.detailVP.GotoTop()
		cmd = tea.Batch(cmd, m.scheduleDetailFetch())
	}

	return m, cmd
}

func (m mcModel) handleMsg(msg tea.Msg) (mcModel, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		listWidth := m.width * 2 / 5
		detailWidth := m.width - listWidth - 1
		footerHeight := 1
		if m.paletteActive {
			footerHeight = m.paletteHeight()
		}
		contentHeight := m.height - 3 - footerHeight // header(1) + border(1) + footer newline(1)
		m.listVP = viewport.New(listWidth, contentHeight)
		m.detailVP = viewport.New(detailWidth, contentHeight)
		return m, nil

	case mcWtStatusMsg:
		for i := range m.rows {
			if m.rows[i].kind == rowWorktree && m.rows[i].repo == msg.repo && m.rows[i].wt == msg.wt.Name {
				m.rows[i].branch = msg.wt.Branch
				m.rows[i].dirty = msg.wt.Dirty
				m.rows[i].ahead = msg.wt.Ahead
				m.rows[i].behind = msg.wt.Behind
				m.rows[i].loaded = true
				m.wtDone++
				m.matchWorktreePR(i)
				break
			}
		}
		// Update branch cache so next launch can match PRs immediately.
		branches := make(map[string]string)
		for _, row := range m.rows {
			if row.kind == rowWorktree && row.repo == msg.repo && row.branch != "" {
				branches[row.wt] = row.branch
			}
		}
		if len(branches) > 0 {
			github.WriteBranchCache(github.CacheDir(), m.ws.Org, msg.repo, branches)
		}
		if m.activeFilters != 0 {
			m.ensureCursorOnVisible()
		}
		return m, nil

	case mcPRsMsg:
		m.prDone++
		for i := range m.repos {
			if m.repos[i].name != msg.repo {
				continue
			}
			if msg.err != nil {
				m.prErrors++
				m.repos[i].prsLoaded = true
				break
			}
			cursorRepo, cursorBranch := m.clearRepoPRs(msg.repo)
			m.processPRs(msg.repo, msg.prs)
			m.repos[i].prsLoaded = true
			m.restoreCursor(cursorRepo, cursorBranch)
			break
		}
		return m, nil

	case mcDetailTickMsg:
		if msg.seq != m.detailSeq {
			return m, nil
		}
		return m, m.fetchDetail(m.cursor)

	case mcDetailDataMsg:
		if msg.rowIdx != m.cursor {
			return m, nil
		}
		m.detail = msg.data
		m.detailFor = msg.rowIdx
		return m, nil

	case mcMergedMsg:
		for i := range m.rows {
			if m.rows[i].repo == msg.repo && m.rows[i].kind == rowWorktree && msg.branches[m.rows[i].branch] {
				m.rows[i].merged = true
			}
		}
		return m, nil

	case mcGhUserMsg:
		m.ghUser = msg.login
		if m.activeFilters&filterMine != 0 {
			m.ensureCursorOnVisible()
		}
		return m, nil

	case mcTmuxWindowsMsg:
		for i := range m.rows {
			if m.rows[i].kind == rowRepoHeader {
				continue
			}
			name := tmuxpkg.WindowName(m.ws.DisplayNameFor(m.rows[i].repo), m.rows[i].wt)
			m.rows[i].live = msg.windows[name] != ""
		}
		return m, nil

	case mcWorktreeCreatedMsg:
		m.actionSpinner = -1
		if msg.err != nil {
			return m, nil
		}
		for i := range m.rows {
			if i == msg.rowIdx && m.rows[i].kind == rowGhostPR {
				m.rows[i].kind = rowWorktree
				m.rows[i].wt = msg.capsule
				return m, m.queryWorktree(msg.repo, msg.capsule)
			}
		}
		return m, nil

	case mcWorktreeDeletedMsg:
		m.actionSpinner = -1
		if msg.err != nil {
			return m, nil
		}
		for i := range m.rows {
			if i == msg.rowIdx {
				m.rows = append(m.rows[:i], m.rows[i+1:]...)
				if m.cursor >= len(m.rows) {
					m.moveCursor(-1)
				} else if m.rows[m.cursor].kind == rowRepoHeader {
					m.moveCursor(1)
				}
				break
			}
		}
		return m, nil

	case mcFetchMsg:
		if msg.err != nil {
			return m, nil
		}
		var cmds []tea.Cmd
		for _, repo := range m.repos {
			if repo.name != msg.repo {
				continue
			}
			for _, wt := range repo.worktrees {
				cmds = append(cmds, m.queryWorktree(repo.name, wt))
			}
			cmds = append(cmds, m.queryRepoPRs(repo.name))
			cmds = append(cmds, m.queryMergedBranches(repo.name))
			break
		}
		return m, tea.Batch(cmds...)

	case mcFetchAllMsg:
		if msg.err != nil {
			return m, nil
		}
		return m.rebuildModel()

	case tea.MouseMsg:
		listWidth := m.width * 2 / 5
		if msg.X < listWidth {
			// Left click on a row moves the cursor
			if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
				headerLines := 2 // header + border
				contentLine := msg.Y - headerLines + m.listVP.YOffset
				if rowIdx := m.lineToRowIndex(contentLine); rowIdx >= 0 {
					m.cursor = rowIdx
					return m, nil
				}
			}
			m.syncListContent()
			var cmd tea.Cmd
			m.listVP, cmd = m.listVP.Update(msg)
			return m, cmd
		}
		m.syncDetailContent()
		var cmd tea.Cmd
		m.detailVP, cmd = m.detailVP.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Route cursor blink messages to textinput when filter or palette is active
	if m.filterActive {
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		return m, cmd
	}
	if m.paletteActive {
		var cmd tea.Cmd
		m.paletteInput, cmd = m.paletteInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *mcModel) clearRepoPRs(repoName string) (cursorRepo, cursorBranch string) {
	// Save cursor identity
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		cur := m.rows[m.cursor]
		cursorRepo = cur.repo
		cursorBranch = cur.branch
		if cursorBranch == "" {
			cursorBranch = cur.wt
		}
	}

	// Remove ghost rows for the repo (backwards to keep indices stable)
	for i := len(m.rows) - 1; i >= 0; i-- {
		if m.rows[i].kind == rowGhostPR && m.rows[i].repo == repoName {
			m.rows = append(m.rows[:i], m.rows[i+1:]...)
			if m.cursor > i {
				m.cursor--
			} else if m.cursor == i && m.cursor >= len(m.rows) {
				m.cursor = len(m.rows) - 1
			}
		}
	}

	// Clear PR pointers from worktree rows for the repo
	for i := range m.rows {
		if m.rows[i].kind == rowWorktree && m.rows[i].repo == repoName {
			m.rows[i].pr = nil
		}
	}

	return cursorRepo, cursorBranch
}

func (m *mcModel) restoreCursor(repo, branch string) {
	if repo == "" && branch == "" {
		m.ensureCursorOnVisible()
		return
	}
	for i, row := range m.rows {
		if row.repo == repo && (row.branch == branch || row.wt == branch) && m.isRowVisible(i) {
			m.cursor = i
			return
		}
	}
	m.ensureCursorOnVisible()
}

func (m *mcModel) processPRs(repoName string, prs []github.PR) {
	var repoIdx int
	for i := range m.repos {
		if m.repos[i].name == repoName {
			repoIdx = i
			break
		}
	}

	m.repos[repoIdx].prs = make(map[string]*github.PR, len(prs))
	for j := range prs {
		m.repos[repoIdx].prs[prs[j].HeadRefName] = &prs[j]
	}

	matched := make(map[string]bool)
	for j := range m.rows {
		if m.rows[j].kind != rowWorktree || m.rows[j].repo != repoName {
			continue
		}
		if pr, ok := m.repos[repoIdx].prs[m.rows[j].branch]; ok && m.rows[j].branch != "" {
			m.rows[j].pr = pr
			matched[m.rows[j].branch] = true
		} else if pr, ok := m.repos[repoIdx].prs[m.rows[j].wt]; ok {
			m.rows[j].pr = pr
			matched[m.rows[j].wt] = true
		}
	}

	var ghosts []mcRow
	for _, pr := range prs {
		if matched[pr.HeadRefName] {
			continue
		}
		p := pr
		ghosts = append(ghosts, mcRow{
			kind:   rowGhostPR,
			repo:   repoName,
			branch: pr.HeadRefName,
			pr:     &p,
			loaded: true,
		})
	}
	if len(ghosts) > 0 {
		m.insertGhostRows(repoName, ghosts)
	}
}

// matchWorktreePR links a worktree row to a PR once its branch is known,
// removing any ghost row that represented the same PR.
func (m *mcModel) matchWorktreePR(rowIdx int) {
	row := &m.rows[rowIdx]
	if row.pr != nil || row.branch == "" {
		return
	}
	var repoIdx int
	for i := range m.repos {
		if m.repos[i].name == row.repo {
			repoIdx = i
			break
		}
	}
	pr, ok := m.repos[repoIdx].prs[row.branch]
	if !ok {
		return
	}
	row.pr = pr

	// Remove the ghost row for this PR, if one exists.
	for i := len(m.rows) - 1; i >= 0; i-- {
		if m.rows[i].kind == rowGhostPR && m.rows[i].repo == row.repo && m.rows[i].branch == row.branch {
			m.rows = append(m.rows[:i], m.rows[i+1:]...)
			if m.cursor > i {
				m.cursor--
			} else if m.cursor == i && m.cursor >= len(m.rows) {
				m.cursor = len(m.rows) - 1
			}
			break
		}
	}
}

func (m *mcModel) insertGhostRows(repo string, ghosts []mcRow) {
	insertAt := -1
	for i := len(m.rows) - 1; i >= 0; i-- {
		if m.rows[i].repo == repo {
			insertAt = i + 1
			break
		}
	}
	if insertAt < 0 {
		return
	}

	newRows := make([]mcRow, 0, len(m.rows)+len(ghosts))
	newRows = append(newRows, m.rows[:insertAt]...)
	newRows = append(newRows, ghosts...)
	newRows = append(newRows, m.rows[insertAt:]...)
	m.rows = newRows

	if m.cursor >= insertAt {
		m.cursor += len(ghosts)
	}
}

func queryTmuxWindows() tea.Cmd {
	return func() tea.Msg {
		return mcTmuxWindowsMsg{windows: tmuxpkg.ListWindows()}
	}
}

// --- detail fetching with debounce ---

func (m mcModel) scheduleDetailFetch() tea.Cmd {
	seq := m.detailSeq
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return mcDetailTickMsg{seq: seq}
	})
}

func (m mcModel) fetchDetail(rowIdx int) tea.Cmd {
	row := m.rows[rowIdx]
	ws := m.ws
	gh := m.gh
	return func() tea.Msg {
		var d detailData
		if row.kind == rowWorktree && row.wt == workspace.GroundDir {
			prs, err := gh.MergedPRsForRepo(ws.Org, row.repo)
			if err == nil {
				if len(prs) > 8 {
					prs = prs[:8]
				}
				d.landings = prs
			}
			runs, err := gh.WorkflowRuns(ws.Org, row.repo, ws.DefaultBranch, 8)
			if err == nil {
				d.actions = runs
			}
		} else if row.kind == rowWorktree {
			wtPath := filepath.Join(ws.RepoDir(row.repo), row.wt)
			d.commits = workspace.GitRecentCommits(wtPath, 4, ws.DefaultBranch)
			d.diffStat = workspace.GitDiffStat(wtPath)
			d.stashCount = workspace.GitStashCount(wtPath)
		}
		if row.pr != nil {
			pr, err := gh.PRDetail(ws.Org, row.repo, row.pr.Number)
			if err == nil {
				d.prTitle = pr.Title
				d.prBody = pr.Body
				d.checks = pr.Checks
				if row.kind == rowGhostPR {
					d.commits = pr.Commits
					if len(d.commits) > 4 {
						d.commits = d.commits[len(d.commits)-4:]
					}
				}
			}
		}
		d.loaded = true
		return mcDetailDataMsg{rowIdx: rowIdx, data: d}
	}
}
