package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/ide"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

type createCapsuleFn func() (string, error)

func runCapsuleCreate(ctx *Context, repo string, createWorktree createCapsuleFn, successMsg string) error {
	var capsule string
	var hookErr error
	bareDir := ctx.WS.BareDir(repo)
	hook, hasHook := ctx.WS.AfterCreateHooks[repo]

	repoConfigPath := filepath.Join(ctx.WS.MainWorktree(repo), config.RepoFileName)
	repoCfg, err := config.ParseRepoConfig(repoConfigPath)
	if err != nil {
		return err
	}

	hasCopy := repoCfg != nil && len(repoCfg.Capsule.CopyFromGround) > 0

	if !hasHook && repoCfg != nil && repoCfg.Capsule.AfterCreate != "" {
		hook = repoCfg.Capsule.AfterCreate
		hasHook = true
	}

	stepNames := []string{"Aligning ground", "Making capsule"}
	if hasCopy {
		stepNames = append(stepNames, "Copying files")
	}
	if hasHook {
		stepNames = append(stepNames, "Running hooks")
	}

	var copySkipped []string

	op := func(name string) (bool, error) {
		switch name {
		case "Aligning ground":
			return false, workspace.GitFetch(bareDir)
		case "Making capsule":
			c, err := createWorktree()
			if err != nil {
				return false, err
			}
			capsule = c
			return false, nil
		case "Copying files":
			wtPath := filepath.Join(ctx.WS.RepoDir(repo), capsule)
			groundDir := ctx.WS.MainWorktree(repo)
			s, err := workspace.CopyFromGround(groundDir, wtPath, repoCfg.Capsule.CopyFromGround)
			copySkipped = s
			return false, err
		case "Running hooks":
			wtPath := filepath.Join(ctx.WS.RepoDir(repo), capsule)
			if err := workspace.RunHook(wtPath, hook, io.Discard, io.Discard); err != nil {
				hookErr = err
			}
			return false, nil
		}
		return false, nil
	}

	if ui.IsInteractive() {
		m := newOperationModel(stepNames, nil, op, false)
		p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
		if _, err := p.Run(); err != nil {
			return err
		}
	} else {
		results := runOperationSync(stepNames, nil, op)
		fprintResults(os.Stderr, results)
	}

	if capsule == "" {
		return fmt.Errorf("failed to create capsule")
	}

	wtPath := filepath.Join(ctx.WS.RepoDir(repo), capsule)

	if hookErr != nil {
		fmt.Fprintf(os.Stderr, "  %s after_create hook failed: %v\n", ui.Orange.Render("⚠"), hookErr)
	}

	if len(copySkipped) > 0 {
		fmt.Fprintf(os.Stderr, "  %s copy_from_ground: skipped missing files: %s\n",
			ui.Orange.Render("⚠"), strings.Join(copySkipped, ", "))
	}

	if err := ctx.WS.Board(repo, capsule); err == nil {
		config.SaveBoarded(ctx.WS.Root, ctx.WS.Boarded)
		if err := ide.Regenerate(ctx.WS.Root, ctx.WS.Boarded, ctx.WS.DisplayNames, ctx.WS.Org); err != nil {
			fmt.Fprintf(os.Stderr, "  %s workspace files: %v\n", ui.Orange.Render("⚠"), err)
		}
	}

	fmt.Fprintf(os.Stderr, "\n%s %s %s is ready for work.\n", successMsg, ctx.WS.FormatRepoName(repo), ui.TagDim.Render(capsule))
	fmt.Printf("cd %s\n", shellQuote(wtPath))
	return nil
}
