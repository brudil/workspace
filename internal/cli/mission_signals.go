package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/ui"
	"github.com/spf13/cobra"
)

// signallingCrew resolves the mission and crew a signal comes from: crew
// only, since these say something about the crew itself.
func signallingCrew(slug string) (*Context, *mission.Mission, mission.Crew, error) {
	ctx, err := LoadContext()
	if err != nil {
		return nil, nil, mission.Crew{}, err
	}
	id := os.Getenv(mission.EnvCrew)
	if id == "" {
		return nil, nil, mission.Crew{}, errors.New("this is for crew running in the deck (WS_CREW isn't set)")
	}
	m, err := resolveMission(ctx, slug)
	if err != nil {
		return nil, nil, mission.Crew{}, err
	}
	c, ok := crewRecord(m, id)
	if !ok {
		return nil, nil, mission.Crew{}, fmt.Errorf("mission %s has no crew %s", m.Slug, id)
	}
	if c.Role == mission.RoleBriefing {
		return nil, nil, mission.Crew{}, errors.New("you're briefing this mission: finish with ws mission ready")
	}
	return ctx, m, c, nil
}

func newMissionAskCmd() *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Crew: say you need the user's decision (shows in the deck and notifies)",
		Long: `For crew: flag that you need the user. Ask in your reply as usual, and run
this too, so the deck marks you as asking and notifies them. It clears when
they next talk to you.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, m, c, err := signallingCrew(slug)
			if err != nil {
				return err
			}
			q := strings.TrimSpace(strings.Join(args, " "))
			if err := missionStore(ctx).SetCrewStatus(m.Slug, c.ID, mission.CrewAsking, q); err != nil {
				return err
			}
			fmt.Fprintf(ui.Stderr, "  %s asked; the user has been notified\n", ui.Orange.Render("?"))
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	return cmd
}

func newMissionDoneCmd() *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "done <summary>",
		Short: "Crew: say you've finished your part, with a short summary",
		Long: `For crew: report that your part of the mission is finished. ws finds the
PRs from the mission's capsules itself; record other outputs (a proposal
or ADR you wrote, notes, a link) with ws mission artifact add <file|url>
--kind <kind> "label".`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, m, c, err := signallingCrew(slug)
			if err != nil {
				return err
			}
			summary := strings.TrimSpace(strings.Join(args, " "))
			if err := missionStore(ctx).SetCrewStatus(m.Slug, c.ID, mission.CrewDone, summary); err != nil {
				return err
			}
			if c.Role != mission.RoleAnswer { // its answer already ran the orchestrator
				orchestrateSpawner(ctx.WS.Root, m.Slug, c.ID, "done")
			}
			fmt.Fprintf(ui.Stderr, "  %s done; the user has been notified\n", ui.Green.Render("✓"))
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	return cmd
}

func newMissionArtifactCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artifact",
		Short: "Record what a mission produced or relies on: files, PRs, links",
		Long: `Artifacts point at what a mission produced or relies on, so the next crew
(and the orchestrator) can find it: ws mission show -f llm lists them with
where to read each one. ws finds the capsules' own PRs itself.

What you can record:
  a file path       classified by where it is: one of the mission's capsules
                    (repo/capsule:path, followed onto the default branch when
                    its PR merges), a repo's .ground (repo:path), or the
                    mission's private files folder (mission:path)
  repo:path         a file on a repo's default branch
  repo#N, PR URL    a PR or issue
  https://…         anything else

Kinds say what it is: proposal, adr, note, doc, plan, handoff, pr, issue,
link, or any lowercase word.`,
	}
	var slug, kind string
	add := &cobra.Command{
		Use:   "add <file|ref|url> [label]",
		Short: "Record an artifact (adding it again updates its kind and label)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			if kind != "" && !kindShape.MatchString(kind) {
				return fmt.Errorf("kind %q: use one lowercase word, like proposal, adr, note, doc or link", kind)
			}
			r, err := normalizeArtifact(ctx, m, args[0])
			if err != nil {
				return err
			}
			if kind == "" {
				kind = defaultKind(r)
			}
			a := mission.Artifact{Ref: r.String(), Kind: kind, Label: strings.TrimSpace(strings.Join(args[1:], " ")), Crew: os.Getenv(mission.EnvCrew)}
			store := missionStore(ctx)
			if err := store.AddArtifact(m.Slug, a); err != nil {
				return err
			}
			fresh, _ := store.Load(m.Slug)
			v := buildMissionViewsFrom(ctx.WS, nil, []*mission.Mission{fresh})[0]
			for _, av := range resolveArtifacts(ctx.WS, store, v) {
				if av.Ref == a.Ref {
					fmt.Fprintf(ui.Stderr, "  %s recorded on %s: %s\n", ui.Green.Render("✓"), m.Slug, artifactLine(av))
				}
			}
			return nil
		},
	}
	add.Flags().StringVar(&kind, "kind", "", "what it is: proposal, adr, note, doc, plan, handoff, pr, issue, link… (default: pr, link or doc)")
	addMissionFlag(add, &slug)
	rm := &cobra.Command{
		Use:   "rm <ref>",
		Short: "Forget an artifact, by its reference as ws mission show lists it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			if err := missionStore(ctx).RemoveArtifact(m.Slug, strings.TrimSpace(args[0]), os.Getenv(mission.EnvCrew)); err != nil {
				return err
			}
			fmt.Fprintf(ui.Stderr, "  %s removed from %s\n", ui.Green.Render("✓"), m.Slug)
			return nil
		},
	}
	addMissionFlag(rm, &slug)
	cmd.AddCommand(add, rm)
	return cmd
}

// raisedBy is who a proposal comes from: the orchestrator, crew, or the user.
func raisedBy() string {
	if os.Getenv(mission.EnvRole) == mission.RoleOrchestrator {
		return mission.RoleOrchestrator
	}
	if c := os.Getenv(mission.EnvCrew); c != "" {
		return c
	}
	return "you"
}

func newMissionFlagCmd() *cobra.Command {
	var slug string
	var clear bool
	var options []string
	cmd := &cobra.Command{
		Use:   "flag <question for the user>",
		Short: "Mark a mission as needing the user, with a question (and answers to pick from)",
		Long: `Mark a mission as needing the user: the deck shows the question under
