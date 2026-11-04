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
PRs from the mission's capsules itself; record other outputs with
ws mission artifact add <url> "label".`,
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
			orchestrateSpawner(ctx.WS.Root, m.Slug, c.ID, "done")
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
		Short: "Record outputs of a mission that ws can't find itself",
	}
	var slug string
	add := &cobra.Command{
		Use:   "add <url> [label]",
		Short: "Record a link the mission produced (a doc, a PR outside its capsules, …)",
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
			label := strings.TrimSpace(strings.Join(args[1:], " "))
			if err := missionStore(ctx).AddArtifact(m.Slug, args[0], label, os.Getenv(mission.EnvCrew)); err != nil {
				return err
			}
			fmt.Fprintf(ui.Stderr, "  %s recorded on %s\n", ui.Green.Render("✓"), m.Slug)
			return nil
		},
	}
	addMissionFlag(add, &slug)
	cmd.AddCommand(add)
	return cmd
}

// EnvRole names a special role a process plays for a mission; the orchestrator
// sets it to "orchestrator".
const EnvRole = "WS_ROLE"

// roleOrchestrator is WS_ROLE for the orchestrator.
const roleOrchestrator = "orchestrator"

// raisedBy is who a proposal comes from: the orchestrator, crew, or the user.
func raisedBy() string {
	if os.Getenv(EnvRole) == roleOrchestrator {
		return roleOrchestrator
	}
	if c := os.Getenv(mission.EnvCrew); c != "" {
		return c
	}
	return "you"
}

func newMissionFlagCmd() *cobra.Command {
	var slug string
	var clear bool
	cmd := &cobra.Command{
		Use:   "flag <why the user is needed>",
		Short: "Mark a mission as needing the user, with why (shown in the deck)",
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
			if err := store.SetFlag(m.Slug, text, raisedBy()); err != nil {
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
	return cmd
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
