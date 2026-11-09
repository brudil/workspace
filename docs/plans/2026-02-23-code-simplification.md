# Code Simplification Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Eliminate duplicated code, dead code, and unnecessary complexity across the ws codebase.

**Architecture:** Each task is a self-contained refactoring that can be done independently. Tasks are ordered from lowest risk (dead code deletion) to highest complexity (shared helper extraction). All existing tests must continue to pass; new tests are added where behavior is extracted into new functions.

**Tech Stack:** Go, bubbletea, cobra, toml

---

### Task 1: Delete dead `printResults` function

**Files:**
- Modify: `internal/cli/progress.go:211-213`

**Step 1: Verify no callers exist**

Run: `grep -r 'printResults' internal/`

Confirm: Only the definition at `progress.go:211` and `fprintResults` calls appear. No callers of `printResults`.

**Step 2: Delete the function**

Remove lines 211-213 from `internal/cli/progress.go`:

```go
// Delete this:
func printResults(results []repoEntry) {
	fprintResults(os.Stderr, results)
}
```

Also remove `"os"` from the import if it's no longer used (check other references in the file first — `cloneRepos` uses `os.Stderr` so it will still be needed, skip this).

**Step 3: Run tests**

Run: `just test`
Expected: All tests pass.

**Step 4: Commit**

```bash
git add internal/cli/progress.go
git commit -m "chore: remove dead printResults function"
```

---

### Task 2: Delete dead `github.PRForBranch` function

**Files:**
- Modify: `internal/github/github.go:128-140`

**Step 1: Verify no callers exist**

Run: `grep -r 'PRForBranch' internal/`

Confirm: Only the definition appears. No callers.

**Step 2: Delete the function**

Remove lines 128-140 from `internal/github/github.go`:

```go
// Delete this:
func PRForBranch(org, repo, branch string) (*PR, error) {
	prs, err := PRsForRepo(org, repo)
	if err != nil {
		return nil, err
	}
	for _, pr := range prs {
		if pr.HeadRefName == branch {
			return &pr, nil
		}
	}
	return nil, nil
}
```

**Step 3: Run tests**

Run: `just test`
Expected: All tests pass.

**Step 4: Commit**

```bash
git add internal/github/github.go
git commit -m "chore: remove dead PRForBranch function"
```

---

### Task 3: Eliminate double `git status` in `FindAllCapsules`

**Files:**
- Modify: `internal/workspace/stale.go:83-88`

**Step 1: Replace `GitIsDirty` + `GitDirtyCount` with single `GitDirtyCount` call**

In `internal/workspace/stale.go`, replace lines 83-88:

```go
			dirty := GitIsDirty(wtPath)

			var dirtyCount int
			if dirty {
				dirtyCount = GitDirtyCount(wtPath)
			}
```

With:

```go
			dirtyCount := GitDirtyCount(wtPath)
			dirty := dirtyCount > 0
```

**Step 2: Run tests**

Run: `just test`
Expected: All tests pass (including `internal/workspace/stale_test.go`).

**Step 3: Commit**

```bash
git add internal/workspace/stale.go
git commit -m "fix: avoid double git-status call in FindAllCapsules"
```

---

### Task 4: Rename `repoEntry` in `status_llm.go` to avoid same-package shadowing

**Files:**
- Modify: `internal/cli/status_llm.go:80`

**Step 1: Rename the type**

In `internal/cli/status_llm.go`, the function-scoped `repoEntry` struct (line 80) shadows the package-level `repoEntry` in `progress.go`. Rename it to `llmRepoEntry`:

Replace line 80:

```go
	type repoEntry struct {
```

With:

```go
	type llmRepoEntry struct {
```

Also update all references within `runStatusLLM` — the variable declarations on lines 85-93:

Replace:

```go
	repos := make([]repoEntry, len(outlines))
	for i, o := range outlines {
		repos[i] = repoEntry{
```

With:

```go
	repos := make([]llmRepoEntry, len(outlines))
	for i, o := range outlines {
		repos[i] = llmRepoEntry{
```

**Step 2: Run tests**