Needs you, with any --option answers to pick from, and the user answers it
there or with ws mission answer. The orchestrator then runs again with
their answer and acts on it. Give each option as a short instruction you'd
act on ("Commit both and open PRs"); the user can always write their own.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if !clear && len(args) == 0 {
				return errors.New("say why the user is needed")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			text := strings.Join(args, " ")
			if clear {
				text = ""
			}
			store := missionStore(ctx)
			if err := store.SetFlag(m.Slug, text, raisedBy(), options...); err != nil {
				return err
			}
			if clear && raisedBy() == "you" {
				store.UserActed(m.Slug) // as dismissing it in the deck does
			}
			if clear {
				fmt.Fprintf(ui.Stderr, "  %s flag cleared on %s\n", ui.Green.Render("✓"), m.Slug)
			} else {
				fmt.Fprintf(ui.Stderr, "  %s %s flagged for the user\n", ui.Orange.Render("!"), m.Slug)
			}
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the flag")
	cmd.Flags().StringArrayVar(&options, "option", nil, "an answer the user can pick (repeatable)")
	return cmd
}

func newMissionAnswerCmd() *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "answer <your answer>",
		Short: "Answer the question a mission is flagged with; the orchestrator acts on it",
		Long: `Answer the question the orchestrator flagged a mission with (the deck does
this with ⏎ on the mission). The flag clears, your answer goes in the log,
and the orchestrator runs again with it, to dispatch crew, resume one, or
conclude as you said. Crew you asked to answer it (from the deck) answer
the same way, and that's their part done.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			by := raisedBy()
			if by == mission.RoleOrchestrator {
				return errors.New("the orchestrator asks with ws mission flag; the user, or crew they ask, answers")
			}
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			if by != "you" {
				if c, ok := crewRecord(m, by); !ok || c.Role != mission.RoleAnswer {
					return errors.New("only the user answers a flag, or crew they asked to answer it; crew ask the user with ws mission ask")
				}
			}
			store := missionStore(ctx)
			answer := strings.Join(args, " ")
			if _, err := store.AnswerFlag(m.Slug, answer, by); err != nil {
				return err
			}
			if by != "you" {
				// Answering was its whole job: it has handed back.
				store.SetCrewStatus(m.Slug, by, mission.CrewDone, "answered: "+answer)
			}
			fmt.Fprintf(ui.Stderr, "  %s answered on %s; %s\n", ui.Green.Render("✓"), m.Slug, afterAnswer(ctx, m.Slug))
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	return cmd
}

// afterAnswer hands a flag's answer to the orchestrator, and says what
// happens next.
func afterAnswer(ctx *Context, slug string) string {
	if !ctx.Config.OrchestratorEnabled() {
		return "the orchestrator is off ([orchestrator] enabled = false), so act on it yourself"
	}
	orchestrateSpawner(ctx.WS.Root, slug, "", "answer")
	return "the orchestrator is acting on it"
}

func newMissionResumeWithCmd() *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "resume-with <crew> <message>",
		Short: "Suggest resuming crew with a message (the deck offers it on R)",
		Long: `Suggest that ended crew be resumed to continue its work, with a message
saying what's left ("the error path has no test"). The deck offers it: R
resumes that crew and types the message in.

Only crew that recorded a conversation can be resumed; for anything else,
raise a follow-up with ws mission new.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			c, ok := crewRecord(m, args[0])
			if !ok {
				var ids []string
				for _, c := range m.Crew {
					ids = append(ids, c.ID)
				}
				return fmt.Errorf("mission %s has no crew %s (it has: %s)", m.Slug, args[0], strings.Join(ids, ", "))
			}
			if c.Role == mission.RoleBriefing {
				return fmt.Errorf("%s briefed the mission; resume work crew, or raise a follow-up with ws mission new", c.ID)
			}
			if c.Conversation == "" {
				return fmt.Errorf("%s recorded no conversation, so it can't be resumed; raise a follow-up with ws mission new instead", c.ID)
			}
			msg := strings.TrimSpace(strings.Join(args[1:], " "))
			if err := missionStore(ctx).SetResumeWith(m.Slug, c.ID, msg); err != nil {
				return err
			}
			fmt.Fprintf(ui.Stderr, "  %s the deck will offer to resume %s with that message\n", ui.Green.Render("✓"), c.ID)
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	return cmd
}

func newMissionConcludeCmd() *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "conclude <why it's finished>",
		Short: "Suggest the mission is finished (the user archives it)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			if err := missionStore(ctx).SetConclude(m.Slug, strings.Join(args, " "), raisedBy()); err != nil {
				return err
			}
			fmt.Fprintf(ui.Stderr, "  %s suggested concluding %s; the user decides\n", ui.Green.Render("✓"), m.Slug)
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	return cmd
}
