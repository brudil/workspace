package review

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Render is a review as crew and the orchestrator read it: everything to
// act on without the diff. Comments are grouped by file, each with its
// lines as they were when commented on, and what to do comes last.
func Render(r *Review) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", Title(r))
	when := ""
	if r.Submitted != nil {
		when = ", " + r.Submitted.Local().Format("2006-01-02 15:04")
	}
	fmt.Fprintf(&b, "From the user%s, of commit %s (the capsule's changes since %s).\n\n", when, short(r.Head), r.BaseRef)
	if s := strings.TrimSpace(r.Summary); s != "" {
		for _, l := range strings.Split(s, "\n") {
			b.WriteString(strings.TrimRight("> "+l, " ") + "\n")
		}
		b.WriteString("\n")
	}
	if len(r.Comments) == 0 {
		b.WriteString("No comments on the code.\n\n")
	} else {
		fmt.Fprintf(&b, "%d %s%s.\n\n", len(r.Comments), plural(len(r.Comments), "comment"), countWords(r))
	}

	var paths []string
	byPath := map[string][]Note{}
	for _, n := range r.Comments {
		if _, ok := byPath[n.Path]; !ok {
			paths = append(paths, n.Path)
		}
		byPath[n.Path] = append(byPath[n.Path], n)
	}
	for _, p := range paths {
		fmt.Fprintf(&b, "## %s\n\n", p)
		for _, n := range byPath[p] {
			renderNote(&b, n)
		}
	}

	b.WriteString("## What to do\n\n")
	switch r.Verdict {
	case Approve:
		b.WriteString("Approved: the work is good to go. Act on any comments above only if they're quick and clearly right.\n")
	default:
		b.WriteString("Address every blocking comment. Take each suggestion and nit too unless you have a reason not to, and answer each question. ")
		b.WriteString("Work in the capsule, commit your changes, and report done with a handoff that says, for each comment by its id, what you did (or why not).\n")
	}
	return b.String()
}

func renderNote(b *strings.Builder, n Note) {
	// New-side lines are where they are at the reviewed commit; old-side
	// ones are lines the change removed, numbered as they were before it.
	where := fmt.Sprintf("%s:%d", n.Path, n.Line)
	if n.EndLine > n.Line {
		where += fmt.Sprintf("-%d", n.EndLine)
	}
	where += " at " + short(n.Commit)
	if n.Side == "old" {
		lines := fmt.Sprintf("line %d", n.Line)
		if n.EndLine > n.Line {
			lines = fmt.Sprintf("lines %d–%d", n.Line, n.EndLine)
		}
		where = fmt.Sprintf("%s, removed %s (as numbered at %s, before the change)", n.Path, lines, short(n.Commit))
	}
	fmt.Fprintf(b, "### %s · %s · %s\n\n", n.ID, n.Kind, where)
	lang := strings.TrimPrefix(filepath.Ext(n.Path), ".")
	if len(n.Excerpt) > 0 {
		var ex strings.Builder
		width := len(fmt.Sprint(n.EndLine))
		for i, l := range n.Excerpt {
			fmt.Fprintf(&ex, "%*d  %s\n", width, n.Line+i, l)
		}
		fence(b, lang, ex.String())
	}
	if body := strings.TrimSpace(n.Body); body != "" {
		b.WriteString(body + "\n\n")
	}
	if n.Suggestion != nil {
		if *n.Suggestion == "" {
			b.WriteString("Suggested change: delete these lines.\n\n")
		} else {
			b.WriteString("Suggested change, replacing these lines:\n\n")
			fence(b, lang, *n.Suggestion+"\n")
		}
	}
	for _, t := range n.Thread {
		fmt.Fprintf(b, "- %s, %s: %s\n", t.By, t.At.Local().Format("2006-01-02 15:04"), t.Body)
	}
	if len(n.Thread) > 0 {
		b.WriteString("\n")
	}
}

// fence writes code in a fence longer than any run of backticks in it.
func fence(b *strings.Builder, lang, code string) {
	n, run := 3, 0
	for _, c := range code {
		if c == '`' {
			run++
			n = max(n, run+1)
		} else {
			run = 0
		}
	}
	f := strings.Repeat("`", n)
	fmt.Fprintf(b, "%s%s\n%s%s\n\n", f, lang, code, f)
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