Run: `just test`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add internal/cli/status_llm.go
git commit -m "chore: rename shadowed repoEntry type in status_llm"
```

---

### Task 5: Unify `newBoardCmd` / `newUnboardCmd` in `board.go`

**Files:**
- Modify: `internal/cli/board.go`

**Step 1: Extract shared helper**

Replace the entire contents of `board.go` with a unified implementation. The two commands differ only in:
- Use string ("board" vs "unboard")
- Short description
- The operation: `ctx.WS.Board()` vs `ctx.WS.Unboard()`
- Success message verb: "Boarded" vs "Unboarded"

Replace both `newBoardCmd` and `newUnboardCmd` with a single `newBoardToggleCmd` factory, then call it twice:

```go
package cli

import (
	"fmt"
	"os"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/ide"
	"github.com/brudil/workspace/internal/ui"
	"github.com/spf13/cobra"
)

func newBoardCmd() *cobra.Command {
	return newBoardToggleCmd("board", "Add a capsule to IDE workspace files", "Boarded",
		func(ctx *Context, repo, capsule string) error { return ctx.WS.Board(repo, capsule) })
}

func newUnboardCmd() *cobra.Command {
	return newBoardToggleCmd("unboard", "Remove a capsule from IDE workspace files", "Unboarded",
		func(ctx *Context, repo, capsule string) error { return ctx.WS.Unboard(repo, capsule) })
}

func newBoardToggleCmd(use, short, verb string, op func(*Context, string, string) error) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <repo> <capsule>",
		Short: short,
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

			if err := op(ctx, repo, capsule); err != nil {
				return err
			}

			if err := config.SaveBoarded(ctx.WS.Root, ctx.WS.Boarded); err != nil {
				return fmt.Errorf("saving board state: %w", err)
			}

			if err := ide.Regenerate(ctx.WS.Root, ctx.WS.Boarded, ctx.WS.DisplayNames, ctx.WS.Org); err != nil {
				fmt.Fprintf(os.Stderr, "  %s workspace files: %v\n", ui.Orange.Render("⚠"), err)
			}

			fmt.Fprintf(os.Stderr, "  %s %s %s %s\n", ui.Green.Render("✓"), verb, ctx.WS.FormatRepoName(repo), ui.TagDim.Render(capsule))
			return nil
		},
	}
}
```

**Step 2: Run tests**

Run: `just test`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add internal/cli/board.go
git commit -m "refactor: unify board/unboard commands into shared factory"
```

---

### Task 6: Unify `doBoard` / `doUnboard` in `mc_keys.go`

**Files:**
- Modify: `internal/cli/mc_keys.go:234-274`

**Step 1: Replace both methods with a single `doBoardToggle`**

Replace `doBoard()` (lines 234-253) and `doUnboard()` (lines 255-274) with:

```go
func (m mcModel) doBoardToggle() (mcModel, tea.Cmd) {
	row := m.rows[m.cursor]
	if row.kind != rowWorktree {
		return m, nil
	}
	if row.wt == m.ws.DefaultBranch || row.wt == workspace.GroundDir {
		return m, nil
	}

	var err error
	if row.isBoarded {
		err = m.ws.Unboard(row.repo, row.wt)
	} else {
		err = m.ws.Board(row.repo, row.wt)
	}
	if err != nil {
		return m, nil
	}

	config.SaveBoarded(m.ws.Root, m.ws.Boarded)
	ide.Regenerate(m.ws.Root, m.ws.Boarded, m.ws.DisplayNames, m.ws.Org)
	for i := range m.rows {
		if m.rows[i].kind == rowWorktree && m.rows[i].repo == row.repo {
			m.rows[i].isBoarded = m.ws.IsBoarded(m.rows[i].repo, m.rows[i].wt)
		}
	}
	return m, nil
}
```

**Step 2: Update the caller in `handleKey`**

In the same file, update the `"b"` key handler (around line 132-139). Replace:

```go
	case "b":
		row := m.rows[m.cursor]
		if row.kind != rowWorktree {
			return m, nil
		}
		if row.isBoarded {
			return m.doUnboard()
		}
		return m.doBoard()
```

With:

```go
	case "b":
		return m.doBoardToggle()
```

**Step 3: Delete old `doBoard` and `doUnboard` methods**

Remove the old `doBoard()` (lines 234-253) and `doUnboard()` (lines 255-274) methods entirely.

