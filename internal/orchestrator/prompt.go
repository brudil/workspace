// Package orchestrator decides what happens next on a mission. When crew
// report done or end by themselves, a headless agent reads the brief and the
// history and acts through ws commands: dispatch new crew, resume crew,
// flag the user, raise a follow-up or suggest concluding.
package orchestrator

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/brudil/workspace/internal/mission"
)

// The prompt goes to the agent as one argument, and Linux caps a single
// argument at 128 KiB (MAX_ARG_STRLEN), so each section has a budget and the
// whole stays well under it. What's cut is a command away.
const (
	promptBriefMax      = 24 << 10
	promptCapsulesMax   = 16 << 10
	promptLogMax        = 16 << 10
	promptCrewMax       = 8 << 10
	promptHandoffMax    = 600
	promptTranscriptMax = 32 << 10
	promptMax           = 112 << 10
)

// Input is what the orchestrator is told about a mission.
type Input struct {
	Mission *mission.Mission
	// CrewID and Trigger say what prompted this run: crew that reported
	// "done" or "ended"; Trigger "manual" (or "") for a run by hand.
	CrewID, Trigger string
	Brief           string
	Capsules        []Capsule
	// Artifacts are the mission's recorded artifacts, one line each: kind,
	// where it is now, reference, label, and where to read it.
	Artifacts []string
	Log       []string // recent log lines, oldest first
	Output    string   // the end of CrewID's output
}

// Capsule is one of the mission's capsules, with what's known about it.
type Capsule struct {
	Name     string // repo/name
	Path     string
	Exists   bool
	Branch   string
	PR       int // 0 if none
	PRMerged bool
	Checks   string
	URL      string
	DiffStat string
}

