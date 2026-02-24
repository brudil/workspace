package cli

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/workspace"
)

func runStatusLLM(ws *workspace.Workspace, gh github.Client) error {
	outlines := ws.StatusOutline(false)

	reverseAliases := make(map[string][]string)
	for alias, canonical := range ws.AliasMap {
		reverseAliases[canonical] = append(reverseAliases[canonical], alias)
	}
	for _, aliases := range reverseAliases {
		sort.Strings(aliases)
	}

	data := collectStatusData(ws, gh, outlines)

	type wtEntry struct {
		name   string
		status workspace.WorktreeStatus
		pr     *github.PR
	}
	type llmRepoEntry struct {
		outline   workspace.RepoOutline
		worktrees []wtEntry
	}

	repos := make([]llmRepoEntry, len(outlines))
	for i, o := range outlines {
		wts := make([]wtEntry, len(o.Worktrees))
		for j, wtName := range o.Worktrees {
			var st workspace.WorktreeStatus
			if data.statuses[i] != nil {
				st = data.statuses[i][j]
			}
			wts[j] = wtEntry{name: wtName, status: st}
		}

		prs := data.prsByRepo[o.Name]
		if prs != nil {
			for j := range wts {
				wts[j].pr = lookupPR(prs, wts[j].status.Branch, wts[j].name)
			}
		}

		repos[i] = llmRepoEntry{outline: o, worktrees: wts}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Workspace: %s\n", ws.Title()))

	for idx, repo := range repos {
		if idx > 0 {
			b.WriteByte('\n')
		}

		name := repo.outline.Name

		if repo.outline.Err != nil {
			b.WriteString(formatLLMRepoHeader(name, ws.DisplayNames[name], reverseAliases[name]))
			b.WriteString(fmt.Sprintf(" — error: %s\n", simplifyError(repo.outline.Err)))
			continue
		}

		b.WriteString(formatLLMRepoHeader(name, ws.DisplayNames[name], reverseAliases[name]))
		b.WriteByte('\n')

		for _, wt := range repo.worktrees {
			b.WriteString("  ")
			b.WriteString(formatLLMWorktreeLine(wt.name, wt.status, wt.pr, slices.Contains(repo.outline.Boarded, wt.name)))
			b.WriteByte('\n')
		}
	}

	_, err := os.Stdout.WriteString(b.String())
	return err
}

func formatLLMRepoHeader(canonical, displayName string, aliases []string) string {
	var b strings.Builder
	b.WriteString(canonical)

	var meta []string
	if displayName != "" {
		meta = append(meta, fmt.Sprintf("%q", displayName))
	}
	if len(aliases) > 0 {
		meta = append(meta, "aliases: "+strings.Join(aliases, ", "))
	}
	if len(meta) > 0 {
		b.WriteString(" (")
		b.WriteString(strings.Join(meta, " — "))
		b.WriteByte(')')
	}

	return b.String()
}

func formatLLMWorktreeLine(name string, st workspace.WorktreeStatus, pr *github.PR, boarded bool) string {
	var parts []string
	parts = append(parts, name)

	if boarded {
		parts = append(parts, "[boarded]")
	}

	if st.Branch == "" && name != ".ground" {
		parts = append(parts, "(detached)")
	}

	if st.Dirty {
		parts = append(parts, "dirty")
	}
	if st.Ahead > 0 {
		parts = append(parts, fmt.Sprintf("↑%d", st.Ahead))
	}
	if st.Behind > 0 {
		parts = append(parts, fmt.Sprintf("↓%d", st.Behind))
	}

	if pr != nil {
		parts = append(parts, formatLLMPR(pr))
	}

	return strings.Join(parts, " ")
}

func formatLLMPR(pr *github.PR) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("PR #%d", pr.Number))
	parts = append(parts, pr.State)

	switch pr.StatusRollup {
	case "success":
		parts = append(parts, "checks:pass")
	case "failure":
		parts = append(parts, "checks:fail")
	case "pending":
		parts = append(parts, "checks:pending")
	}

	switch pr.ReviewDecision {
	case "APPROVED":
		parts = append(parts, "review:approved")
	case "CHANGES_REQUESTED":
		parts = append(parts, "review:changes-requested")
	case "REVIEW_REQUIRED":
		parts = append(parts, "review:required")
	}

	return strings.Join(parts, " ")
}

func simplifyError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "no such file or directory") {
		return "repo not found"
	}
	return msg
}
