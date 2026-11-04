package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/ui"
	"github.com/spf13/cobra"
)

// briefingHangupDelay gives the briefing crew time to show ready's output
// before its session ends.
const briefingHangupDelay = "2"

// crewMayEdit reports whether the caller may change a mission's brief or
// title: always for a person, but for crew only while it is being briefed.
// Once briefed, the brief is the user's.
func crewMayEdit(m *mission.Mission) error {
	if os.Getenv(mission.EnvCrew) != "" && m.State != mission.Briefing {
		return errors.New("the brief belongs to the user once a mission is briefed; record findings with ws mission note")
	}
	return nil
}

func newMissionBriefCmd() *cobra.Command {
	var slug, set string
	cmd := &cobra.Command{
		Use:   "brief",
		Short: "Print a mission's brief, or replace it with --set",
		Long: `Print the mission's brief, or replace it:

  ws mission brief --set - <<'EOF'
  ## Goal
  ...
  EOF

Crew can only replace the brief while briefing a mission.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			store := missionStore(ctx)
			if set == "" {
				brief, err := store.ReadBrief(m.Slug)
				if err != nil {
					return err
				}
				_, err = os.Stdout.WriteString(brief)
				return err
			}
			if err := crewMayEdit(m); err != nil {
				return err
			}
			var data []byte
			if set == "-" {
				data, err = io.ReadAll(os.Stdin)
			} else {
				data, err = os.ReadFile(set)
			}
			if err != nil {
				return fmt.Errorf("reading brief: %w", err)
			}
			if strings.TrimSpace(string(data)) == "" {
				return errors.New("the new brief is empty")
			}
			if err := store.WriteBrief(m.Slug, string(data)); err != nil {
				return err
			}
			store.AppendLog(m.Slug, mission.Event{Kind: mission.EventNote, Text: "brief written"})
			fmt.Fprintf(ui.Stderr, "  %s brief for %s written\n", ui.Green.Render("✓"), m.Slug)
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	cmd.Flags().StringVar(&set, "set", "", "replace the brief from a file, or - for stdin")
	return cmd
}

func newMissionTitleCmd() *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "title <title>",
		Short: "Set a mission's title (its slug stays)",
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
			if err := crewMayEdit(m); err != nil {
				return err
			}
			title := strings.Join(args, " ")
			if err := missionStore(ctx).SetTitle(m.Slug, title); err != nil {
				return err
			}
			fmt.Fprintf(ui.Stderr, "  %s %s is now %q\n", ui.Green.Render("✓"), m.Slug, strings.TrimSpace(title))
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	return cmd
}

func newMissionReadyCmd() *cobra.Command {
	var slug, newSlug string
	var noCapsules bool
	cmd := &cobra.Command{
		Use:   "ready",
		Short: "Finish briefing a mission: it becomes ready for crew to work on",
		Long: `Finish briefing: checks the brief is written and the mission has capsules,
then marks it ready. Run by a briefing crew, it also ends that crew's session.

--slug renames the mission (only here: renaming mid-briefing would leave the
briefing crew's WS_MISSION pointing nowhere).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			if m.State != mission.Briefing {
				return fmt.Errorf("mission %s isn't being briefed", m.Slug)
			}
			store := missionStore(ctx)
			brief, _ := store.ReadBrief(m.Slug)
			if mission.IsUnwrittenBrief(brief, m.Title) {
				return errors.New("write the brief first: ws mission brief --set - <<'EOF' … EOF")
			}
			v := buildMissionViews(ctx.WS, nil, []*mission.Mission{m})[0]
			live := 0
			for _, c := range v.Capsules {
				if c.Exists {
					live++
				}
			}
			if live == 0 && !noCapsules {
				return errors.New("create the capsules the work needs first (ws lift <repo> <branch>, or ws dock <repo> <branch|PR>); pass --no-capsules if it really needs none")
			}

			// Record this briefing crew's end before it is hung up, so the
			// end reads "briefed" (the first recorded end wins).
			crewID := os.Getenv(mission.EnvCrew)
			pid := 0
			if c, ok := crewRecord(m, crewID); ok && c.Role == mission.RoleBriefing {
				pid = c.PID
				store.EndCrew(m.Slug, crewID, "briefed")
			}
			if err := store.MarkReady(m.Slug); err != nil {
				return err
			}
			final := m.Slug
			if newSlug != "" && newSlug != m.Slug {
				if err := store.Rename(m.Slug, newSlug); err != nil {
					return fmt.Errorf("mission is ready, but renaming failed: %w", err)
				}
				final = newSlug
			}
			got, _ := store.Load(final)
			title := m.Title
			if got != nil {
				title = got.Title
			}
			noun := "capsules"
			if live == 1 {
				noun = "capsule"
			}
			fmt.Fprintf(ui.Stderr, "  %s Mission %s is ready: %s · %d %s\n", ui.Green.Render("✓"), ui.Bold.Render(final), title, live, noun)
			if pid > 0 {
				fmt.Fprintf(ui.Stderr, "  %s this briefing session ends now; crew can be launched on the mission from ws deck\n", ui.Dim.Render("·"))
				hangUpLater(pid)
			}
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	cmd.Flags().StringVar(&newSlug, "slug", "", "rename the mission to this slug")
	cmd.Flags().BoolVar(&noCapsules, "no-capsules", false, "allow a mission with no capsules")
	return cmd
}

// hangUpLater ends a briefing crew's session (its process group) after a
// short delay, from a detached helper so this command can return first and
// the crew sees its output.
func hangUpLater(pid int) {
	script := fmt.Sprintf("sleep %s; kill -HUP -- -%d 2>/dev/null || kill -HUP %d 2>/dev/null", briefingHangupDelay, pid, pid)
	c := exec.Command("/bin/sh", "-c", script)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if c.Start() == nil {
		c.Process.Release()
	}
}
