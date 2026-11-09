# `ws doctor --fix` Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `--fix` flag to `ws doctor` that auto-repairs safe issues (cloning missing repos) and prints hints for destructive ones (orphaned worktrees, missing tools).

**Architecture:** Extend `CheckResult` with optional `Fix func() error` and `FixHint string` fields. The check methods populate these. The CLI executes fixes / prints hints when `--fix` is passed.

**Tech Stack:** Go, cobra (CLI), existing workspace operations

---

### Task 1: Add Fix and FixHint fields to CheckResult

**Files:**
- Modify: `internal/workspace/doctor.go:19-23`

**Step 1: Write the failing test**

Create `internal/workspace/doctor_test.go`:

```go
package workspace

import "testing"

func TestCheckResult_HasFixFields(t *testing.T) {
	called := false
	r := CheckResult{
		Name:    "test",
		Status:  CheckFail,
		Detail:  "broken",
		Fix:     func() error { called = true; return nil },
		FixHint: "try this",
	}

	if r.Fix == nil {
		t.Fatal("Fix should not be nil")
	}
	if err := r.Fix(); err != nil {
		t.Fatalf("Fix returned error: %v", err)
	}
	if !called {
		t.Fatal("Fix was not called")
	}
	if r.FixHint != "try this" {
		t.Errorf("FixHint = %q, want %q", r.FixHint, "try this")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test`
Expected: FAIL — `CheckResult` doesn't have `Fix` or `FixHint` fields yet.

**Step 3: Add the fields to CheckResult**

In `internal/workspace/doctor.go`, change `CheckResult` (lines 19-23) to:

```go
type CheckResult struct {
	Name    string
	Status  CheckStatus
	Detail  string
	Fix     func() error // nil if not auto-fixable
	FixHint string       // shown when Fix is nil but manual action exists
}
```

**Step 4: Run test to verify it passes**

Run: `just test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/workspace/doctor.go internal/workspace/doctor_test.go
git commit -m "feat(doctor): add Fix and FixHint fields to CheckResult"
```

---

### Task 2: Attach fix closure to checkRepos for missing repos

**Files:**
- Modify: `internal/workspace/doctor.go:39-51`
- Test: `internal/workspace/doctor_test.go`

**Step 1: Write the failing test**

Append to `internal/workspace/doctor_test.go`:

```go
import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckRepos_MissingRepo_HasFix(t *testing.T) {
	root := t.TempDir()
	reposDir := filepath.Join(root, "repos")
	os.MkdirAll(filepath.Join(reposDir, "exists", ".bare"), 0755)
	os.MkdirAll(filepath.Join(reposDir, "missing"), 0755)

	w := &Workspace{
		Root:      root,
		RepoNames: []string{"exists", "missing"},
	}

	cat := w.checkRepos()

	for _, check := range cat.Checks {
		if check.Name == "exists" {
			if check.Status != CheckOK {
				t.Errorf("exists: status = %d, want CheckOK", check.Status)
			}
			if check.Fix != nil {
				t.Error("exists: Fix should be nil for healthy repo")
			}
		}
		if check.Name == "missing" {
			if check.Status != CheckFail {
				t.Errorf("missing: status = %d, want CheckFail", check.Status)
			}
			if check.Fix == nil {
				t.Error("missing: Fix should not be nil for missing repo")
			}
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test`
Expected: FAIL — `check.Fix` is nil for missing repo.

**Step 3: Add fix closure in checkRepos**

In `internal/workspace/doctor.go`, update `checkRepos()` (lines 39-51):

```go
func (w *Workspace) checkRepos() CheckCategory {
	var checks []CheckResult
	for _, name := range w.RepoNames {
		bareDir := w.BareDir(name)
		result := CheckResult{Name: name, Status: CheckOK}
		if _, err := os.Stat(bareDir); err != nil {
			result.Status = CheckFail
			result.Detail = fmt.Sprintf("not cloned (expected %s)", bareDir)
			repoName := name // capture for closure
			result.Fix = func() error {
				sr := w.SetupRepo(repoName)
				if sr.Err != nil {
					return sr.Err
				}
				return nil
			}
		}
		checks = append(checks, result)
	}
	return CheckCategory{Name: "Repos", Checks: checks}
}
```

**Step 4: Run test to verify it passes**

Run: `just test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/workspace/doctor.go internal/workspace/doctor_test.go
git commit -m "feat(doctor): attach fix closure for missing repos"
```

---

### Task 3: Add FixHint to orphaned worktrees and missing tools

**Files:**
- Modify: `internal/workspace/doctor.go:53-99`
- Test: `internal/workspace/doctor_test.go`

**Step 1: Write the failing test**

Append to `internal/workspace/doctor_test.go`:

```go
func TestCheckTools_MissingGh_HasHint(t *testing.T) {
	w := &Workspace{}

	// Temporarily break PATH to ensure gh isn't found
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", "")
	defer os.Setenv("PATH", origPath)

	cat := w.checkTools()

	if len(cat.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(cat.Checks))
	}
	check := cat.Checks[0]
	if check.Status != CheckFail {
		t.Errorf("status = %d, want CheckFail", check.Status)
	}
	if check.FixHint == "" {
		t.Error("expected FixHint for missing gh tool")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test`
