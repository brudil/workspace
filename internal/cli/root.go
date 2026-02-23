package cli

import (
	"github.com/spf13/cobra"
)

func NewRootCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Multi-repo workspace manager",
		Long:  "Manage multi-repo workspaces with git worktrees and symlinks.",
	}

	cmd.Version = version

	// Default command: show status when no subcommand given
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return newStatusCmd().RunE(cmd, args)
	}

	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newSetupCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newLiftCmd())
	cmd.AddCommand(newDockCmd())
	cmd.AddCommand(newBurnCmd())
	cmd.AddCommand(newOpenCmd())
	cmd.AddCommand(newMCCmd())
	cmd.AddCommand(newDebriefCmd())
	cmd.AddCommand(newPromptCmd())
	cmd.AddCommand(newUpgradeCmd())
	cmd.AddCommand(newDoctorCmd())
	cmd.AddCommand(newJumpCmd())
	cmd.AddCommand(newShellInitCmd())
	cmd.AddCommand(newBoardCmd())
	cmd.AddCommand(newUnboardCmd())

	return cmd
}