// Prompt is everything the orchestrator needs, gathered up front so it can
// usually decide without digging.
func Prompt(in Input) string {
	m := in.Mission
	var b strings.Builder
	fmt.Fprintf(&b, "You are the orchestrator for the mission %q (%s) in a multi-repo workspace managed by the ws tool. Crew (coding agents) do the work; you decide what happens between them.\n", m.Title, m.Slug)
	switch in.Trigger {
	case "done":
		fmt.Fprintf(&b, "Crew %s has just reported that its part is done.\n", in.CrewID)
	case "ended":
		fmt.Fprintf(&b, "Crew %s has just ended without reporting done.\n", in.CrewID)
	case "answer":
		if a := m.Answer; a != nil {
			who := "They answered"
			if a.By != "" && a.By != "you" {
				who = "Crew " + a.By + ", which the user asked to answer it for them, answered"
			}
			fmt.Fprintf(&b, "You flagged the user, and they've answered.\n\nYou asked: %s\n%s: %s\n\nAct on the answer now: it's the decision you were waiting for.\n", a.Question, who, a.Text)
		}
	}
	b.WriteString(`
Read the brief and the history below and decide what should happen next: is what just finished good, what does the brief still need, and who should do it? The brief may describe stages for separate crew (say, a proposal and then its implementation); the log and crew list show which are done. Act only through these commands (WS_MISSION is set for you):

- ws mission note "Orchestrator: <what you found and decided, two or three sentences>"   (always, once; the next orchestrator run reads it)
- ws mission dispatch "<handoff>" [--capsule <repo>/<name>] [--read <artifact>]...   (start new, fresh crew on the next piece of work. The handoff is all it knows beyond the brief: what to do, where earlier crew's output is (files, commits, PRs), what to watch for. Name each artifact it should read first with --read and its reference from the Artifacts list; ws adds where to read it)
- ws mission resume-with <crew> "<what's left>"   (that crew should carry on: unfinished, tests missing, CI failing. Better than dispatching when its context matters)
- ws mission flag "<question for the user>" --option "<answer>" --option "<answer>"   (a decision or problem only the user can resolve, or the brief says to check with them before the next stage. Ask one clear question and give the two or three answers you'd act on, each a short instruction like "Commit both and open PRs": the user answers with one key, or writes their own, and you're run again with their answer. Give what they need to decide in the question itself: paths, what you checked)
- ws mission new "<follow-up, what and why>"   (separate work worth doing, outside this mission's scope)
- ws mission conclude "<why it's finished>"   (everything in the brief is met; PRs merged or ready)
- ws mission title "<terse name>"   (at most 50 characters, plain English; only when the title below is still the raw request, or no longer says what the mission is)

If crew asked the user something they haven't answered yet (crew_asked in the log, after that crew last started), the user answers the crew directly: don't flag the same question, just note that you're waiting for their answer.
An artifact still a draft or in review (its PR open) can be built on: the next crew reads it from its capsule. Only wait for it to land if the brief says to check in first; then flag instead.
If the next crew needs a capsule the mission doesn't have yet, create it first with ws lift <repo> <branch> (it joins the mission), then dispatch with --capsule.
You may investigate read-only: ws mission show -f llm, git -C <capsule path> diff/log/show/status for the capsule paths below, gh pr view/checks, and reading files. Don't change code, commit or push yourself. Prefer one clear next step; don't invent work, and if nothing is needed, just note why. If a command fails, read its message and correct it (a refused dispatch means flag the user instead). Finish with one short paragraph saying what you did.

Run one command per shell call rather than chaining them with && or ;. You may only be allowed the commands above: one outside them is refused, and the refusal covers that command alone, so carry on with the ones above. Always end by running your ws mission commands; never stop at saying what you would run.

`)
	if m.TitleIsRequest() {
		b.WriteString("The mission's title is still the request it was raised with: give it a name with ws mission title first.\n\n")
	}
	brief := strings.TrimSpace(in.Brief)
	if brief == "" {
		brief = "(empty)"
	}
	fmt.Fprintf(&b, "## Brief\n\n%s\n\n", clipHead(brief, promptBriefMax, "\n… (the brief continues: ws mission brief prints it whole)"))

	b.WriteString("## Capsules\n\n")
	if len(in.Capsules) == 0 {
		b.WriteString("(none)\n")
	}
	var caps strings.Builder
	for _, c := range in.Capsules {
		fmt.Fprintf(&caps, "- %s", c.Name)
		if c.Exists {
			fmt.Fprintf(&caps, " at %s (branch %s)", c.Path, c.Branch)
		} else {
			caps.WriteString(" (burned)")
		}
		if c.PR != 0 {
			state := "open"
			if c.PRMerged {
				state = "merged"
			}
			fmt.Fprintf(&caps, " — PR #%d %s", c.PR, state)
			if c.Checks != "" {
				fmt.Fprintf(&caps, ", checks %s", c.Checks)
			}
			if c.URL != "" {
				fmt.Fprintf(&caps, " %s", c.URL)
			}
		}
		caps.WriteByte('\n')
		if c.DiffStat != "" {
			for _, line := range strings.Split(c.DiffStat, "\n") {
				caps.WriteString("    " + line + "\n")
			}
		}
	}
	b.WriteString(clipHead(caps.String(), promptCapsulesMax, "… (more: ws mission show -f llm, and git diff --stat in each capsule)\n"))

	if len(in.Artifacts) > 0 {
		b.WriteString("\n## Artifacts\n\nWhat the mission has produced or relies on, beyond the capsules' PRs: [kind, where it is now] reference — label — where to read it.\n\n")
		var arts strings.Builder
		for _, a := range in.Artifacts {
			arts.WriteString("- " + a + "\n")
		}
		b.WriteString(clipHead(arts.String(), promptCapsulesMax, "… (more: ws mission show -f llm)\n"))
	}

	if len(in.Log) > 0 {
		b.WriteString("\n## Recent log\n\n")
		b.WriteString(clipTail(strings.Join(in.Log, "\n")+"\n", promptLogMax, "… (earlier: ws mission show -f llm)\n"))
	}

	if len(m.Crew) > 0 {
		b.WriteString("\n## Crew\n\n")
		var crew strings.Builder
		for _, c := range m.Crew {
			fmt.Fprintf(&crew, "- %s", c.ID)
			if c.Role != "" {
				fmt.Fprintf(&crew, " (%s)", c.Role)
			}
			if c.Capsule != "" {
				fmt.Fprintf(&crew, " in %s", c.Capsule)
			}
			if c.Ended != nil {
				fmt.Fprintf(&crew, ", ended (%s)", c.Exit)
			} else {
				crew.WriteString(", running")
			}
			if c.Status != "" {
				fmt.Fprintf(&crew, ", %s: %s", c.Status, c.Note)
			}
			if c.Handoff != "" {
				fmt.Fprintf(&crew, "\n    dispatched with: %s", clipHead(c.Handoff, promptHandoffMax, "…"))
			}
			crew.WriteByte('\n')
		}
		b.WriteString(clipTail(crew.String(), promptCrewMax, "… (earlier crew: ws mission show -f llm)\n"))
	}

	if in.CrewID != "" && in.Output != "" {
		fmt.Fprintf(&b, "\n## The end of %s's output\n\n```\n%s\n```\n", in.CrewID, clipTail(in.Output, promptTranscriptMax, "…\n"))
	}
	return clipHead(b.String(), promptMax, "\n… (cut to fit)")
}

// clipHead keeps the start of s within limit bytes, at a line break where one
// is near, never splitting a character, and appends more if anything went.
func clipHead(s string, limit int, more string) string {
	if len(s) <= limit {
		return s
	}
	cut := max(0, limit-len(more))
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	head := s[:cut]
	if i := strings.LastIndexByte(head, '\n'); i > len(head)/2 {
		head = head[:i+1]
	}
	return head + more
}

// clipTail keeps the end of s within limit bytes, from a line break where one
// is near, never splitting a character, and prepends more if anything went.
func clipTail(s string, limit int, more string) string {
	if len(s) <= limit {
		return s
	}
	cut := len(s) - limit + len(more)
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	tail := s[cut:]
	if i := strings.IndexByte(tail, '\n'); i >= 0 && i < len(tail)/2 {
		tail = tail[i+1:]
	}
	return more + tail
}
