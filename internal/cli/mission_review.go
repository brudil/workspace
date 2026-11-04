package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/brudil/workspace/internal/deckserver"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/ui"
	"github.com/spf13/cobra"
)

const (
	reviewTimeout         = 10 * time.Minute
	reviewTranscriptLines = 200
	reviewLogEvents       = 30
	reviewOutputKeep      = 1500 // characters of the reviewer's final message kept in the log
)

func newMissionReviewCmd() *cobra.Command {
	var slug, crewID, trigger, model string
	var auto, printPrompt bool
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Have a reviewer agent look at what crew did and propose what's next",
		Long: `Run a headless reviewer on a mission: it reads the brief, the log, the
capsules' diffs and PRs and the crew's output, then acts through ws:
ws mission note (its verdict), flag (needs you), resume-with (crew should
continue), new (a follow-up mission) or conclude (looks finished). It
doesn't change code or launch crew.

The deck runs this automatically when crew report done, or end by
themselves. The model is [review] model in ws.toml / ws.local.toml
(default ` + "claude-haiku-5-5-medium" + `); [review] enabled = false turns the automatic runs off.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			if auto && !ctx.Config.ReviewEnabled() {
				return nil
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			store := missionStore(ctx)
			if crewID != "" {
				if _, ok := crewRecord(m, crewID); !ok {
					return fmt.Errorf("mission %s has no crew %s", m.Slug, crewID)
				}
			}
			if model == "" {
				model = ctx.Config.ReviewModel()
			}
			prompt := reviewPrompt(ctx, m, crewID, trigger)

			h, err := harness.Get(harness.Default())
			if err != nil {
				return err
			}
			rv, ok := h.(harness.Reviewer)
			if !ok {
				if h, err = harness.Get("cursor"); err == nil {
					rv, ok = h.(harness.Reviewer)
				}
			}
			if !ok {
				return errors.New("no harness here can run a headless review")
			}
			l, err := rv.Review(prompt, ctx.WS.Root, model)
			if err != nil {
				return err
			}
			if printPrompt {
				fmt.Println(prompt)
				fmt.Fprintf(ui.Stderr, "  %s would run: %s\n", ui.Dim.Render("·"), strings.Join(append(l.Args[:len(l.Args)-1:len(l.Args)-1], "<prompt>"), " "))
				return nil
			}

			if auto && crewID != "" {
				fresh, err := store.MarkReviewed(m.Slug, crewID)
				if err != nil || !fresh {
					return err // already reviewed this event
				}
			}

			what := "the mission"
			if crewID != "" {
				what = crewID + " (" + trigger + ")"
			}
			store.AppendLog(m.Slug, mission.Event{Kind: mission.EventReview, Crew: "reviewer", Text: "reviewing " + what + " with " + model})
			fmt.Fprintf(ui.Stderr, "  %s reviewing %s on %s with %s…\n", ui.Dim.Render("·"), what, m.Slug, model)

			out, runErr := runReviewer(l, m.Slug)
			summary := strings.TrimSpace(out)
			if len(summary) > reviewOutputKeep {
				summary = "…" + summary[len(summary)-reviewOutputKeep:]
			}
			if runErr != nil {
				summary = "review failed: " + runErr.Error() + ifSome(" · "+summary, summary)
			}
			store.AppendLog(m.Slug, mission.Event{Kind: mission.EventReview, Crew: "reviewer", Text: summary})
			fmt.Fprintln(ui.Stderr, summary)
			return runErr
		},
	}
	addMissionFlag(cmd, &slug)
	cmd.Flags().StringVar(&crewID, "crew", "", "the crew whose work to review")
	cmd.Flags().StringVar(&trigger, "trigger", "manual", "what prompted the review: done, ended or manual")
	cmd.Flags().StringVar(&model, "model", "", "model to use (default: [review] model, or "+"claude-haiku-5-5-medium"+")")
	cmd.Flags().BoolVar(&auto, "auto", false, "run as the deck's automatic review: honour [review] enabled, review each crew event once")
	cmd.Flags().BoolVar(&printPrompt, "print-prompt", false, "print the reviewer's prompt and command instead of running it")
	return cmd
}

func ifSome(s, cond string) string {
	if cond == "" {
		return ""
	}
	return s
}

// runReviewer runs the reviewer with the reviewer role, returning what it
// printed.
func runReviewer(l harness.Launch, slug string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, l.Args[0], l.Args[1:]...)
	c.Dir = l.Dir
	env := crewEnv(l.Dir, slug, "")
	c.Env = append(env, EnvRole+"=reviewer")
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	err := c.Run()
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("timed out after %s", reviewTimeout)
	}
	return out.String(), err
}

// reviewPrompt is everything the reviewer needs, gathered up front so it can
// usually decide without digging: the brief, the capsules (diffs, PRs), the
// log, and the crew's output.
func reviewPrompt(ctx *Context, m *mission.Mission, crewID, trigger string) string {
	store := missionStore(ctx)
	v := buildMissionViews(ctx.WS, ctx.GitHub, []*mission.Mission{m})[0]
	brief, _ := store.ReadBrief(m.Slug)
	events, _ := store.ReadLog(m.Slug, reviewLogEvents)

	var b strings.Builder
	fmt.Fprintf(&b, "You are the reviewer for the mission %q (%s) in a multi-repo workspace managed by the ws tool.\n", m.Title, m.Slug)
	switch trigger {
	case "done":
		fmt.Fprintf(&b, "Crew %s has just reported that its part is done.\n", crewID)
	case "ended":
		fmt.Fprintf(&b, "Crew %s has just ended without reporting done.\n", crewID)
	}
	b.WriteString(`
Decide whether the mission's goal and acceptance criteria are met, and what should happen next. Act only through these commands (WS_MISSION is set for you):

- ws mission note "Review: <verdict and reasoning, two or three sentences>"   (always do this, once)
- ws mission flag "<why the user is needed>"   (a decision or problem only the user can resolve)
- ws mission resume-with <crew> "<what's left>"   (crew should continue: something unfinished, tests missing, CI failing)
- ws mission new "<follow-up, what and why>"   (separate work worth doing, outside this mission's scope)
- ws mission conclude "<why it's finished>"   (everything's met; PRs merged or ready)

You may investigate read-only: ws mission show -f llm, git diff/log in the capsule paths below, gh pr view/checks. Don't change code, commit, push, or launch or stop crew. Prefer the single most useful action; don't invent work. If a command fails, read its message and correct it. Finish with one short paragraph saying what you did.

`)
	fmt.Fprintf(&b, "## Brief\n\n%s\n\n", strings.TrimSpace(firstNonEmpty(brief, "(empty)")))

	b.WriteString("## Capsules\n\n")
	if len(v.Capsules) == 0 {
		b.WriteString("(none)\n")
	}
	for _, c := range v.Capsules {
		fmt.Fprintf(&b, "- %s/%s", c.Repo, c.Name)
		if c.Exists {
			fmt.Fprintf(&b, " at %s (branch %s)", c.Path, c.Branch)
		} else {
			b.WriteString(" (burned)")
		}
		if c.PR != nil {
			state := "open"
			if c.PRS == mission.PRMerged {
				state = "merged"
			}
			fmt.Fprintf(&b, " — PR #%d %s", c.PR.Number, state)
			if c.PR.StatusRollup != "" {
				fmt.Fprintf(&b, ", checks %s", c.PR.StatusRollup)
			}
			if c.PR.URL != "" {
				fmt.Fprintf(&b, " %s", c.PR.URL)
			}
		}
		b.WriteByte('\n')
		if c.Exists {
			if stat := diffStat(c.Path, "origin/"+ctx.WS.DefaultBranch); stat != "" {
				for _, line := range strings.Split(stat, "\n") {
					b.WriteString("    " + line + "\n")
				}
			}
		}
	}

	if len(events) > 0 {
		b.WriteString("\n## Recent log\n\n")
		for _, e := range events {
			b.WriteString(formatEventLine(e, true) + "\n")
		}
	}

	if crewID != "" {
		if tail := crewOutputTail(ctx, m.Slug, crewID); tail != "" {
			fmt.Fprintf(&b, "\n## The end of %s's output\n\n```\n%s\n```\n", crewID, tail)
		}
	}
	return b.String()
}

func firstNonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// diffStat summarises a capsule's changes against its base.
func diffStat(dir, base string) string {
	out, err := exec.Command("git", "-C", dir, "diff", "--stat", base+"...HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

// crewOutputTail is the last lines a crew showed: from the deck server if
// it's still running there, else from its saved transcript.
func crewOutputTail(ctx *Context, slug, crewID string) string {
	text := ""
	if c, err := deckserver.Dial(ctx.WS.Root); err == nil {
		text, _ = c.Transcript(slug + "/" + crewID)
		c.Close()
	}
	if text == "" {
		if data, err := os.ReadFile(missionStore(ctx).TranscriptPath(slug, crewID)); err == nil {
			text = string(data)
		}
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > reviewTranscriptLines {
		lines = lines[len(lines)-reviewTranscriptLines:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