Expected: FAIL — `FixHint` is empty.

**Step 3: Add hints**

In `internal/workspace/doctor.go`, update `checkOrphanedWorktrees()` — add `FixHint` to the orphaned worktree check (around line 74-78):

```go
			if !knownBranches[wt] {
				wtPath := filepath.Join(w.RepoDir(name), wt)
				checks = append(checks, CheckResult{
					Name:    fmt.Sprintf("%s/%s", name, wt),
					Status:  CheckWarn,
					Detail:  "directory exists but not registered as a git worktree",
					FixHint: fmt.Sprintf("rm -rf %s", wtPath),
				})
			}
```

Update `checkTools()` — add `FixHint` (around line 92-98):

```go
func (w *Workspace) checkTools() CheckCategory {
	result := CheckResult{Name: "gh", Status: CheckOK}
	if _, err := exec.LookPath("gh"); err != nil {
		result.Status = CheckFail
		result.Detail = "gh CLI not found on PATH"
		result.FixHint = "install with: brew install gh"
	}
	return CheckCategory{Name: "Tools", Checks: []CheckResult{result}}
}
```

**Step 4: Run test to verify it passes**

Run: `just test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/workspace/doctor.go internal/workspace/doctor_test.go
git commit -m "feat(doctor): add fix hints for orphaned worktrees and missing tools"
```

---

### Task 4: Add --fix flag to the doctor CLI command

**Files:**
- Modify: `internal/cli/doctor.go:12-32`
- Modify: `internal/cli/doctor.go:34-68`

**Step 1: Write the failing test**

Append to `internal/cli/integration_test.go`:

```go
func TestDoctor_FixFlag_HealthyWorkspace(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}},
	})

	result := testutil.RunCommand(t, w.Root, nil, "doctor", "--fix")
	if result.Err != nil {
		t.Fatalf("doctor --fix failed on healthy workspace: %v\nstderr: %s", result.Err, result.Stderr)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test`
Expected: FAIL — unknown flag `--fix`.

**Step 3: Add --fix flag and fix execution logic**

Replace `internal/cli/doctor.go` entirely:

```go
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
```

**Step 4: Run test to verify it passes**

Run: `just test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/doctor.go internal/cli/integration_test.go
git commit -m "feat(doctor): add --fix flag to auto-repair safe issues"
```

---

### Task 5: Integration test for hint output

**Files:**
- Test: `internal/cli/integration_test.go`

**Step 1: Write the test**

Append to `internal/cli/integration_test.go`:

```go
func TestDoctor_FixFlag_MissingRepo_ShowsFixAttempt(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}, {Name: "repo-b"}},
	})

	// Remove repo-b to simulate missing
	os.RemoveAll(filepath.Join(w.Root, "repos", "repo-b", ".bare"))
	os.RemoveAll(filepath.Join(w.Root, "repos", "repo-b", ".ground"))

	result := testutil.RunCommand(t, w.Root, nil, "doctor", "--fix")

	// Fix will fail because test-org/repo-b isn't a real GitHub repo,
	// but verify the fix was attempted (error message changes from
	// "not cloned" to "fix failed").
	if result.Err == nil {
		t.Fatal("expected doctor to still report failure when fix fails")
	}
	if !strings.Contains(result.Stderr, "fix failed") {
		t.Errorf("expected 'fix failed' in stderr, got:\n%s", result.Stderr)
	}
}

func TestDoctor_WithoutFix_NoHints(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}, {Name: "repo-b"}},
	})

	// Remove repo-b to simulate missing
	os.RemoveAll(filepath.Join(w.Root, "repos", "repo-b", ".bare"))

	result := testutil.RunCommand(t, w.Root, nil, "doctor")

	// Without --fix, should not show "fix failed" or hints
	if strings.Contains(result.Stderr, "fix failed") {
		t.Error("should not attempt fix without --fix flag")
	}
	if strings.Contains(result.Stderr, "hint:") {
		t.Error("should not show hints without --fix flag")
	}
}
```

**Step 2: Run test to verify it passes**

Run: `just test`
Expected: PASS (these tests exercise the new plumbing).

**Step 3: Commit**

```bash
git add internal/cli/integration_test.go
git commit -m "test(doctor): add integration tests for --fix flag behavior"
```

---

### Task 6: Manual verification and final cleanup

**Step 1: Build**

Run: `just build`

**Step 2: Verify doctor still works without --fix**

Run: `bin/workspace doctor`
Expected: Same output as before — no behavioral change.

**Step 3: Verify --fix flag is accepted**

Run: `bin/workspace doctor --fix`
Expected: Attempts to fix any issues, prints hints for non-fixable ones.

**Step 4: Verify help text**

Run: `bin/workspace doctor --help`
Expected: Shows `--fix` flag with description.

**Step 5: Run full test suite**

Run: `just test`
Expected: All tests pass.

**Step 6: Final commit if any cleanup needed**

Only if adjustments were needed during verification.
