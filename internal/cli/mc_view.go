package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/ui"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// --- View ---

func (m mcModel) renderHeader() string {
	title := lipgloss.NewStyle().Bold(true).Render(m.ws.Title()) +
		" " + ui.Dim.Render("Mission Control")

	// Filter tags
	tagStyle := lipgloss.NewStyle().Background(lipgloss.Color("237")).Foreground(lipgloss.Color("252"))
	var tags []string
	if m.activeFilters&filterLocal != 0 {
		tags = append(tags, tagStyle.Render(" local "))
	}
	if m.activeFilters&filterMine != 0 {
		tags = append(tags, tagStyle.Render(" mine "))
	}
	if m.activeFilters&filterReviewReq != 0 {
		tags = append(tags, tagStyle.Render(" review "))
	}
	if m.activeFilters&filterDirty != 0 {
		tags = append(tags, tagStyle.Render(" dirty "))
	}
	tagStr := ""
	if len(tags) > 0 {
		tagStr = " " + strings.Join(tags, " ")
	}

	// Row count when filters are active
	countStr := ""
	if m.activeFilters != 0 || m.filterInput.Value() != "" {
		visible, total := m.filteredRowCount()
		countStr = " " + ui.Dim.Render(fmt.Sprintf("(%d/%d)", visible, total))
	}

	// Text filter
	var right string
	if m.filterActive {
		right = ui.Dim.Render("/") + " " + m.filterInput.View()
	} else if m.filterInput.Value() != "" {
		right = ui.Dim.Render("/ " + m.filterInput.Value())
	} else {
		right = ui.Dim.Render("/ to filter")
	}

	titleWidth := lipgloss.Width(title)
	tagWidth := lipgloss.Width(tagStr)
	countWidth := lipgloss.Width(countStr)
	rightWidth := lipgloss.Width(right)
	padding := max(m.width-titleWidth-tagWidth-countWidth-rightWidth-2, 2)

	header := " " + title + tagStr + countStr + strings.Repeat(" ", padding) + right + " "
	border := ui.Dim.Render(strings.Repeat("─", m.width))
	return header + "\n" + border
}

func (m mcModel) View() string {
	if m.width == 0 {
		return "loading..."
	}

	if len(m.rows) == 0 {
		return "No repos configured. Run ws setup first.\n"
	}

	if m.showHelp {
		return m.renderHelpOverlay()
	}

	header := m.renderHeader()

	listWidth := m.width * 2 / 5
	detailWidth := m.width - listWidth - 1

	// Dynamic footer height: palette takes multiple lines, help bar takes 1.
	footerHeight := 1 // help bar
	if m.paletteActive {
		footerHeight = m.paletteHeight()
	}
	contentHeight := m.height - 3 - footerHeight // header(1) + border(1) + footer newline(1)

	if m.listVP.Height != contentHeight {
		m.listVP = viewport.New(listWidth, contentHeight)
		m.detailVP = viewport.New(detailWidth, contentHeight)
	}

	leftContent := m.renderList(listWidth)
	m.listVP.SetContent(leftContent)

	rightContent := m.renderDetail(detailWidth)
	savedYOffset := m.detailVP.YOffset
	m.detailVP.SetContent(rightContent)
	m.detailVP.SetYOffset(savedYOffset)

	divider := lipgloss.NewStyle().
		Foreground(lipgloss.Color("238")).
		Render(strings.Repeat("│\n", contentHeight))

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		m.listVP.View(),
		divider,
		m.detailVP.View(),
	)

	var footer string
	if m.paletteActive {
		footer = m.renderPalette()
	} else {
		footer = m.renderHelpBar()
	}

	return header + "\n" + body + "\n" + footer
}

// --- left panel ---

