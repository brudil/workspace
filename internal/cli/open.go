package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var editors = map[string]func(root string) *exec.Cmd{
	"cursor": func(root string) *exec.Cmd {
		return exec.Command("cursor", filepath.Join(root, "workspace.code-workspace"))
	},
	"code": func(root string) *exec.Cmd {
		return exec.Command("code", filepath.Join(root, "workspace.code-workspace"))
	},
	"cursor-agent": func(root string) *exec.Cmd {
		return exec.Command("cursor-agent", filepath.Join(root, "workspace.code-workspace"))
	},
	"idea": func(root string) *exec.Cmd {
		return exec.Command("idea", root)
	},
}

func newOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "open [editor]",
		Short:     "Open workspace in editor (default: cursor)",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"cursor", "code", "cursor-agent", "idea"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}

			editor := "cursor"
			if len(args) > 0 {
				editor = args[0]
			}

			factory, ok := editors[editor]
			if !ok {
				return fmt.Errorf("unknown editor %q (options: cursor, code, cursor-agent, idea)", editor)
			}

			c := factory(ctx.WS.Root)
			return c.Start()
		},
	}
}
