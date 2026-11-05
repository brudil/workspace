package mission

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// A handoff is what crew leave for whoever comes next when they report
// done: what they did, what they learned, what's left and what to read
// first. It's a private mission file (HandoffRef), recorded as an artifact
// of kind "handoff", and new crew and the orchestrator are given it.

// HandoffKind is the artifact kind handoffs are recorded as.
const HandoffKind = "handoff"

// Handoff is a parsed handoff.
type Handoff struct {
	Done, Learned, Left string // each section's markdown
	ReadFirst           []ReadItem
	// Other is anything under headings beyond the four, kept as written.
	Other string
}

// ReadItem is one entry under Read first: a reference (an artifact
// reference, a file or a URL) and what it's for, if said.
type ReadItem struct {
	Ref, Note string
}

// HandoffTemplate is what crew are shown when they're asked for one.
const HandoffTemplate = `## Done
- What you did, and where it is (files, commits, PRs).
## Learned
- Where things live, what you tried that didn't work and why, surprises.
## Left
- What's still to do, or "Nothing."
## Read first
- Files, references (records:adr/0042.md) or URLs the next crew should read, or "Nothing."`

var handoffHeading = regexp.MustCompile(`^#{1,3}\s+(.+?)\s*#*\s*$`)

// ParseHandoff reads a handoff written in markdown. The four sections
// (Done, Learned, Left, Read first) must each be there, under headings of
// any level, in any order, and say something: "Nothing." will do. Read
// first lists its entries as a list, "- <reference> — <what for>".
func ParseHandoff(text string) (*Handoff, error) {
	sections := map[string]*strings.Builder{}
	var other strings.Builder
	var cur *strings.Builder
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if g := handoffHeading.FindStringSubmatch(line); g != nil {
			name := strings.ToLower(strings.Trim(g[1], "*_: "))
			if name == "read" || name == "read next" {
				name = "read first"
			}
			switch name {
			case "done", "learned", "left", "read first":
				if sections[name] == nil {
					sections[name] = &strings.Builder{}
				}
				cur = sections[name]
				continue
			}
			cur = &other
			other.WriteString(line + "\n")
			continue
		}
		if cur != nil {
			cur.WriteString(line + "\n")
		}
	}
	body := func(name string) string {
		if b := sections[name]; b != nil {
			return strings.TrimSpace(b.String())
		}
		return ""
	}
	var missing, empty []string
	for _, name := range []string{"done", "learned", "left", "read first"} {
		switch {
		case sections[name] == nil:
			missing = append(missing, name)
		case body(name) == "":
			empty = append(empty, name)
		}
	}
	if len(missing) > 0 || len(empty) > 0 {
		var why []string
		if len(missing) > 0 {
			why = append(why, "no "+sectionList(missing)+" section")
		}
		if len(empty) > 0 {
			why = append(why, sectionList(empty)+" says nothing (write \"Nothing.\" if there's nothing)")
		}
		return nil, errors.New(strings.Join(why, "; "))
	}
	h := &Handoff{Done: body("done"), Learned: body("learned"), Left: body("left"), Other: strings.TrimSpace(other.String())}
	items, err := parseReadFirst(body("read first"))
	if err != nil {
		return nil, err
	}
	h.ReadFirst = items
	return h, nil
}

func sectionList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = `"` + strings.ToUpper(n[:1]) + n[1:] + `"`
	}
	return strings.Join(quoted, ", ")
}

