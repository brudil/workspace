package cli

import (
	"fmt"
	"os"

	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "doctor",
		Short:             "Check workspace health",
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}

			categories := ctx.WS.Doctor()
			hasFail := printDoctorResults(categories)

			if hasFail {
				return fmt.Errorf("doctor found issues")
			}
			return nil
		},
	}
}

func printDoctorResults(categories []workspace.CheckCategory) bool {
	hasFail := false

	for i, cat := range categories {
		if i > 0 {
			fmt.Fprintln(os.Stderr)
		}
		fmt.Fprintln(os.Stderr, ui.Bold.Render(cat.Name))

		for _, check := range cat.Checks {
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
		}
	}

	return hasFail
}
