package cli

import (
	"fmt"
	"strings"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/ui"
	"github.com/charmbracelet/lipgloss"
)

// wtColumns holds precomputed column widths for aligning worktree indicators.
type wtColumns struct {
	maxNameWidth int
	hasDirty     bool
}

func computeColumns(worktrees []worktreeView) wtColumns {
	cols := wtColumns{}
	for _, wt := range worktrees {
		if len(wt.name) > cols.maxNameWidth {
			cols.maxNameWidth = len(wt.name)
		}
		if wt.loaded && wt.dirty {
			cols.hasDirty = true
		}
	}
	return cols
}

func formatAheadBehind(ahead, behind int) string {
	if ahead == 0 && behind == 0 {
		return ""
	}
	var b strings.Builder
	if ahead > 0 {
		fmt.Fprintf(&b, "↑%d", ahead)
	}
	if behind > 0 {
		fmt.Fprintf(&b, "↓%d", behind)
	}
	return b.String()
}

func formatPRInfo(pr *github.PR) string {
	if pr == nil {
		return ""
	}
	var parts []string
	prNum := fmt.Sprintf("#%d", pr.Number)
	if pr.URL != "" {
		prNum = ui.Hyperlink(pr.URL, prNum)
	}
	parts = append(parts, ui.Dim.Render(prNum))

	switch pr.StatusRollup {
	case "success":
		parts = append(parts, ui.Green.Render("✓"))
	case "failure":
		parts = append(parts, ui.Red.Render("✗"))
	case "pending":
		parts = append(parts, ui.Orange.Render("◌"))
	}

	switch pr.ReviewDecision {
	case "APPROVED":
		parts = append(parts, ui.Green.Render("cleared"))
	case "CHANGES_REQUESTED":
		parts = append(parts, ui.Red.Render("changes req"))
	case "REVIEW_REQUIRED":
		parts = append(parts, ui.Orange.Render("review needed"))
	}

	return strings.Join(parts, " ")
}

func formatWorktreeLine(wt worktreeView, isBoarded bool, cols wtColumns, pr *github.PR) string {
	marker := "  "
	if isBoarded {
		marker = ui.Blue.Render(ui.BoardedMarker) + " "
	}

	name := fmt.Sprintf("%-*s", cols.maxNameWidth, wt.name)

	if !wt.loaded {
		return marker + name + "  " + ui.Dim.Render("…")
	}

	var buf strings.Builder
	buf.WriteString(marker)
	buf.WriteString(name)
	buf.WriteString("  ")

	// Dirty column: fixed 2-char slot (● + space) when any worktree in repo is dirty
	if cols.hasDirty {
		if wt.dirty {
			buf.WriteString(ui.Orange.Render("●") + " ")
		} else {
			buf.WriteString("  ")
		}
	}

	// Branch tracking (→ branch) when checked-out branch differs from worktree name
	if wt.branch != "" && wt.branch != wt.name {
		buf.WriteString(ui.Dim.Render("→ "+wt.branch) + " ")
	}

	// Ahead/behind remote
	if ab := formatAheadBehind(wt.ahead, wt.behind); ab != "" {
		buf.WriteString(ui.Dim.Render(ab))
	}

	// PR info
	if prStr := formatPRInfo(pr); prStr != "" {
		buf.WriteString(" " + prStr)
	}

	return strings.TrimRight(buf.String(), " ")
}

// Palette of colors for repo left borders — each repo gets a unique color.
var repoPalette = []lipgloss.Color{
	lipgloss.Color("63"),  // purple-blue
	lipgloss.Color("43"),  // teal
	lipgloss.Color("168"), // pink
	lipgloss.Color("107"), // olive
	lipgloss.Color("74"),  // steel blue
	lipgloss.Color("209"), // salmon
}

// renderRepoBlock renders content lines with a colored left border.
// The line at ruleIdx gets a ├─ connector instead of │ .
func renderRepoBlock(contentLines []string, ruleIdx int, borderColor lipgloss.Color) string {
	bStyle := lipgloss.NewStyle().Foreground(borderColor)
	var out []string
	for i, line := range contentLines {
		if i == ruleIdx {
			out = append(out, bStyle.Render("├─"+line))
		} else {
			out = append(out, bStyle.Render("│")+" "+line)
		}
	}
	return strings.Join(out, "\n")
}

func formatFooter(repoCount, wtCount, dirtyCount, behindCount, prCount, prErrors int) string {
	parts := []string{
		fmt.Sprintf("%d repos", repoCount),
		fmt.Sprintf("%d worktrees", wtCount),
	}
	if dirtyCount > 0 {
		parts = append(parts, fmt.Sprintf("%d dirty", dirtyCount))
	}
	if behindCount > 0 {
		parts = append(parts, fmt.Sprintf("%d behind", behindCount))
	}
	if prCount > 0 {
		parts = append(parts, fmt.Sprintf("%d open PRs", prCount))
	}
	if prErrors > 0 {
		parts = append(parts, "PRs unavailable")
	}
	if dirtyCount == 0 && behindCount == 0 {
		parts = append(parts, "all clean")
	}
	return ui.Dim.Render(strings.Join(parts, " · "))
}
