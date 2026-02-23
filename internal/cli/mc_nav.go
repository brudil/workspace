package cli

import (
	"path/filepath"

	"github.com/brudil/workspace/internal/workspace"
	"github.com/charmbracelet/lipgloss"
)

func (m mcModel) isRowVisible(i int) bool {
	row := m.rows[i]
	if row.kind == rowRepoHeader {
		return false
	}

	// Text filter
	filter := m.filterInput.Value()
	if filter != "" {
		branch := row.branch
		if branch == "" {
			branch = row.wt
		}
		if !workspace.FuzzyMatch(filter, branch) {
			return false
		}
	}

	// Preset filters (all active flags must pass)
	if m.activeFilters&filterLocal != 0 && row.kind != rowWorktree {
		return false
	}
	if m.activeFilters&filterMine != 0 && m.ghUser != "" {
		if row.pr == nil || row.pr.Author != m.ghUser {
			return false
		}
	}
	if m.activeFilters&filterReviewReq != 0 {
		if row.pr == nil || row.pr.ReviewDecision != "REVIEW_REQUIRED" {
			return false
		}
	}
	if m.activeFilters&filterDirty != 0 {
		if !row.dirty && row.ahead <= 0 {
			return false
		}
	}

	return true
}

func (m mcModel) filteredRowCount() (visible, total int) {
	for i, row := range m.rows {
		if row.kind == rowRepoHeader {
			continue
		}
		total++
		if m.isRowVisible(i) {
			visible++
		}
	}
	return
}

func (m *mcModel) moveCursor(delta int) {
	newCursor := m.cursor
	for {
		newCursor += delta
		if newCursor < 0 || newCursor >= len(m.rows) {
			return
		}
		if m.isRowVisible(newCursor) {
			m.cursor = newCursor
			return
		}
	}
}

func (m *mcModel) ensureCursorOnVisible() {
	if m.cursor >= 0 && m.cursor < len(m.rows) && m.isRowVisible(m.cursor) {
		return
	}
	for i := range m.rows {
		if m.isRowVisible(i) {
			m.cursor = i
			return
		}
	}
}

func (m *mcModel) ensureCursorVisible() {
	// Count rendered lines up to the cursor, matching renderList's output
	lineIdx := 0
	repoHasVisible := false

	for i, row := range m.rows {
		if row.kind == rowRepoHeader {
			// Check if this repo group has any visible children
			repoHasVisible = false
			for j := i + 1; j < len(m.rows) && m.rows[j].kind != rowRepoHeader; j++ {
				if m.isRowVisible(j) {
					repoHasVisible = true
					break
				}
			}
			if repoHasVisible {
				if i == m.cursor {
					break
				}
				lineIdx += 2 // header + rule line
			}
			continue
		}

		if !m.isRowVisible(i) {
			continue
		}

		if i == m.cursor {
			break
		}
		lineIdx++
	}

	if lineIdx < m.listVP.YOffset {
		m.listVP.SetYOffset(lineIdx)
	} else if lineIdx >= m.listVP.YOffset+m.listVP.Height {
		m.listVP.SetYOffset(lineIdx - m.listVP.Height + 1)
	}
}

func (m mcModel) repoColorFor(repoName string) lipgloss.Color {
	if c, ok := m.ws.RepoColors[repoName]; ok {
		return lipgloss.Color(c)
	}
	paletteIdx := 0
	for _, row := range m.rows {
		if row.kind != rowRepoHeader {
			continue
		}
		if row.repo == repoName {
			return repoPalette[paletteIdx%len(repoPalette)]
		}
		if _, ok := m.ws.RepoColors[row.repo]; !ok {
			paletteIdx++
		}
	}
	return repoPalette[0]
}

func (m *mcModel) syncDetailContent() {
	if m.width == 0 {
		return
	}
	detailWidth := m.width - m.width*2/5 - 1
	m.detailVP.SetContent(m.renderDetail(detailWidth))
}

func (m mcModel) selectedWorktreePath() string {
	row := m.rows[m.cursor]
	if row.kind != rowWorktree {
		return ""
	}
	return filepath.Join(m.ws.RepoDir(row.repo), row.wt)
}
