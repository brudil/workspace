package cli

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func newSiloCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "silo",
		Short: "Manage repo silos for stable runtime directories",
	}
	cmd.AddCommand(newSiloPointCmd())
	cmd.AddCommand(newSiloStopCmd())
	cmd.AddCommand(newSiloStatusCmd())
	cmd.AddCommand(newSiloWatchCmd())
	return cmd
}

func newSiloPointCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "point <repo> <capsule>",
		Short: "Point a repo's silo at a capsule",
		Args:  cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			switch len(args) {
			case 0:
				return completeRepoNames(cmd, args, toComplete)
			case 1:
				return completeWorktreeNames(0)(cmd, args, toComplete)
			default:
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}

			repo, err := ctx.ResolveRepo(args[0])
			if err != nil {
				return err
			}

			capsule := args[1]
			if capsule != workspace.GroundDir {
				capsule, err = ctx.ResolveCapsule(repo, capsule)
				if err != nil {
					return err
				}
			}

			siloDir := ctx.WS.SiloWorktree(repo)
			capsuleDir := filepath.Join(ctx.WS.RepoDir(repo), capsule)

			// Create .silo/ worktree if doesn't exist (detached HEAD to avoid branch conflicts)
			if _, err := os.Stat(siloDir); os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "  Creating silo for %s...\n", ctx.WS.FormatRepoName(repo))
				bareDir := ctx.WS.BareDir(repo)
				if err := workspace.GitWorktreeAddDetached(bareDir, siloDir, ctx.WS.DefaultBranch); err != nil {
					return fmt.Errorf("creating silo worktree: %w", err)
				}
			}

			// Update silo state
			if ctx.WS.Silo == nil {
				ctx.WS.Silo = make(map[string]string)
			}
			ctx.WS.Silo[repo] = capsule
			if err := config.SaveSilo(ctx.WS.Root, ctx.WS.Silo); err != nil {
				return fmt.Errorf("saving silo state: %w", err)
			}

			// Full sync
			fmt.Fprintf(os.Stderr, "  Syncing %s -> .silo...\n", capsule)
			if _, err := workspace.FullSync(capsuleDir, siloDir); err != nil {
				return fmt.Errorf("syncing: %w", err)
			}

			// Run after_create hook (precedence: ws.local.toml > ws.toml > ws.repo.toml)
			hook, hasHook := ctx.WS.AfterCreateHooks[repo]
			repoConfigPath := filepath.Join(ctx.WS.MainWorktree(repo), config.RepoFileName)
			repoCfg, err := config.ParseRepoConfig(repoConfigPath)
			if err != nil {
				return err
			}
			if !hasHook && repoCfg != nil && repoCfg.Capsule.AfterCreate != "" {
				hook = repoCfg.Capsule.AfterCreate
				hasHook = true
			}
			if hasHook {
				fmt.Fprintf(os.Stderr, "  Running after_create hook...\n")
				if err := workspace.RunHook(siloDir, hook, os.Stderr, os.Stderr); err != nil {
					fmt.Fprintf(os.Stderr, "  %s after_create hook failed: %v\n", ui.Orange.Render("⚠"), err)
				}
			}

			// Run after_switch hook from ws.repo.toml
			if repoCfg != nil && repoCfg.Silo.AfterSwitch != "" {
				fmt.Fprintf(os.Stderr, "  Running after_switch hook...\n")
				if err := workspace.RunHook(siloDir, repoCfg.Silo.AfterSwitch, os.Stderr, os.Stderr); err != nil {
					fmt.Fprintf(os.Stderr, "  %s after_switch hook failed: %v\n", ui.Orange.Render("⚠"), err)
				}
			}

			fmt.Fprintf(os.Stderr, "  %s Silo for %s now points at %s\n",
				ui.Green.Render("✓"), ctx.WS.FormatRepoName(repo), ui.TagDim.Render(capsule))
			return nil
		},
	}
}

func newSiloStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <repo>",
		Short: "Remove a repo's silo",
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeRepoNames(cmd, args, toComplete)
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			repo, err := ctx.ResolveRepo(args[0])
			if err != nil {
				return err
			}
			siloDir := ctx.WS.SiloWorktree(repo)
			if _, err := os.Stat(siloDir); os.IsNotExist(err) {
				return fmt.Errorf("no silo exists for %s", repo)
			}
			bareDir := ctx.WS.BareDir(repo)
			// Force remove since .silo/ may have build artifacts that make it "dirty"
			if err := workspace.GitWorktreeRemoveForce(bareDir, siloDir); err != nil {
				return fmt.Errorf("removing silo worktree: %w", err)
			}
			delete(ctx.WS.Silo, repo)
			if err := config.SaveSilo(ctx.WS.Root, ctx.WS.Silo); err != nil {
				return fmt.Errorf("saving silo state: %w", err)
			}
			fmt.Fprintf(os.Stderr, "  %s Removed silo for %s\n",
				ui.Green.Render("✓"), ctx.WS.FormatRepoName(repo))
			return nil
		},
	}
}

func newSiloStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show silo state for all repos",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			if len(ctx.WS.Silo) == 0 {
				fmt.Fprintf(os.Stderr, "No active silos.\n")
				return nil
			}
			lockPath := filepath.Join(ctx.WS.Root, ".silo.lock")
			watching := isWatcherRunning(lockPath)
			for _, repo := range ctx.WS.RepoNames {
				target, ok := ctx.WS.Silo[repo]
				if !ok {
					continue
				}
				status := "synced"
				if watching {
					status = "watching"
				}
				targetDir := filepath.Join(ctx.WS.RepoDir(repo), target)
				if _, err := os.Stat(targetDir); os.IsNotExist(err) {
					status = "target missing"
				}
				fmt.Fprintf(os.Stderr, "  %-20s %-20s (%s)\n",
					ctx.WS.FormatRepoName(repo), target, status)
			}
			return nil
		},
	}
}

func isWatcherRunning(lockPath string) bool {
	return workspace.IsLockHeld(lockPath)
}

func newSiloWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Watch and sync all active silos",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			if len(ctx.WS.Silo) == 0 {
				return fmt.Errorf("no active silos — use 'ws silo point' first")
			}
			lockPath := filepath.Join(ctx.WS.Root, ".silo.lock")
			if err := workspace.AcquireLockFile(lockPath); err != nil {
				return err
			}
			defer workspace.ReleaseLockFile(lockPath)

			stop := make(chan struct{})

			if ui.IsInteractive() {
				// Silence the logger in interactive mode — TUI handles display
				silentLogger := log.New(io.Discard, "", 0)
				watcher, err := workspace.NewSiloWatcher(ctx.WS, silentLogger)
				if err != nil {
					return err
				}

				syncCh := make(chan workspace.SyncEvent, 64)
				watcher.OnSync = func(ev workspace.SyncEvent) {
					select {
					case syncCh <- ev:
					default: // drop if UI is behind
					}
				}

				go func() {
					watcher.Watch(stop, ctx.WS.Silo)
					close(syncCh)
				}()

				m := newSiloWatchModel(ctx.WS, syncCh, watcher.FullResyncAll)
				p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
				if _, err := p.Run(); err != nil {
					close(stop)
					return err
				}
				close(stop)
				return nil
			}

			// Non-interactive: plain log output
			logger := log.New(os.Stderr, "", log.LstdFlags)
			watcher, err := workspace.NewSiloWatcher(ctx.WS, logger)
			if err != nil {
				return err
			}
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sigCh
				logger.Println("Shutting down...")
				close(stop)
			}()

			return watcher.Watch(stop, ctx.WS.Silo)
		},
	}
}
