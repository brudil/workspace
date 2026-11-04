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

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/deckserver"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/ui"
	"github.com/spf13/cobra"
)

const (
	orchestrateTimeout         = 10 * time.Minute
	orchestrateTranscriptLines = 200
	orchestrateLogEvents       = 40
	orchestrateOutputKeep      = 1500 // characters of the orchestrator's final message kept in the log
)

func newMissionOrchestrateCmd() *cobra.Command {
	var slug, crewID, trigger, model string
	var auto, printPrompt bool
	cmd := &cobra.Command{
		Use:   "orchestrate",
		Short: "Have the orchestrator decide what happens next on a mission",
		Long: `Run the orchestrator on a mission: a headless agent that reads the brief,
the log, the capsules' diffs and PRs and the crew's output, then decides
what's next and acts through ws: ws mission dispatch (start new crew with a
handoff), resume-with (crew should carry on), flag (needs you), new (a
follow-up mission) or conclude (looks finished), always leaving a note. It
doesn't change code.

The deck runs this automatically when crew report done, or end by
themselves. Configure it under [orchestrator] in ws.toml / ws.local.toml:
model (default ` + config.DefaultOrchestratorModel + `), enabled = false to stop the
automatic runs, dispatch = "ask" to queue dispatched crew for you instead of
launching them, and max_dispatches (default 3) dispatches in a row without
you before it must flag you.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			if auto && !ctx.Config.OrchestratorEnabled() {
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
				model = ctx.Config.OrchestratorModel()
			}
			prompt := orchestratePrompt(ctx, m, crewID, trigger)

			h, err := harness.Get(harness.Default())
			if err != nil {
				return err
			}
			rv, ok := h.(harness.Orchestrator)
			if !ok {
				if h, err = harness.Get("cursor"); err == nil {
					rv, ok = h.(harness.Orchestrator)
				}
			}
			if !ok {
				return errors.New("no harness here can run a headless orchestrator")
			}
			l, err := rv.Orchestrate(prompt, ctx.WS.Root, model)
			if err != nil {
				return err
			}
			if printPrompt {
				fmt.Println(prompt)
				fmt.Fprintf(ui.Stderr, "  %s would run: %s\n", ui.Dim.Render("·"), strings.Join(append(l.Args[:len(l.Args)-1:len(l.Args)-1], "<prompt>"), " "))
				return nil
			}

			if auto && crewID != "" {
				fresh, err := store.MarkOrchestrated(m.Slug, crewID)
				if err != nil || !fresh {
					return err // already handled this event
				}
			}

			what := "the mission"
			if crewID != "" {
				what = crewID + " (" + trigger + ")"
			}
			store.AppendLog(m.Slug, mission.Event{Kind: mission.EventOrchestrate, Crew: "orchestrator", Text: "looking at " + what + " with " + model})
			fmt.Fprintf(ui.Stderr, "  %s orchestrator looking at %s on %s with %s…\n", ui.Dim.Render("·"), what, m.Slug, model)

			out, runErr := runOrchestrator(l, m.Slug)
			summary := strings.TrimSpace(out)
			if len(summary) > orchestrateOutputKeep {
				summary = "…" + summary[len(summary)-orchestrateOutputKeep:]
			}
			if runErr != nil {
				summary = "orchestrator failed: " + runErr.Error() + ifSome(" · "+summary, summary)
			}
			store.AppendLog(m.Slug, mission.Event{Kind: mission.EventOrchestrate, Crew: "orchestrator", Text: summary})
			fmt.Fprintln(ui.Stderr, summary)
			return runErr
		},
	}
	addMissionFlag(cmd, &slug)
	cmd.Flags().StringVar(&crewID, "crew", "", "the crew whose event prompted this")
	cmd.Flags().StringVar(&trigger, "trigger", "manual", "what prompted the run: done, ended or manual")
	cmd.Flags().StringVar(&model, "model", "", "model to use (default: [orchestrator] model, or "+config.DefaultOrchestratorModel+")")
	cmd.Flags().BoolVar(&auto, "auto", false, "run as the deck's automatic run: honour [orchestrator] enabled, handle each crew event once")
	cmd.Flags().BoolVar(&printPrompt, "print-prompt", false, "print the orchestrator's prompt and command instead of running it")
	return cmd
}

func ifSome(s, cond string) string {
	if cond == "" {
		return ""
	}
	return s
}

// runOrchestrator runs the orchestrator with its role, returning what it
// printed.
func runOrchestrator(l harness.Launch, slug string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), orchestrateTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, l.Args[0], l.Args[1:]...)
	c.Dir = l.Dir
	env := crewEnv(l.Dir, slug, "")
	c.Env = append(env, EnvRole+"="+roleOrchestrator)
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	err := c.Run()
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("timed out after %s", orchestrateTimeout)
	}
	return out.String(), err
}

// orchestratePrompt is everything the orchestrator needs, gathered up front
// so it can usually decide without digging: the brief, the capsules (diffs,
// PRs), the log, the crew and the latest crew's output.
func orchestratePrompt(ctx *Context, m *mission.Mission, crewID, trigger string) string {
	store := missionStore(ctx)
	v := buildMissionViews(ctx.WS, ctx.GitHub, []*mission.Mission{m})[0]
	brief, _ := store.ReadBrief(m.Slug)
	events, _ := store.ReadLog(m.Slug, orchestrateLogEvents)

	var b strings.Builder
	fmt.Fprintf(&b, "You are the orchestrator for the mission %q (%s) in a multi-repo workspace managed by the ws tool. Crew (coding agents) do the work; you decide what happens between them.\n", m.Title, m.Slug)
	switch trigger {
	case "done":
		fmt.Fprintf(&b, "Crew %s has just reported that its part is done.\n", crewID)
	case "ended":
		fmt.Fprintf(&b, "Crew %s has just ended without reporting done.\n", crewID)
	}
	b.WriteString(`
Read the brief and the history below and decide what should happen next: is what just finished good, what does the brief still need, and who should do it? The brief may describe stages for separate crew (say, a proposal and then its implementation); the log and crew list show which are done. Act only through these commands (WS_MISSION is set for you):

- ws mission note "Orchestrator: <what you found and decided, two or three sentences>"   (always, once; the next orchestrator run reads it)
- ws mission dispatch "<handoff>" [--capsule <repo>/<name>]   (start new, fresh crew on the next piece of work. The handoff is all it knows beyond the brief: what to do, where earlier crew's output is (files, commits, PRs), what to watch for)
- ws mission resume-with <crew> "<what's left>"   (that crew should carry on: unfinished, tests missing, CI failing. Better than dispatching when its context matters)
- ws mission flag "<why the user is needed>"   (a decision or problem only the user can resolve, or the brief says to check with them before the next stage)
- ws mission new "<follow-up, what and why>"   (separate work worth doing, outside this mission's scope)
- ws mission conclude "<why it's finished>"   (everything in the brief is met; PRs merged or ready)

If the next crew needs a capsule the mission doesn't have yet, create it first with ws lift <repo> <branch> (it joins the mission), then dispatch with --capsule.
You may investigate read-only: ws mission show -f llm, git diff/log in the capsule paths below, gh pr view/checks. Don't change code, commit or push yourself. Prefer one clear next step; don't invent work, and if nothing is needed, just note why. If a command fails, read its message and correct it (a refused dispatch means flag the user instead). Finish with one short paragraph saying what you did.

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

	if len(m.Crew) > 0 {
		b.WriteString("\n## Crew\n\n")
		for _, c := range m.Crew {
			fmt.Fprintf(&b, "- %s", c.ID)
			if c.Role != "" {
				fmt.Fprintf(&b, " (%s)", c.Role)
			}
			if c.Capsule != "" {
				fmt.Fprintf(&b, " in %s", c.Capsule)
			}
			switch {
			case c.Ended != nil:
				fmt.Fprintf(&b, ", ended (%s)", c.Exit)
			default:
				b.WriteString(", running")
			}
			if c.Status != "" {
				fmt.Fprintf(&b, ", %s: %s", c.Status, c.Note)
			}
			if c.Handoff != "" {
				fmt.Fprintf(&b, "\n    dispatched with: %s", c.Handoff)
			}
			b.WriteByte('\n')
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
	if len(lines) > orchestrateTranscriptLines {
		lines = lines[len(lines)-orchestrateTranscriptLines:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
