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
