package cli

import (
	"fmt"
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

func runCapsuleCreate(ctx *Context, repo string, branch string, createWorktree createCapsuleFn, successMsg string) error {
	capsuleName := workspace.CapsuleName(branch)
	capsulePath := filepath.Join(ctx.WS.RepoDir(repo), capsuleName)
	if _, err := os.Stat(capsulePath); err == nil {
		return fmt.Errorf("capsule %q already exists for %s", capsuleName, repo)
	}

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

	var copySkipped []string

	op := func(name string) (bool, error) {
		switch name {
		case "Aligning ground":
			if err := workspace.GitFetch(bareDir); err != nil {
				return false, err
			}
			groundDir := ctx.WS.MainWorktree(repo)
			return false, workspace.GitFFMerge(groundDir, "origin/"+ctx.WS.DefaultBranch)
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
		}
		return false, nil
	}

	if ui.IsInteractive() {
		opModel := newOperationModel(stepNames, nil, op, false, true)
		hookDirFn := func() string { return filepath.Join(ctx.WS.RepoDir(repo), capsule) }
		m := newCapsuleCreateModel(opModel, hasHook, hookDirFn, hook)
		p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
		final, err := p.Run()
		if err != nil {
			return err
		}
		if fm, ok := final.(capsuleCreateModel); ok {
			hookErr = fm.HookErr()
		}
	} else {
		results := runOperationSync(stepNames, nil, op, true)
		fprintResults(os.Stderr, results)
		if hasHook && capsule != "" {
			wtPath := filepath.Join(ctx.WS.RepoDir(repo), capsule)
			if err := workspace.RunHook(wtPath, hook, os.Stderr, os.Stderr); err != nil {
				hookErr = err
			}
		}
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
