package cli

import (
	"fmt"
	"os"

	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var fix bool

	cmd := &cobra.Command{
		Use:               "doctor",
		Short:             "Check workspace health",
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}

			categories := ctx.WS.Doctor()
			hasFail := printDoctorResults(categories, fix)

			if hasFail {
				return fmt.Errorf("doctor found issues")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&fix, "fix", false, "Auto-fix safe issues")
	return cmd
}

func printDoctorResults(categories []workspace.CheckCategory, fix bool) bool {
	hasFail := false

	for i, cat := range categories {
		if i > 0 {
			fmt.Fprintln(os.Stderr)
		}
		fmt.Fprintln(os.Stderr, ui.Bold.Render(cat.Name))

		for _, check := range cat.Checks {
			// Attempt fix if --fix and a Fix function exists
			if fix && check.Fix != nil && check.Status != workspace.CheckOK {
				if err := check.Fix(); err != nil {
					check.Detail = fmt.Sprintf("fix failed: %v", err)
				} else {
					check.Status = workspace.CheckOK
					check.Detail = "fixed"
				}
			}

			var icon, detail string
			switch check.Status {
			case workspace.CheckOK:
				icon = ui.Green.Render("✓")
				if check.Detail != "" {
					detail = " " + ui.Dim.Render("→ "+check.Detail)
				}
			case workspace.CheckWarn:
				icon = ui.Orange.Render("⚠")
				if check.Detail != "" {
					detail = " — " + check.Detail
				}
			case workspace.CheckFail:
				hasFail = true
				icon = ui.Red.Render("✗")
				if check.Detail != "" {
					detail = " — " + check.Detail
				}
			}
			fmt.Fprintf(os.Stderr, "  %s %s%s\n", icon, check.Name, detail)

			// Print hint if --fix and a hint exists
			if fix && check.FixHint != "" && check.Status != workspace.CheckOK {
				fmt.Fprintf(os.Stderr, "    %s %s\n", ui.Dim.Render("hint:"), check.FixHint)
			}
		}
	}

	return hasFail
}