**Step 4: Run tests**

Run: `just test`
Expected: All tests pass (particularly `mc_keys_test.go` — `TestHandleKey_Board_TogglesIsBoarded` and `TestHandleKey_Board_SkipsDefaultBranch`).

**Step 5: Commit**

```bash
git add internal/cli/mc_keys.go
git commit -m "refactor: unify doBoard/doUnboard into doBoardToggle"
```

---

### Task 7: Extract capsule creation helper from `lift.go` / `dock.go`

**Files:**
- Create: `internal/cli/capsule_create.go`
- Modify: `internal/cli/lift.go`
- Modify: `internal/cli/dock.go`

This is the largest duplication. Lines 54-146 of `lift.go` and lines 117-209 of `dock.go` are nearly identical. The only differences:
1. The "Making capsule" step calls `CreateLiftWorktree` vs `CreateDockWorktree`
2. The success message: "Lift off!" vs "Docked!"

**Step 1: Create the shared helper**

Create `internal/cli/capsule_create.go`:

```go
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

// createCapsuleFn creates a worktree and returns the capsule directory name.
type createCapsuleFn func() (string, error)

// runCapsuleCreate runs the shared capsule creation flow for lift and dock.
func runCapsuleCreate(ctx *Context, repo string, createWorktree createCapsuleFn, successMsg string) error {
	var capsule string
	var hookErr error
	bareDir := ctx.WS.BareDir(repo)
	hook, hasHook := ctx.WS.AfterCreateHooks[repo]

	// Load per-repo config from .ground/ws.repo.toml
	repoConfigPath := filepath.Join(ctx.WS.MainWorktree(repo), config.RepoFileName)
	repoCfg, err := config.ParseRepoConfig(repoConfigPath)
	if err != nil {
		return err
	}

	hasCopy := repoCfg != nil && len(repoCfg.Capsule.CopyFromGround) > 0

	// Determine effective hook: ws.toml after_create wins, ws.repo.toml is fallback
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
			return false, nil // non-fatal
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
```

**Step 2: Simplify `lift.go`**

Replace `internal/cli/lift.go` — the RunE body (lines 54-146) becomes:

```go
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}

			repo, err := ctx.ResolveRepo(args[0])
			if err != nil {
				return err
			}

			branch := args[1]

			base := "origin/" + ctx.WS.DefaultBranch
			if len(args) == 3 {
				base = args[2]
			}

			return runCapsuleCreate(ctx, repo, func() (string, error) {
				return ctx.WS.CreateLiftWorktree(repo, branch, base)
			}, "Lift off!")
		},
```

The imports in `lift.go` simplify to just `cobra` and nothing else (the others move to `capsule_create.go`). Actually we need to keep the import for `LoadContext` etc. Let's be precise:

```go
package cli

import (
	"github.com/spf13/cobra"
)
```

**Step 3: Simplify `dock.go`**

Replace the RunE body in `dock.go` — everything from line 117 (`var capsule string`) through line 209 becomes:

```go
			return runCapsuleCreate(ctx, repo, func() (string, error) {
				return ctx.WS.CreateDockWorktree(repo, branch)
			}, "Docked!")
```

The imports simplify to:

```go
package cli

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/brudil/workspace/internal/github"
	"github.com/spf13/cobra"
)
```

**Step 4: Run tests**

Run: `just test`
Expected: All tests pass (particularly `dock_test.go`).

**Step 5: Commit**

```bash
git add internal/cli/capsule_create.go internal/cli/lift.go internal/cli/dock.go
git commit -m "refactor: extract shared capsule creation flow from lift/dock"
```

---

### Task 8: Extract PR lookup helper

**Files:**
- Create: `internal/cli/pr_lookup.go`
- Modify: `internal/cli/status.go:279-286`
- Modify: `internal/cli/status_json.go:122-128`
- Modify: `internal/cli/status_llm.go:107-115`

**Step 1: Create the helper**

Create `internal/cli/pr_lookup.go`:

```go
package cli

import "github.com/brudil/workspace/internal/github"

// lookupPR finds a PR by branch name, falling back to worktree name.
func lookupPR(prs map[string]*github.PR, branch, name string) *github.PR {
	if branch != "" {
		if pr := prs[branch]; pr != nil {
			return pr
		}
	}
	return prs[name]
}
```