var listItem = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+(.*)$`)

// nothing reports whether a section says there's nothing in it.
func nothing(s string) bool {
	s = strings.ToLower(strings.Trim(strings.TrimSpace(s), ".!*_- "))
	return s == "nothing" || s == "none" || s == "n/a" || s == "nothing to read"
}

func parseReadFirst(text string) ([]ReadItem, error) {
	if nothing(text) {
		return nil, nil
	}
	var items []ReadItem
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		g := listItem.FindStringSubmatch(line)
		if g == nil {
			if len(items) > 0 && strings.HasPrefix(line, " ") {
				continue // a list item's wrapped line
			}
			return nil, fmt.Errorf(`"Read first" lists what to read as "- <file, reference or URL> — <what for>", or says "Nothing." (not: %q)`, strings.TrimSpace(line))
		}
		item := ReadItem{Ref: g[1]}
		for _, sep := range []string{" — ", " – ", " - "} {
			if ref, note, ok := strings.Cut(item.Ref, sep); ok {
				item.Ref, item.Note = ref, strings.TrimSpace(note)
				break
			}
		}
		item.Ref = strings.Trim(strings.TrimSpace(item.Ref), "`<>")
		if item.Ref == "" || nothing(item.Ref) {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// HandoffRef is where crew's handoff is kept, as an artifact reference.
func HandoffRef(crewID string) string { return "mission:handoffs/" + crewID + ".md" }

// HandoffPath is the file a crew's handoff is kept in.
func (s Store) HandoffPath(slug, crewID string) string {
	return filepath.Join(s.FilesDir(slug), "handoffs", crewID+".md")
}

// LeftLine is the first line of what a handoff says is left, or "" when
// nothing is.
func (h *Handoff) LeftLine() string {
	if nothing(h.Left) {
		return ""
	}
	for _, l := range strings.Split(h.Left, "\n") {
		if g := listItem.FindStringSubmatch(l); g != nil {
			l = g[1]
		}
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// Render is the handoff as it's kept: the summary as its title, who left it,
// when and where, then the sections.
func (h *Handoff) Render(summary string, c Crew, at time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", strings.TrimSpace(summary))
	fmt.Fprintf(&b, "Handoff from crew %s", c.ID)
	if c.Capsule != "" {
		fmt.Fprintf(&b, ", in %s", c.Capsule)
	}
	fmt.Fprintf(&b, ", %s.\n\n", at.Local().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "## Done\n\n%s\n\n## Learned\n\n%s\n\n## Left\n\n%s\n\n## Read first\n\n", h.Done, h.Learned, h.Left)
	if len(h.ReadFirst) == 0 {
		b.WriteString("Nothing.\n")
	}
	for _, r := range h.ReadFirst {
		line := "- " + r.Ref
		if r.Note != "" {
			line += " — " + r.Note
		}
		b.WriteString(line + "\n")
	}
	if h.Other != "" {
		b.WriteString("\n" + h.Other + "\n")
	}
	return b.String()
}

// ReportDone marks crew done with a summary and, for work crew, keeps
// their handoff: written to HandoffPath, recorded as a handoff artifact
// by that crew (replacing an earlier one of theirs), and summarised in the
// log with what's left.
func (s Store) ReportDone(slug, id, summary string, h *Handoff) error {
	if h == nil {
		return s.SetCrewStatus(slug, id, CrewDone, summary)
	}
	m, err := s.Load(slug)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(m.Crew, func(c Crew) bool { return c.ID == id })
	if i < 0 {
		return fmt.Errorf("mission %s has no crew %s", slug, id)
	}
	at := now()
	path := s.HandoffPath(slug, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(h.Render(summary, m.Crew[i], at)), 0o644); err != nil {
		return err
	}
	ref := HandoffRef(id)
	if _, err := s.Update(slug, func(m *Mission) error {
		for i := range m.Crew {
			if m.Crew[i].ID == id {
				m.Crew[i].Status, m.Crew[i].Note, m.Crew[i].StatusAt = CrewDone, summary, &at
				m.Crew[i].HandedOff = &at
			}
		}
		for i := range m.Artifacts {
			if m.Artifacts[i].Ref == ref {
				m.Artifacts[i].Label, m.Artifacts[i].Added = summary, at
				return nil
			}
		}
		m.Artifacts = append(m.Artifacts, Artifact{Ref: ref, Kind: HandoffKind, Label: summary, Crew: id, Added: at})
		return nil
	}); err != nil {
		return err
	}
	text := summary
	if left := h.LeftLine(); left != "" {
		text += " · left: " + left
	}
	return s.AppendLog(slug, Event{Kind: EventCrewDone, Crew: id, Text: text})
}

// LatestHandoffs is the newest handoff from each capsule's crew, newest
// first: the crew records that left them.
func (m *Mission) LatestHandoffs() []Crew {
	latest := map[string]Crew{}
	for _, c := range m.Crew {
		if c.HandedOff == nil {
			continue
		}
		if l, ok := latest[c.Capsule]; !ok || c.HandedOff.After(*l.HandedOff) {
			latest[c.Capsule] = c
		}
	}
	out := make([]Crew, 0, len(latest))
	for _, c := range latest {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Crew) int { return b.HandedOff.Compare(*a.HandedOff) })
	return out
}
