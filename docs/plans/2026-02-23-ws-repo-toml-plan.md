# ws.repo.toml Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add per-repo `ws.repo.toml` config supporting `copy_from_ground` and `on_create` for capsule creation.

**Architecture:** New `ParseRepoConfig()` in `config/` for TOML parsing. New `CopyFromGround()` in `workspace/` for file copying. CLI commands (`lift`, `dock`) load repo config on demand and wire in the new steps.

**Tech Stack:** Go, `github.com/BurntSushi/toml`

---

### Task 1: ParseRepoConfig — Tests

**Files:**
- Modify: `internal/config/config_test.go`

**Step 1: Write failing tests for ParseRepoConfig**

Add these tests at the end of `internal/config/config_test.go`:

```go
func TestParseRepoConfig_Valid(t *testing.T) {
	content := `[capsule]
copy_from_ground = [".env", "config/local.yaml"]
on_create = "npm install"
`
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ws.repo.toml")
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := ParseRepoConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Capsule.CopyFromGround) != 2 {
		t.Fatalf("copy_from_ground count = %d, want 2", len(cfg.Capsule.CopyFromGround))
	}
	if cfg.Capsule.CopyFromGround[0] != ".env" {
		t.Errorf("copy_from_ground[0] = %q, want %q", cfg.Capsule.CopyFromGround[0], ".env")
	}
	if cfg.Capsule.CopyFromGround[1] != "config/local.yaml" {
		t.Errorf("copy_from_ground[1] = %q, want %q", cfg.Capsule.CopyFromGround[1], "config/local.yaml")
	}
	if cfg.Capsule.OnCreate != "npm install" {
		t.Errorf("on_create = %q, want %q", cfg.Capsule.OnCreate, "npm install")
	}
}

func TestParseRepoConfig_MissingFile(t *testing.T) {
	cfg, err := ParseRepoConfig(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Error("expected nil config for missing file")
	}
}

func TestParseRepoConfig_Malformed(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ws.repo.toml")
	os.WriteFile(path, []byte("this is not valid toml [[["), 0644)

	_, err := ParseRepoConfig(path)
	if err == nil {
		t.Error("expected error for malformed TOML")
	}
}

func TestParseRepoConfig_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ws.repo.toml")
	os.WriteFile(path, []byte(""), 0644)

	cfg, err := ParseRepoConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config for empty file")
	}
	if len(cfg.Capsule.CopyFromGround) != 0 {
		t.Errorf("copy_from_ground = %v, want empty", cfg.Capsule.CopyFromGround)
	}
	if cfg.Capsule.OnCreate != "" {
		t.Errorf("on_create = %q, want empty", cfg.Capsule.OnCreate)
	}
}

func TestParseRepoConfig_OnlyOnCreate(t *testing.T) {
	content := `[capsule]
on_create = "make setup"
`
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ws.repo.toml")
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := ParseRepoConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Capsule.OnCreate != "make setup" {
		t.Errorf("on_create = %q, want %q", cfg.Capsule.OnCreate, "make setup")
	}
	if len(cfg.Capsule.CopyFromGround) != 0 {
		t.Errorf("copy_from_ground = %v, want empty", cfg.Capsule.CopyFromGround)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `just test`
Expected: FAIL — `ParseRepoConfig` undefined

**Step 3: Commit**

```bash
git add internal/config/config_test.go
git commit -m "test: add ParseRepoConfig tests"
```

---

### Task 2: ParseRepoConfig — Implementation

**Files:**
- Modify: `internal/config/config.go`

**Step 1: Add structs and ParseRepoConfig function**

Add after the `LocalConfig` struct (around line 26) in `internal/config/config.go`:

```go
const RepoFileName = "ws.repo.toml"

type RepoFileConfig struct {
	Capsule CapsuleConfig `toml:"capsule"`
}

type CapsuleConfig struct {
	CopyFromGround []string `toml:"copy_from_ground"`
	OnCreate       string   `toml:"on_create"`
}

// ParseRepoConfig parses a ws.repo.toml file at the given path.
// Returns nil, nil if the file does not exist.
func ParseRepoConfig(path string) (*RepoFileConfig, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	var cfg RepoFileConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cfg, nil
}
```

**Step 2: Run tests to verify they pass**

Run: `just test`
Expected: All `TestParseRepoConfig_*` tests PASS

**Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat: add ParseRepoConfig for ws.repo.toml parsing"
```

---

