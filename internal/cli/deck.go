package cli

import (
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/brudil/workspace/internal/crew"
	"github.com/brudil/workspace/internal/deckserver"
	"github.com/spf13/cobra"
)

func newDeckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deck [mission]",
		Short: "Missions dashboard: briefs, capsules and crew in one TUI",
		Long: `Open the deck: the selected mission on the left, every mission on the
right. Create missions, edit briefs, and attach or detach capsules.

Pass a mission slug to open with it selected.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeMissionSlugs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			if err := applyGroupFlag(cmd, ctx.WS); err != nil {
				return err
			}
			m := newDeckModel(ctx.WS, ctx.GitHub, firstArg(args))
			m.roles = agentRoles(ctx.Config)
			if deckServerEnabled() {
				m.host = deckserver.NewHost(ctx.WS.Root)
			}
			// Close crew records a deck server left open when it died.
			m.store.ReconcileCrew(crew.ProcessAlive)
			final, err := tea.NewProgram(m, tea.WithOutput(os.Stderr)).Run()
			if fm, ok := final.(deckModel); ok && fm.mouseOff {
				os.Stderr.WriteString(altScrollRestore) // left with M on: give the terminal its wheel back
			}
			m.stopLeftovers()
			return err
		},
	}
	addGroupFlag(cmd)
	cmd.AddCommand(newDeckServerCmd())
	return cmd
}