func (m mcModel) renderList(width int) string {
	var b strings.Builder

	type repoGroup struct {
		name    string
		color   lipgloss.Color
		rows    []mcRow
		rowIdxs []int
	}

	var groups []repoGroup
	var current *repoGroup
	paletteIdx := 0

	for i, row := range m.rows {
		if row.kind == rowRepoHeader {
			if current != nil {
				groups = append(groups, *current)
			}
			color := repoPalette[paletteIdx%len(repoPalette)]
			if c, ok := m.ws.RepoColors[row.repo]; ok {
				color = lipgloss.Color(c)
			} else {
				paletteIdx++
			}
			current = &repoGroup{
				name:  row.repo,
				color: color,
			}
		} else if current != nil {
			current.rows = append(current.rows, row)
			current.rowIdxs = append(current.rowIdxs, i)
		}
	}
	if current != nil {
		groups = append(groups, *current)
	}

	for _, g := range groups {
		// Check if any children are visible
		hasVisible := slices.ContainsFunc(g.rowIdxs, m.isRowVisible)
		if !hasVisible {
			continue
		}

		nameStyle := lipgloss.NewStyle().Bold(true).Foreground(g.color)
		header := nameStyle.Render(m.ws.DisplayNameFor(g.name))
		if _, ok := m.ws.DisplayNames[g.name]; ok {
			header += " " + ui.Dim.Render("("+g.name+")")
		}

		bStyle := lipgloss.NewStyle().Foreground(g.color)
		b.WriteString(bStyle.Render("│") + " " + header + "\n")
		b.WriteString(bStyle.Render("├─") + strings.Repeat("─", width-3) + "\n")

		for j, row := range g.rows {
			globalIdx := g.rowIdxs[j]
			if !m.isRowVisible(globalIdx) {
				continue
			}
			selected := globalIdx == m.cursor

			line := m.renderRow(row, selected, globalIdx)
			prefix := bStyle.Render("│") + " "
			b.WriteString(prefix + line + "\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m mcModel) renderRow(row mcRow, selected bool, globalIdx int) string {
	highlightStyle := lipgloss.NewStyle().Background(lipgloss.Color("236"))

	var line string
	switch row.kind {
	case rowWorktree:
		line = m.renderWorktreeRow(row)
	case rowGhostPR:
		line = m.renderGhostRow(row)
	}

	if m.actionSpinner == globalIdx {
		line += "  " + ui.Orange.Render("⟳")
	}

	if selected {
		line = highlightStyle.Render(line)
	}
	return line
}

func (m mcModel) renderWorktreeRow(row mcRow) string {
	if m.confirmIdx >= 0 && m.confirmIdx < len(m.rows) && m.rows[m.confirmIdx].wt == row.wt && m.rows[m.confirmIdx].repo == row.repo {
		return ui.Red.Render("delete " + row.wt + "? y/n")
	}

	var buf strings.Builder

	if row.isBoarded {
		buf.WriteString(ui.Blue.Render(ui.BoardedMarker) + " ")
	} else {
		buf.WriteString("  ")
	}

	buf.WriteString(row.wt)

	if !row.loaded {
		buf.WriteString("  " + ui.Dim.Render("…"))
		return buf.String()
	}

	buf.WriteString("  ")

	if row.dirty {
		buf.WriteString(ui.Orange.Render("●") + " ")
	}

	if row.merged {
		buf.WriteString(ui.TagGreen.Render("landed") + " ")
	}

	if row.branch != "" && row.branch != row.wt {
		buf.WriteString(ui.Dim.Render("→ "+row.branch) + " ")
	}

	if ab := formatAheadBehind(row.ahead, row.behind); ab != "" {
		buf.WriteString(ui.Dim.Render(ab) + " ")
	}

	if row.pr != nil {
		buf.WriteString(formatPRInfo(row.pr))
	}

	return strings.TrimRight(buf.String(), " ")
}

func (m mcModel) renderGhostRow(row mcRow) string {
	var buf strings.Builder
	buf.WriteString("  ")
	buf.WriteString(ui.Dim.Render(row.branch))
	if row.pr != nil {
		buf.WriteString("  " + formatPRInfo(row.pr))
	}
	return buf.String()
}

func uintPtr(v uint) *uint { return &v }

func renderPRStatusParts(pr *github.PR) []string {
	var parts []string
	switch pr.ReviewDecision {
	case "CHANGES_REQUESTED":
		parts = append(parts, ui.Red.Render("changes req"))
	case "REVIEW_REQUIRED":
		parts = append(parts, ui.Orange.Render("review needed"))
	}
	return parts
}

// --- right panel ---

func (m mcModel) renderDetail(width int) string {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	row := m.rows[m.cursor]
	if row.kind == rowRepoHeader {
		return ""
	}

	var b strings.Builder
	indent := "  "

	// --- header line: branch/wt name + status tags ... repo name (right-aligned) ---
	branchName := row.wt
	if row.kind == rowGhostPR {
		branchName = row.branch
	}
	left := lipgloss.NewStyle().Bold(true).Render(branchName)

	// Status tags
	var tags []string
	if row.kind == rowWorktree {
		tags = append(tags, ui.TagDim.Render("docked"))
	}
	if row.kind == rowGhostPR {
		tags = append(tags, ui.TagOrange.Render("remote"))
	}
	if row.isBoarded {
		tags = append(tags, ui.TagBlue.Render("boarded"))
	}
	if row.kind == rowWorktree && row.loaded && !row.dirty {
		tags = append(tags, ui.TagGreen.Render("clean"))
	}
	if row.pr != nil && row.pr.ReviewDecision == "APPROVED" {
		tags = append(tags, ui.TagGreen.Render("cleared"))
	}
	if row.merged {
		tags = append(tags, ui.TagGreen.Render("landed"))
	}
	if len(tags) > 0 {
		left += " " + strings.Join(tags, " ")
	}

	repoColor := m.repoColorFor(row.repo)
	repoLabel := lipgloss.NewStyle().Foreground(repoColor).Render(m.ws.DisplayNameFor(row.repo))

	// Right-align repo name on the header line
	leftWidth := lipgloss.Width(left)
	repoWidth := lipgloss.Width(repoLabel)
	padding := max(
		// 4 = 2 indent + 2 margin
		width-4-leftWidth-repoWidth, 2)
	b.WriteString(indent + left + strings.Repeat(" ", padding) + repoLabel + "\n")

	// --- PR line (if applicable) ---
	if row.pr != nil {
		prNum := fmt.Sprintf("#%d", row.pr.Number)
		if row.pr.URL != "" {
			prNum = ui.Hyperlink(row.pr.URL, prNum)
		}

		// Use title from detail data if loaded, otherwise from PR object
		title := row.pr.Title
		if m.detailFor == m.cursor && m.detail.loaded && m.detail.prTitle != "" {
			title = m.detail.prTitle
		}

		prRight := ui.Dim.Render(prNum)
		prRightWidth := lipgloss.Width(prRight)
		// 4 = 2 indent + 2 border/padding from style
		titleWidth := max(width-4-prRightWidth-2, 10)

		prStyle := lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderLeft(true).
			BorderForeground(lipgloss.Color("238")).
			PaddingLeft(1).
			Width(titleWidth)

		prRendered := prStyle.Render(title)
		prRenderedWidth := lipgloss.Width(prRendered)
		prPadding := max(width-4-prRenderedWidth-prRightWidth, 1)
		b.WriteString(indent + prRendered + strings.Repeat(" ", prPadding) + prRight + "\n")
	}

	b.WriteString("\n")

	// --- git status (worktree only) ---
	if row.kind == rowWorktree && row.loaded {
		var gitParts []string
		if row.ahead > 0 {
			gitParts = append(gitParts, fmt.Sprintf("↑%d", row.ahead))
		}
		if row.behind > 0 {
			gitParts = append(gitParts, fmt.Sprintf("↓%d", row.behind))
		}
		b.WriteString(indent + strings.Join(gitParts, "  ") + "\n")

		// PR status indicators
		if row.pr != nil {
			if prParts := renderPRStatusParts(row.pr); len(prParts) > 0 {
				b.WriteString(indent + strings.Join(prParts, "  ") + "\n")
			}
		}
		b.WriteString("\n")
	}

	// --- ghost PR status ---
	if row.kind == rowGhostPR && row.pr != nil {
		if prParts := renderPRStatusParts(row.pr); len(prParts) > 0 {
			b.WriteString(indent + strings.Join(prParts, "  ") + "\n")
			b.WriteString("\n")
		}
	}

	if m.detailFor == m.cursor && m.detail.loaded {
		b.WriteString(m.renderDetailTier2(indent, width))
	} else if row.loaded || row.kind == rowGhostPR {
		b.WriteString(indent + ui.Dim.Render("loading details…") + "\n")
	}

	return b.String()
}

func (m mcModel) renderDetailTier2(indent string, width int) string {
	d := m.detail
	var b strings.Builder
	// Usable width for content after indent
	contentWidth := max(width-len(indent), 20)
	wrapStyle := lipgloss.NewStyle().Width(contentWidth)

	if len(d.commits) > 0 {
		b.WriteString(indent + lipgloss.NewStyle().Bold(true).Render("Recent Commits") + "\n")
		for _, c := range d.commits {
			b.WriteString(indent + ui.Dim.Render(c) + "\n")
		}
		b.WriteString("\n")
	}

	if d.diffStat != "" {
		b.WriteString(indent + lipgloss.NewStyle().Bold(true).Render("Changes") + "\n")
		wrapped := wrapStyle.Render(ui.Dim.Render(d.diffStat))
		for line := range strings.SplitSeq(wrapped, "\n") {
			b.WriteString(indent + line + "\n")
		}
		b.WriteString("\n")
	}

	if d.stashCount > 0 {
		b.WriteString(indent + fmt.Sprintf("Stash: %d entries\n", d.stashCount))
		b.WriteString("\n")
	}

	if len(d.checks) > 0 {
		allPassing := true
		var failures []github.CheckRun
		for _, c := range d.checks {
			switch c.Conclusion {
			case "SUCCESS":
			case "FAILURE", "ERROR", "CANCELLED", "TIMED_OUT":
				allPassing = false
				failures = append(failures, c)
			default:
				allPassing = false
			}
		}

		header := lipgloss.NewStyle().Bold(true).Render("Checks")
		passed := len(d.checks) - len(failures)
		if allPassing {
			header += " (" + ui.Green.Render("✓ all") + ")"
		} else if passed > 0 {
			header += " (" + ui.Green.Render(fmt.Sprintf("✓ %d/%d", passed, len(d.checks))) + ")"
		}
		b.WriteString(indent + header + "\n")

		for _, c := range failures {
			b.WriteString(indent + ui.Red.Render("✗") + " " + c.Name + "\n")
		}
		b.WriteString("\n")
	}

	if d.prBody != "" {
		b.WriteString(indent + lipgloss.NewStyle().Bold(true).Render("Description") + "\n")
		style := styles.DarkStyleConfig
		style.Document.Margin = uintPtr(0)
		renderer, err := glamour.NewTermRenderer(
			glamour.WithStyles(style),
			glamour.WithWordWrap(contentWidth),
		)
		if err == nil {
			rendered, err := renderer.Render(d.prBody)
			if err == nil {
				for line := range strings.SplitSeq(strings.TrimSpace(rendered), "\n") {
					b.WriteString(indent + line + "\n")
				}
			}
		}
		// Fallback if glamour fails
		if err != nil {
			wrapped := wrapStyle.Render(ui.Dim.Render(d.prBody))
			for line := range strings.SplitSeq(wrapped, "\n") {
				b.WriteString(indent + line + "\n")
			}
		}
	}

	return b.String()
}

// --- help ---

func (m mcModel) renderHelpOverlay() string {
	help := []struct{ key, desc string }{
		{"j/k ↑/↓", "Navigate worktrees"},
		{"J/K", "Scroll detail panel"},
		{"/", "Filter by branch name"},
		{"1", "Toggle filter: local worktrees only"},
		{"2", "Toggle filter: my PRs"},
		{"3", "Toggle filter: review requested"},
		{"4", "Toggle filter: dirty / needs push"},
		{"Esc", "Clear all filters"},
		{"g", "Go into worktree / open PR on GitHub"},
		{"o", "Open worktree in $EDITOR"},
		{"d", "Dock ghost PR / undock worktree"},
		{"b", "Toggle board/unboard"},
		{"r", "Refresh all data"},
		{":", "Command palette"},
		{"?", "Toggle this help"},
		{"q", "Quit"},
	}

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("  Mission Control") + "\n\n")
	for _, h := range help {
		key := lipgloss.NewStyle().Bold(true).Width(10).Render(h.key)
		b.WriteString("  " + key + " " + h.desc + "\n")
	}
	b.WriteString("\n" + ui.Dim.Render("  Press ? to close"))
	return b.String()
}

func (m mcModel) renderHelpBar() string {
	keys := []string{
		"j/k navigate",
		"J/K scroll detail",
		"/ filter",
	}

	var row mcRow
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		row = m.rows[m.cursor]
	}

	switch {
	case row.kind == rowWorktree && row.pr != nil:
		keys = append(keys, "g github", "o open", "b board", "d undock")
	case row.kind == rowGhostPR:
		keys = append(keys, "g github", "d dock")
	case row.kind == rowWorktree:
		keys = append(keys, "g go", "o open", "b board", "d undock")
	}

	keys = append(keys, "r refresh", ": commands", "? help", "q quit")
	return ui.Dim.Render(strings.Join(keys, "  "))
}
