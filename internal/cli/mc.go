package cli

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
)

// --- cobra command ---

func newMCCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "mc",
		Short:             "Mission control — interactive workspace dashboard",
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			if err := applyGroupFlag(cmd, ctx.WS); err != nil {
				return err
			}

			cwd, _ := os.Getwd()
			m := newMCModel(ctx.WS, ctx.GitHub, cwd)
			p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
			finalModel, err := p.Run()
			if err != nil {
				return err
			}

			if fm, ok := finalModel.(mcModel); ok && fm.jumpPath != "" {
				fmt.Printf("cd '%s'\n", fm.jumpPath)
			}
			return nil
		},
	}
	addGroupFlag(cmd)
	return cmd
}