**Step 2: Replace the pattern in `status.go`**

In `internal/cli/status.go`, replace lines 279-286:

```go
			var pr *github.PR
			if repo.prs != nil {
				if wt.branch != "" {
					pr = repo.prs[wt.branch]
				}
				if pr == nil {
					pr = repo.prs[wt.name]
				}
			}
```

With:

```go
			var pr *github.PR
			if repo.prs != nil {
				pr = lookupPR(repo.prs, wt.branch, wt.name)
			}
```

**Step 3: Replace the pattern in `status_json.go`**

In `internal/cli/status_json.go`, replace lines 122-128:

```go
			var pr *github.PR
			if wt.Branch != "" {
				pr = prs[wt.Branch]
			}
			if pr == nil {
				pr = prs[wt.Name]
			}
```

With:

```go
			pr := lookupPR(prs, wt.Branch, wt.Name)
```

**Step 4: Replace the pattern in `status_llm.go`**

In `internal/cli/status_llm.go`, replace lines 108-114:

```go
			var pr *github.PR
			if wt.status.Branch != "" {
				pr = prs[wt.status.Branch]
			}
			if pr == nil {
				pr = prs[wt.name]
			}
```

With:

```go
			pr := lookupPR(prs, wt.status.Branch, wt.name)
```

**Step 5: Run tests**

Run: `just test`
Expected: All tests pass.

**Step 6: Commit**

```bash
git add internal/cli/pr_lookup.go internal/cli/status.go internal/cli/status_json.go internal/cli/status_llm.go
git commit -m "refactor: extract lookupPR helper for branch-then-name lookup"
```

---

### Task 9: Extract `config.UpdateLocal` helper

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/cli/init.go`

**Step 1: Add `UpdateLocal` to config package**

Add this function to `internal/config/config.go` (after `SaveBoarded`):

```go
// UpdateLocal reads ws.local.toml, applies a mutation, and writes it back.
func UpdateLocal(root string, fn func(*LocalConfig)) error {
	path := filepath.Join(root, LocalFileName)

	var local LocalConfig
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &local); err != nil {
			return fmt.Errorf("parsing %s: %w", LocalFileName, err)
		}
	}

	fn(&local)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(local)
}
```

**Step 2: Rewrite `SaveBoarded` to use `UpdateLocal`**

Replace the existing `SaveBoarded` function:

```go
func SaveBoarded(root string, boarded map[string][]string) error {
	return UpdateLocal(root, func(local *LocalConfig) {
		local.Boarded = boarded
	})
}
```

**Step 3: Rewrite `writeLocalGitConfig` in `init.go`**

Replace the `writeLocalGitConfig` function in `internal/cli/init.go`:

```go
func writeLocalGitConfig(root, gitProtocol string) error {
	return config.UpdateLocal(root, func(local *config.LocalConfig) {
		local.Git = gitProtocol
	})
}
```

Remove the now-unused imports from `init.go`: `"github.com/BurntSushi/toml"` (check that it's not used elsewhere in the file — it isn't).

**Step 4: Run tests**

Run: `just test`
Expected: All tests pass (particularly `config_test.go` and `init_test.go`).

**Step 5: Commit**

```bash
git add internal/config/config.go internal/cli/init.go
git commit -m "refactor: extract UpdateLocal for ws.local.toml read-modify-write"
```

---

### Task 10: Move github package-level functions into `LiveClient` methods

**Files:**
- Modify: `internal/github/client.go`
- Modify: `internal/github/github.go`
- Modify: `internal/cli/mc_update.go`

Currently the `LiveClient` methods are trivial delegations to package-level functions, and `mcModel` bypasses the interface entirely by calling package functions directly. The fix: make `mcModel` use `github.Client` consistently, and move the implementation into the `LiveClient` methods (making the package-level functions call the methods instead, or just removing them).

However, some package-level functions (like `WritePRCache`, `ReadPRCache`, cache functions) are legitimately stateless utilities that don't belong on the interface. The interface methods (`PRsForRepo`, `MergedPRsForRepo`, `PRFromNumber`, `PRDetail`, `WorkflowRuns`) are the ones that need consolidation.

**Step 1: Add `github.Client` field to `mcModel`**

Find the `mcModel` struct definition. Add a `gh github.Client` field. Then find where `mcModel` is constructed (`newMCModel`) and thread through a `github.LiveClient{}`.

In `mc_update.go`, update `queryRepoPRs` (line 48-57) to use `m.gh` instead of calling `github.PRsForRepo` directly:

```go
func (m mcModel) queryRepoPRs(repoName string) tea.Cmd {
	org := m.ws.Org
	gh := m.gh
	return func() tea.Msg {
		prs, err := gh.PRsForRepo(org, repoName)
		if err == nil {
			github.WritePRCache(github.CacheDir(), org, repoName, prs)
		}
		return mcPRsMsg{repo: repoName, prs: prs, err: err}
	}
}
```

Update `fetchDetail` (line 475-515) to use `m.gh` instead of direct `github.MergedPRsForRepo`, `github.WorkflowRuns`, `github.PRDetail` calls. Capture `gh := m.gh` at the top of the closure-returning function and use `gh.MergedPRsForRepo(...)`, `gh.WorkflowRuns(...)`, `gh.PRDetail(...)`.

**Step 2: Thread `github.Client` through MC construction**

Find `newMCModel` and the `mc` command that constructs it. Add the `gh` field. This will involve reading the mc.go or wherever `newMCModel` is defined to determine exact signature.

**Step 3: Run tests**

Run: `just test`
Expected: All tests pass.

**Step 4: Commit**

```bash
git add internal/cli/mc_update.go internal/cli/mc.go
git commit -m "refactor: route MC github calls through Client interface"
```

---

### Task 11: Extract shared status collection helper

**Files:**
- Create: `internal/cli/status_collect.go`
- Modify: `internal/cli/status_json.go`
- Modify: `internal/cli/status_llm.go`

Both `runStatusJSON` and `runStatusLLM` implement the same parallel goroutine pattern: WaitGroup, goroutines per worktree for `QueryWorktreeStatus`, goroutines per repo for `PRsForRepo`, mutex-guarded collection.

**Step 1: Create the shared collection function**

Create `internal/cli/status_collect.go`:

```go
package cli