### Task 3: CopyFromGround — Tests

**Files:**
- Create: `internal/workspace/operations_test.go`

**Step 1: Write failing tests for CopyFromGround**

Create `internal/workspace/operations_test.go`:

```go
package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFromGround_AllFilesExist(t *testing.T) {
	tmp := t.TempDir()
	groundDir := filepath.Join(tmp, ".ground")
	capsuleDir := filepath.Join(tmp, "my-feature")
	os.MkdirAll(groundDir, 0755)
	os.MkdirAll(capsuleDir, 0755)

	os.WriteFile(filepath.Join(groundDir, ".env"), []byte("SECRET=abc"), 0644)

	skipped, err := CopyFromGround(groundDir, capsuleDir, []string{".env"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want empty", skipped)
	}

	got, err := os.ReadFile(filepath.Join(capsuleDir, ".env"))
	if err != nil {
		t.Fatalf("reading copied file: %v", err)
	}
	if string(got) != "SECRET=abc" {
		t.Errorf("content = %q, want %q", string(got), "SECRET=abc")
	}
}

func TestCopyFromGround_NestedDirs(t *testing.T) {
	tmp := t.TempDir()
	groundDir := filepath.Join(tmp, ".ground")
	capsuleDir := filepath.Join(tmp, "my-feature")
	os.MkdirAll(filepath.Join(groundDir, "config"), 0755)
	os.MkdirAll(capsuleDir, 0755)

	os.WriteFile(filepath.Join(groundDir, "config", "local.yaml"), []byte("key: val"), 0644)

	skipped, err := CopyFromGround(groundDir, capsuleDir, []string{"config/local.yaml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want empty", skipped)
	}

	got, err := os.ReadFile(filepath.Join(capsuleDir, "config", "local.yaml"))
	if err != nil {
		t.Fatalf("reading copied file: %v", err)
	}
	if string(got) != "key: val" {
		t.Errorf("content = %q, want %q", string(got), "key: val")
	}
}

func TestCopyFromGround_SomeMissing(t *testing.T) {
	tmp := t.TempDir()
	groundDir := filepath.Join(tmp, ".ground")
	capsuleDir := filepath.Join(tmp, "my-feature")
	os.MkdirAll(groundDir, 0755)
	os.MkdirAll(capsuleDir, 0755)

	os.WriteFile(filepath.Join(groundDir, ".env"), []byte("SECRET=abc"), 0644)

	skipped, err := CopyFromGround(groundDir, capsuleDir, []string{".env", "missing.txt", "also-missing.conf"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped count = %d, want 2", len(skipped))
	}
	if skipped[0] != "missing.txt" || skipped[1] != "also-missing.conf" {
		t.Errorf("skipped = %v, want [missing.txt also-missing.conf]", skipped)
	}

	// .env should still have been copied
	if _, err := os.Stat(filepath.Join(capsuleDir, ".env")); err != nil {
		t.Error("expected .env to be copied despite other missing files")
	}
}

func TestCopyFromGround_EmptyList(t *testing.T) {
	tmp := t.TempDir()
	groundDir := filepath.Join(tmp, ".ground")
	capsuleDir := filepath.Join(tmp, "my-feature")
	os.MkdirAll(groundDir, 0755)
	os.MkdirAll(capsuleDir, 0755)

	skipped, err := CopyFromGround(groundDir, capsuleDir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want empty", skipped)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `just test`
Expected: FAIL — `CopyFromGround` undefined

**Step 3: Commit**

```bash
git add internal/workspace/operations_test.go
git commit -m "test: add CopyFromGround tests"
```

---

### Task 4: CopyFromGround — Implementation

**Files:**
- Modify: `internal/workspace/operations.go`

**Step 1: Add CopyFromGround function**

Add at the end of `internal/workspace/operations.go`:

```go
// CopyFromGround copies files from groundDir to capsuleDir.
// Missing source files are skipped and returned in the skipped slice.
// Returns a hard error for permission/IO failures on files that do exist.
func CopyFromGround(groundDir, capsuleDir string, paths []string) (skipped []string, err error) {
	for _, p := range paths {
		src := filepath.Join(groundDir, p)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			skipped = append(skipped, p)
			continue
		}

		dst := filepath.Join(capsuleDir, p)
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return skipped, fmt.Errorf("creating directory for %s: %w", p, err)
		}

		data, err := os.ReadFile(src)
		if err != nil {
			return skipped, fmt.Errorf("reading %s: %w", p, err)
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return skipped, fmt.Errorf("writing %s: %w", p, err)
		}
	}
	return skipped, nil
}
```

**Step 2: Run tests to verify they pass**

Run: `just test`
Expected: All `TestCopyFromGround_*` tests PASS

**Step 3: Commit**

```bash
git add internal/workspace/operations.go
git commit -m "feat: add CopyFromGround for copying files into new capsules"
```

---

### Task 5: Wire into lift command

**Files:**
- Modify: `internal/cli/lift.go`

**Step 1: Update lift to load repo config and run copy + fallback hook**

The current lift command (lines 56–82 of `internal/cli/lift.go`) checks `ctx.WS.PostCreateHooks[repo]` to decide if it should add a "Running hooks" step. We need to also load `ws.repo.toml` to determine if there are copy or hook steps.

Replace the block from `bareDir := ctx.WS.BareDir(repo)` (line 55) through the end of the `op` function (line 82) with:

```go
			bareDir := ctx.WS.BareDir(repo)
			hook, hasHook := ctx.WS.PostCreateHooks[repo]

			// Load per-repo config from .ground/ws.repo.toml
			repoConfigPath := filepath.Join(ctx.WS.MainWorktree(repo), config.RepoFileName)
			repoCfg, err := config.ParseRepoConfig(repoConfigPath)
			if err != nil {
				return err
			}

			hasCopy := repoCfg != nil && len(repoCfg.Capsule.CopyFromGround) > 0

			// Determine effective hook: post_create wins, on_create is fallback
			if !hasHook && repoCfg != nil && repoCfg.Capsule.OnCreate != "" {
				hook = repoCfg.Capsule.OnCreate
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
					c, err := ctx.WS.CreateLiftWorktree(repo, branch, base)
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
```

Then after the existing `hookErr` warning (around line 102), add the copy warning:

```go
			if len(copySkipped) > 0 {
				fmt.Fprintf(os.Stderr, "  %s copy_from_ground: skipped missing files: %s\n",
					ui.Orange.Render("⚠"), strings.Join(copySkipped, ", "))
			}
```

Add `"strings"` to the imports.

**Step 2: Run tests to verify they pass**

Run: `just test`
Expected: PASS (existing tests should still work; lift tests don't depend on ws.repo.toml existing)

**Step 3: Commit**

```bash
git add internal/cli/lift.go
git commit -m "feat: lift reads ws.repo.toml for copy_from_ground and on_create"
```

---

### Task 6: Wire into dock command

**Files:**
- Modify: `internal/cli/dock.go`

**Step 1: Update dock with the same repo config logic**

Apply the same pattern as Task 5 to `internal/cli/dock.go`. Replace the block from `bareDir := ctx.WS.BareDir(repo)` (line 118) through the end of the `op` function (line 145):

```go
			bareDir := ctx.WS.BareDir(repo)
			hook, hasHook := ctx.WS.PostCreateHooks[repo]

			// Load per-repo config from .ground/ws.repo.toml
			repoConfigPath := filepath.Join(ctx.WS.MainWorktree(repo), config.RepoFileName)
			repoCfg, err := config.ParseRepoConfig(repoConfigPath)
			if err != nil {
				return err
			}

			hasCopy := repoCfg != nil && len(repoCfg.Capsule.CopyFromGround) > 0

			// Determine effective hook: post_create wins, on_create is fallback
			if !hasHook && repoCfg != nil && repoCfg.Capsule.OnCreate != "" {
				hook = repoCfg.Capsule.OnCreate
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
					c, err := ctx.WS.CreateDockWorktree(repo, branch)
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
```

Then after the existing `hookErr` warning (around line 165), add:

```go
			if len(copySkipped) > 0 {
				fmt.Fprintf(os.Stderr, "  %s copy_from_ground: skipped missing files: %s\n",
					ui.Orange.Render("⚠"), strings.Join(copySkipped, ", "))
			}
```

Add `"strings"` to the imports.

**Step 2: Run tests to verify they pass**

Run: `just test`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/cli/dock.go
git commit -m "feat: dock reads ws.repo.toml for copy_from_ground and on_create"
```

---

### Task 7: Build and verify

**Step 1: Build**

Run: `just build`
Expected: Clean build, binary at `bin/workspace`

**Step 2: Run full test suite**

Run: `just test`
Expected: All tests PASS

**Step 3: Commit (if any fixups needed)**

Only if previous steps required changes.
