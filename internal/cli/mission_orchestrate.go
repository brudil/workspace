package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/brudil/workspace/internal/crew"
	"github.com/brudil/workspace/internal/execlog"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/orchestrator"
	"github.com/brudil/workspace/internal/review"
	"github.com/brudil/workspace/internal/ui"
	"github.com/spf13/cobra"
)

// orchestrateSpawner starts the orchestrator for a crew event; tests stub
// it (os.Executable is the test binary).
var orchestrateSpawner orchestrator.Spawner = orchestrator.Spawn

// orchestrateHook is the end hook hosts run: the orchestrator for crew that
// ended by themselves.
func orchestrateHook(root string, store mission.Store) crew.EndHook {
	return orchestrator.AfterEnd(root, store, func(root, slug, crewID, trigger string) {
		orchestrateSpawner(root, slug, crewID, trigger)
	})
}

func newMissionOrchestrateCmd() *cobra.Command {
	var slug, crewID, trigger, model string
	var auto, printPrompt bool
	cmd := &cobra.Command{
		Use:   "orchestrate",
		Short: "Have the orchestrator decide what happens next on a mission",
		Long: `Run the orchestrator on a mission: a headless agent that reads the brief,
the log, the capsules' diffs and PRs and the crew's output, then decides
what's next and acts through ws: ws mission dispatch (start new crew on
the next piece of work), resume-with (crew should carry on), flag (needs you), new (a
follow-up mission) or conclude (looks finished), always leaving a note. It
doesn't change code.

The deck runs this automatically when crew report done, or end by
themselves. Configure it under [orchestrator] in ws.toml / ws.local.toml:
harness (cursor or claude; default the crew's, if it can run headless),
model (default ` + harness.CursorOrchestratorModel + ` on cursor, ` + harness.ClaudeOrchestratorModel + ` on claude),
enabled = false to stop the automatic runs, dispatch = "ask" to queue
dispatched crew for you instead of launching them, and max_dispatches
(default 3) dispatches in a row without you before it must flag you.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			if auto && !ctx.Config.OrchestratorEnabled() {
				execlog.Note(ctx.WS.Root, "orchestrator "+slug, "not run for %s: [orchestrator] enabled = false", strings.TrimSpace(crewID+" ("+trigger+")"))
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
			pick, rv, err := agentRoles(ctx.Config).ForOrchestrator()
			if err != nil {
				return err
			}
			if model == "" {
				model = pick.Model
			}
			in := orchestratorInput(ctx, m, crewID, trigger)
			prompt := orchestrator.Prompt(in)
			run := harness.OrchestratorRun{Prompt: "<a line pointing at the prompt file>", Dir: ctx.WS.Root, Model: model}
			for _, c := range in.Capsules {
				if c.Exists {
					run.Capsules = append(run.Capsules, c.Path)
				}
			}
			if printPrompt {
				l, err := rv.Orchestrate(run)
				if err != nil {
					return err
				}
				fmt.Println(prompt)
				fmt.Fprintf(ui.Stderr, "  %s would write that to %s and run: %s\n", ui.Dim.Render("·"),
					orchestrator.PromptFile(store, m.Slug), strings.Join(l.Args, " "))
				return nil
			}

			if auto && crewID != "" {
				fresh, err := store.MarkOrchestrated(m.Slug, crewID)
				if err == nil && !fresh {
					execlog.Note(ctx.WS.Root, "orchestrator "+m.Slug, "not run for %s (%s): already handled that event", crewID, trigger)
				}
				if err != nil || !fresh {
					return err
				}
			}

			what := "the mission"
			switch {
			case crewID != "":
				what = crewID + " (" + trigger + ")"
			case trigger == "answer":
				what = "the answer to its question"
			case trigger == "review":
				what = "your review"
			}
			with := rv.Name() + " " + model
			store.AppendLog(m.Slug, mission.Event{Kind: mission.EventOrchestrate, Crew: "orchestrator", Text: "looking at " + what + " with " + with})
			fmt.Fprintf(ui.Stderr, "  %s orchestrator looking at %s on %s with %s…\n", ui.Dim.Render("·"), what, m.Slug, with)

			run.Prompt, err = orchestrator.WritePrompt(store, m.Slug, prompt)
			if err != nil {
				return err
			}
			l, err := rv.Orchestrate(run)
			if err != nil {
				return err
			}
			out, runErr := orchestrator.Run(l, ctx.WS.Root, m.Slug)
			summary := orchestrator.Summary(out, runErr)
			store.AppendLog(m.Slug, mission.Event{Kind: mission.EventOrchestrate, Crew: "orchestrator", Text: summary})
			fmt.Fprintln(ui.Stderr, summary)
			return runErr
		},
	}
	addMissionFlag(cmd, &slug)
	cmd.Flags().StringVar(&crewID, "crew", "", "the crew whose event prompted this")
	cmd.Flags().StringVar(&trigger, "trigger", "manual", "what prompted the run: done, ended, answer (the user answered a flag), review (the user submitted one) or manual")
	cmd.Flags().StringVar(&model, "model", "", "model to use (default: [orchestrator] model, or the harness's own)")
	cmd.Flags().BoolVar(&auto, "auto", false, "run as the deck's automatic run: honour [orchestrator] enabled, handle each crew event once")
	cmd.Flags().BoolVar(&printPrompt, "print-prompt", false, "print the orchestrator's prompt and command instead of running it")
	return cmd
}

// orchestratorInput gathers what the orchestrator is told: the brief, the
// capsules (diffs, PRs), the recent log, and the prompting crew's output.
func orchestratorInput(ctx *Context, m *mission.Mission, crewID, trigger string) orchestrator.Input {
	store := missionStore(ctx)
	v := buildMissionViews(ctx.WS, ctx.GitHub, []*mission.Mission{m})[0]
	brief, _ := store.ReadBrief(m.Slug)
	events, _ := store.ReadLog(m.Slug, orchestrator.LogEvents)
	in := orchestrator.Input{Mission: m, CrewID: crewID, Trigger: trigger, Brief: brief}
	for _, c := range v.Capsules {
		oc := orchestrator.Capsule{Name: c.Repo + "/" + c.Name, Path: c.Path, Exists: c.Exists, Branch: c.Branch}
		if c.PR != nil {
			oc.PR, oc.PRMerged, oc.Checks, oc.URL = c.PR.Number, c.PRS == mission.PRMerged, c.PR.StatusRollup, c.PR.URL
		}
		if c.Exists {
			oc.DiffStat = orchestrator.DiffStat(c.Path, "origin/"+ctx.WS.DefaultBranch)
		}
		in.Capsules = append(in.Capsules, oc)
	}
	for _, av := range resolveArtifacts(ctx.WS, store, v) {
		in.Artifacts = append(in.Artifacts, artifactLine(av))
	}
	for _, e := range events {
		in.Log = append(in.Log, formatEventLine(e, true))
	}
	if crewID != "" {
		in.Output = orchestrator.CrewOutput(ctx.WS.Root, store, m.Slug, crewID)
	}
	in.Stages = stageLines(m, brief)
	states, _ := m.StageProgress(brief)
	if st, ok := mission.StageFor(states, crewID); ok && crewID != "" && !st.Implicit {
		in.CrewStage = fmt.Sprintf("%d of %d: %s", st.N, len(states), oneLine(st.Text))
	}
	if trigger == "review" {
		if r, err := review.For(ctx.WS.Root, m.Slug).Latest(""); err == nil && r != nil {
			in.Review, in.ReviewRef = review.Render(r), review.RenderedRef(r.ID)
			for _, c := range m.Crew {
				if c.Capsule == r.Capsule && c.Role == "" {
					in.ReviewCrew = append(in.ReviewCrew, c.ID)
				}
			}
		}
	}
	// Crew that reported done left a handoff: it says what they did and
	// learned better than the end of their output.
	if c, ok := crewRecord(m, crewID); ok && trigger == "done" && c.HandedOff != nil {
		if data, err := os.ReadFile(store.HandoffPath(m.Slug, crewID)); err == nil {
			in.Handoff = string(data)
		}
	}
	return in
}

func firstNonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