import (
	"path/filepath"
	"sync"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/workspace"
)

// statusData holds the parallel-collected worktree statuses and PR maps.
type statusData struct {
	// Statuses indexed by [repoIdx][wtIdx].
	statuses [][]workspace.WorktreeStatus
	// PRs keyed by repo name, then by headRefName.
	prsByRepo map[string]map[string]*github.PR
}

// collectStatusData runs worktree status queries and PR fetches in parallel
// for all repos described by outlines.
func collectStatusData(ws *workspace.Workspace, gh github.Client, outlines []workspace.RepoOutline) statusData {
	var mu sync.Mutex
	var wg sync.WaitGroup

	result := statusData{
		statuses:  make([][]workspace.WorktreeStatus, len(outlines)),
		prsByRepo: make(map[string]map[string]*github.PR),
	}

	for i, o := range outlines {
		if o.Err != nil {
			continue
		}

		result.statuses[i] = make([]workspace.WorktreeStatus, len(o.Worktrees))

		for j, wtName := range o.Worktrees {
			wg.Add(1)
			go func(ri, wi int, repoName, wtName string) {
				defer wg.Done()
				wtPath := filepath.Join(ws.RepoDir(repoName), wtName)
				st := workspace.QueryWorktreeStatus(wtPath)
				mu.Lock()
				result.statuses[ri][wi] = st
				mu.Unlock()
			}(i, j, o.Name, wtName)
		}

		wg.Add(1)
		go func(repoName string) {
			defer wg.Done()
			prs, err := gh.PRsForRepo(ws.Org, repoName)
			if err != nil {
				return
			}
			m := make(map[string]*github.PR, len(prs))
			for k := range prs {
				m[prs[k].HeadRefName] = &prs[k]
			}
			mu.Lock()
			result.prsByRepo[repoName] = m
			mu.Unlock()
		}(o.Name)
	}

	wg.Wait()
	return result
}
```

**Step 2: Rewrite `runStatusJSON` to use `collectStatusData`**

Replace the parallel collection section in `status_json.go` (lines 53-112) with:

```go
	data := collectStatusData(ws, gh, outlines)

	for i, o := range outlines {
		if o.Err != nil {
			continue
		}
		for j := range o.Worktrees {
			st := data.statuses[i][j]
			result.Repos[i].Worktrees[j] = worktreeJSON{
				Name:   st.Name,
				Branch: st.Branch,
				Dirty:  st.Dirty,
				Ahead:  st.Ahead,
				Behind: st.Behind,
			}
		}
	}

	// Attach PRs to worktrees.
	for i := range result.Repos {
		prs := data.prsByRepo[result.Repos[i].Name]
		if prs == nil {
			continue
		}
		for j := range result.Repos[i].Worktrees {
			wt := &result.Repos[i].Worktrees[j]
			if pr := lookupPR(prs, wt.Branch, wt.Name); pr != nil {
				wt.PR = &prJSON{
					Number:         pr.Number,
					Title:          pr.Title,
					State:          pr.State,
					URL:            pr.URL,
					ReviewDecision: pr.ReviewDecision,
					CheckStatus:    pr.StatusRollup,
				}
			}
		}
	}
```

Remove the now-unused imports: `"path/filepath"`, `"sync"`.

**Step 3: Rewrite `runStatusLLM` to use `collectStatusData`**

Replace the parallel collection section in `status_llm.go` (lines 28-98) with:

```go
	data := collectStatusData(ws, gh, outlines)

	type wtEntry struct {
		name   string
		status workspace.WorktreeStatus
		pr     *github.PR
	}
	type llmRepoEntry struct {
		outline   workspace.RepoOutline
		worktrees []wtEntry
	}

	repos := make([]llmRepoEntry, len(outlines))
	for i, o := range outlines {
		wts := make([]wtEntry, len(o.Worktrees))
		for j, wtName := range o.Worktrees {
			var st workspace.WorktreeStatus
			if data.statuses[i] != nil {
				st = data.statuses[i][j]
			}
			wts[j] = wtEntry{name: wtName, status: st}
		}

		// Attach PRs.
		prs := data.prsByRepo[o.Name]
		if prs != nil {
			for j := range wts {
				wts[j].pr = lookupPR(prs, wts[j].status.Branch, wts[j].name)
			}
		}

		repos[i] = llmRepoEntry{outline: o, worktrees: wts}
	}
```

Remove the now-unused imports: `"path/filepath"`, `"sync"`.

**Step 4: Run tests**

Run: `just test`
Expected: All tests pass (particularly `status_format_test.go` and any status-related tests).

**Step 5: Commit**

```bash
git add internal/cli/status_collect.go internal/cli/status_json.go internal/cli/status_llm.go
git commit -m "refactor: extract shared status data collection from JSON/LLM paths"
```

---

### Task 12: Split `renderDetail` into focused helpers

**Files:**
- Modify: `internal/cli/mc_view.go:361-496`

The `renderDetail` function is 135 lines with interleaved `if isGround` / `if row.kind == rowWorktree` / `if row.kind == rowGhostPR` branches. Extract the tier-1 header rendering into three focused helpers.

**Step 1: Extract `renderWorktreeDetail` and `renderGhostDetail` header helpers**

This is a view-only refactoring. The `renderDetail` function currently renders:
1. A header line (branch name + tags + repo label) — shared
2. PR line — for non-ground only
3. Git status — for worktree only
4. Ghost PR status — for ghost only
5. Detail tier 2 content — delegated to `renderDetailTier2`

Extract the content between the header line and the tier-2 call into `renderCapsuleHeader` (for worktree rows) and `renderGhostHeader` (for ghost PR rows). Keep the shared header line and the tier-2 delegation in `renderDetail`.

The exact refactoring: split the section from line 424 ("if !isGround {") through line 487 into helper methods. Since this is a view-layer refactoring with existing visual test coverage, the primary verification is that `just test` passes and the View output remains identical.

**Step 2: Run tests**

Run: `just test`
Expected: All tests pass (particularly `mc_view_test.go`).

**Step 3: Commit**

```bash
git add internal/cli/mc_view.go
git commit -m "refactor: split renderDetail into focused row-kind helpers"
```
