# Tmux Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Mission Control tmux-aware — named windows, dedup on go, live tracking, and lifecycle cleanup — all implicit behind `$TMUX`.

**Architecture:** New `internal/tmux` package wraps tmux CLI commands. MC's existing actions (`doGo`, `doDelete`, `doRefresh`) call into it. The `workspace/` package is untouched. All tmux behavior is gated on `$TMUX` being set.

**Tech Stack:** Go, `os/exec` for tmux CLI, bubbletea for MC integration.

---

### Task 1: Create `internal/tmux` package with `InTmux` and `ListWindows`

**Files:**
- Create: `internal/tmux/tmux.go`
- Create: `internal/tmux/tmux_test.go`

**Step 1: Write the failing tests**

```go
// internal/tmux/tmux_test.go
package tmux

import "testing"

func TestInTmux_ReturnsTrue_WhenEnvSet(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")
	if !InTmux() {
		t.Error("InTmux() = false, want true")
	}
}

func TestInTmux_ReturnsFalse_WhenEnvEmpty(t *testing.T) {
	t.Setenv("TMUX", "")
	if InTmux() {
		t.Error("InTmux() = true, want false")
	}
}

func TestParseListWindows(t *testing.T) {
	output := "Frontend:auth-flow @1\nAPI:fix-cache @2\nmc @3\n"
	got := parseListWindows(output)

	if got["Frontend:auth-flow"] != "@1" {
		t.Errorf("Frontend:auth-flow = %q, want @1", got["Frontend:auth-flow"])
	}
	if got["API:fix-cache"] != "@2" {
		t.Errorf("API:fix-cache = %q, want @2", got["API:fix-cache"])
	}
	if got["mc"] != "@3" {
		t.Errorf("mc = %q, want @3", got["mc"])
	}
}

func TestParseListWindows_EmptyOutput(t *testing.T) {
	got := parseListWindows("")
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestWindowName(t *testing.T) {
	tests := []struct {
		display, capsule, want string
	}{
		{"Frontend", "auth-flow", "Frontend:auth-flow"},
		{"repo1", ".ground", "repo1:.ground"},
		{"API Server", "fix", "API Server:fix"},
	}
	for _, tt := range tests {
		got := WindowName(tt.display, tt.capsule)
		if got != tt.want {
			t.Errorf("WindowName(%q, %q) = %q, want %q", tt.display, tt.capsule, got, tt.want)
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `just test`
Expected: FAIL — `InTmux`, `parseListWindows`, `WindowName` not defined.

**Step 3: Write the implementation**

```go
// internal/tmux/tmux.go
package tmux

import (
	"os"
	"os/exec"
	"strings"
)

// InTmux returns true when the current process is running inside a tmux session.
func InTmux() bool {
	return os.Getenv("TMUX") != ""
}

// WindowName builds the canonical tmux window name for a capsule.
func WindowName(displayName, capsule string) string {
	return displayName + ":" + capsule
}

// ListWindows returns a map of windowName → windowID for the current tmux session.
// Returns an empty map if not in tmux or if the query fails.
func ListWindows() map[string]string {
	out, err := exec.Command("tmux", "list-windows", "-F", "#{window_name} #{window_id}").CombinedOutput()
	if err != nil {
		return nil
	}
	return parseListWindows(string(out))
}

func parseListWindows(output string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		// Format: "window_name @id" — split on last space since names can contain spaces
		idx := strings.LastIndex(line, " ")
		if idx < 0 {
			continue
		}
		name := line[:idx]
		id := line[idx+1:]
		result[name] = id
	}
	return result
}

// SelectWindow switches to an existing tmux window by ID.
func SelectWindow(id string) error {
	return exec.Command("tmux", "select-window", "-t", id).Run()
}

// NewWindow creates a new tmux window with the given name, starting in path.
func NewWindow(name, path string) error {
	return exec.Command("tmux", "new-window", "-n", name, "-c", path).Start()
}

// KillWindow closes a tmux window by ID.
func KillWindow(id string) error {
	return exec.Command("tmux", "kill-window", "-t", id).Run()
}
```

**Step 4: Run tests to verify they pass**

Run: `just test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tmux/tmux.go internal/tmux/tmux_test.go
git commit -m "feat: add internal/tmux package for tmux window management"
```

---

### Task 2: Smart Go — named windows + dedup in `doGo()`

**Files:**
- Modify: `internal/cli/mc_keys.go:168-179` (`doGo` method)
- Modify: `internal/cli/mc_keys_test.go`

**Step 1: Write the failing tests**

Add to `internal/cli/mc_keys_test.go`:

```go
func TestDoGo_NonTmux_SetsJumpPath(t *testing.T) {
	m := keysMCModel()
	m.ws.Root = t.TempDir()
	os.MkdirAll(filepath.Join(m.ws.Root, "repos", "repo1", "feat"), 0755)
	m.cursor = 2 // "feat" worktree

	t.Setenv("TMUX", "")

	m, cmd := m.doGo()

	if m.jumpPath == "" {
		t.Error("jumpPath should be set when not in tmux")
	}
	if !isQuitCmd(cmd) {
		t.Error("should return tea.Quit when not in tmux")
	}
}
```

**Step 2: Run tests to verify they pass** (this one should already pass since it tests existing behavior)

Run: `just test`
Expected: PASS — this validates our test setup works.

**Step 3: Update `doGo()` to use tmux package**

Replace the `doGo` method in `internal/cli/mc_keys.go`:

```go
func (m mcModel) doGo() (mcModel, tea.Cmd) {
	path := m.selectedWorktreePath()
	if path == "" {
		return m, nil
	}
	if tmuxpkg.InTmux() {
		row := m.rows[m.cursor]
		name := tmuxpkg.WindowName(m.ws.DisplayNameFor(row.repo), row.wt)
		windows := tmuxpkg.ListWindows()
		if id, ok := windows[name]; ok {
			_ = tmuxpkg.SelectWindow(id)
		} else {
			_ = tmuxpkg.NewWindow(name, path)
		}
	} else {
		m.jumpPath = path
		return m, tea.Quit
	}
	return m, nil
}
```

Update the import block in `mc_keys.go` to add:

```go
tmuxpkg "github.com/brudil/workspace/internal/tmux"
```

And remove the now-unused `"os"` and `"os/exec"` imports (if they're no longer needed — `os` is still used for `os.Getenv` in `doOpen`, and `exec` is still used for `doOpen`, `doOpenPR`. Check: `os` is used by `doOpen` for `os.Getenv("EDITOR")`. `exec` is used by `doOpen` and `doOpenPR`. So only remove `"os"` if it's truly unused — it won't be, `doOpen` still uses it. Actually, looking at the imports: `os` is used by `doOpen` for `EDITOR` env, and `exec` is used by `doOpen` and `doOpenPR`. So we keep both, just add `tmuxpkg`.)

**Step 4: Run tests to verify they pass**

Run: `just test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/mc_keys.go internal/cli/mc_keys_test.go
git commit -m "feat: smart go — named tmux windows with dedup"
```

---

### Task 3: Add `live` field to `mcRow` and tmux window query to MC init

**Files:**
- Modify: `internal/cli/mc_model.go` (add `live` field to `mcRow`, add message type)
- Modify: `internal/cli/mc_update.go` (add tmux query to Init, handle message)
- Modify: `internal/cli/mc_update_test.go`

**Step 1: Write the failing test**

Add to `internal/cli/mc_update_test.go`:

```go
func TestMCUpdate_TmuxWindowsMsg(t *testing.T) {
	m := baseMCModel()
	m.ws.DisplayNames = map[string]string{"repo1": "Repo One"}
	m.rows[2].wt = "feat"

	msg := mcTmuxWindowsMsg{
		windows: map[string]string{
			"Repo One:feat": "@1",
		},
	}

	result, _ := m.Update(msg)
	rm := result.(mcModel)

	if !rm.rows[2].live {
		t.Error("feat row should be live")
	}
	if rm.rows[1].live {
		t.Error("main row should not be live")
	}
}

func TestMCUpdate_TmuxWindowsMsg_NoDisplayName(t *testing.T) {
	m := baseMCModel()
	// No display name set, falls back to canonical name "repo1"
	m.rows[2].wt = "feat"

	msg := mcTmuxWindowsMsg{
		windows: map[string]string{
			"repo1:feat": "@1",
		},
	}

	result, _ := m.Update(msg)
	rm := result.(mcModel)

	if !rm.rows[2].live {
		t.Error("feat row should be live")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `just test`
Expected: FAIL — `mcTmuxWindowsMsg` not defined, `live` field not on `mcRow`.

**Step 3: Add `live` field and message type**

In `internal/cli/mc_model.go`, add `live` to `mcRow`:

```go
type mcRow struct {
	kind      rowKind
	repo      string
	wt        string
	branch    string
	dirty     bool
	ahead     int
	behind    int
	loaded    bool
	pr        *github.PR
	isBoarded bool
	merged    bool
	live      bool
}
```

Add the message type after `mcGhUserMsg`:

```go
type mcTmuxWindowsMsg struct {
	windows map[string]string // windowName → windowID
}
```

**Step 4: Handle the message in `mc_update.go`**

Add to the `handleMsg` switch in `mc_update.go`, after the `mcGhUserMsg` case:

```go
	case mcTmuxWindowsMsg:
		for i := range m.rows {
			if m.rows[i].kind == rowRepoHeader {
				continue
			}
			name := tmuxpkg.WindowName(m.ws.DisplayNameFor(m.rows[i].repo), m.rows[i].wt)
			m.rows[i].live = msg.windows[name] != ""
		}
		return m, nil
```

Add the tmux query to `Init()` — append to the cmds slice, right before the return:

```go
	if tmuxpkg.InTmux() {
		cmds = append(cmds, queryTmuxWindows())
	}
```

Add the query function:

```go
func queryTmuxWindows() tea.Cmd {
	return func() tea.Msg {
		return mcTmuxWindowsMsg{windows: tmuxpkg.ListWindows()}
	}
}
```

Add the import for the tmux package to `mc_update.go`:

```go
tmuxpkg "github.com/brudil/workspace/internal/tmux"
```

**Step 5: Run tests to verify they pass**

Run: `just test`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/cli/mc_model.go internal/cli/mc_update.go internal/cli/mc_update_test.go
git commit -m "feat: query tmux windows at init and set live flag on rows"
```

---

### Task 4: Render `live` indicator in list and detail views

**Files:**
- Modify: `internal/cli/mc_view.go` (`renderWorktreeRow` and `renderDetail`)
- Modify: `internal/cli/mc_view_test.go`

**Step 1: Read the existing view test to understand patterns**

Read: `internal/cli/mc_view_test.go`

**Step 2: Write the failing tests**

Add to `internal/cli/mc_view_test.go` (adapt to existing test patterns found in step 1):

Test that `renderWorktreeRow` includes the live indicator when `row.live` is true, and the detail panel includes a `live` tag.

The exact test will depend on the patterns in `mc_view_test.go` — likely checking that the rendered string contains "●" or "live".

**Step 3: Add live indicator to `renderWorktreeRow`**

In `mc_view.go`, in `renderWorktreeRow`, add after the dirty indicator block (after the `if row.dirty` block around line 278-280):

```go
	if row.live {
		buf.WriteString(ui.Green.Render("●") + " ")
	}
```

Wait — dirty already uses `ui.Orange.Render("●")`. We need a different indicator or color. Use `ui.Green.Render("●")` for live. But if the row is both dirty and live, we'd get two dots. Better approach: make the existing dot reflect both states. Actually, keeping them separate is clearer:

- `●` orange = dirty (uncommitted changes)
- `●` green = live (tmux window open)

They convey different things and can coexist. But two dots could be confusing. Let's use a different symbol for live. Use `◆` for live:

```go
	if row.live {
		buf.WriteString(ui.Green.Render("◆") + " ")
	}
```

Actually, the simplest approach: add the live indicator *before* the name (in the left margin area), similar to how `isBoarded` uses the `›` marker. Let's place it next to the boarded marker area. Currently boarded uses the `›` character in the first 2 columns. For non-boarded rows it's 2 spaces. We can add a live dot in a different position.

Simpler: just add a `live` tag in the detail panel header (alongside `docked`, `boarded`, etc.) and a subtle list indicator. The detail tag is the main signal; the list indicator is secondary.

In `renderDetail`, add the `live` tag to the tags list (around line 356-373):

```go
		if row.live {
			tags = append(tags, ui.TagGreen.Render("live"))
		}
```

Place it after `docked`/`remote` and before `boarded`, so the order reads: `docked live boarded clean`.

In `renderWorktreeRow`, add a small indicator. Place it right after the boarded marker area but before the name — use the existing 2-char prefix. When live, use a green `›` instead of the dim `›` or blank:

Actually, simplest: add `live` as a small trailing indicator just like dirty/merged. After the dirty dot:

```go
	if row.live {
		buf.WriteString(ui.Green.Render("⟡") + " ")
	}
```

Hmm, let's keep it minimal. A `●` green dot is fine alongside the orange dirty dot — they communicate different things. If both are present, you see `● ●` (orange then green). That's clear enough.

Actually, re-reading the design: "Small `●` indicator in the list view next to live capsules." So use `●` green.

```go
	if row.live {
		buf.WriteString(ui.Green.Render("●") + " ")
	}
```

Put it right after the dirty indicator. If both dirty and live, you'll see orange `●` then green `●`. Distinguishable by color.

**Step 4: Run tests to verify they pass**

Run: `just test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/mc_view.go internal/cli/mc_view_test.go
git commit -m "feat: render live indicator in MC list and detail views"
```

---

### Task 5: Re-query tmux windows on refresh

**Files:**
- Modify: `internal/cli/mc_keys.go` (`doRefresh` and `rebuildModel`)
- Modify: `internal/cli/mc_update.go` (ensure Init re-queries)

**Step 1: Verify refresh re-queries tmux**

`doRefresh()` calls `rebuildModel()` which calls `newMCModel()` then `.Init()`. Since Task 3 added the tmux query to `Init()`, refresh already re-queries tmux windows. Verify this by reading the code path.

If `Init()` already includes the tmux query, no code changes needed for refresh — it's automatic.

**Step 2: Also re-query after doGo creates/switches a window**

After `doGo()` creates or switches to a tmux window, the `live` state of rows has changed. We should re-query tmux windows. Add a `tea.Cmd` that re-queries:

In `doGo()`, after the tmux operations, return a command to re-query:

```go
func (m mcModel) doGo() (mcModel, tea.Cmd) {
	path := m.selectedWorktreePath()
	if path == "" {
		return m, nil
	}
	if tmuxpkg.InTmux() {
		row := m.rows[m.cursor]
		name := tmuxpkg.WindowName(m.ws.DisplayNameFor(row.repo), row.wt)
		windows := tmuxpkg.ListWindows()
		if id, ok := windows[name]; ok {
			_ = tmuxpkg.SelectWindow(id)
		} else {
			_ = tmuxpkg.NewWindow(name, path)
		}
		return m, queryTmuxWindows()
	}
	m.jumpPath = path
	return m, tea.Quit
}
```

**Step 3: Run tests**

Run: `just test`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/cli/mc_keys.go
git commit -m "feat: re-query tmux windows after go to update live state"
```

---

### Task 6: Lifecycle sync — kill tmux window on capsule delete

**Files:**
- Modify: `internal/cli/mc_keys.go` (delete confirmation handler)
- Modify: `internal/cli/mc_update.go` (`mcWorktreeDeletedMsg` handler)
- Modify: `internal/cli/mc_keys_test.go`

**Step 1: Write the failing test**

Add to `internal/cli/mc_keys_test.go`:

```go
func TestDoDelete_ConfirmY_IncludesWindowName(t *testing.T) {
	m := keysMCModel()
	m.ws.DisplayNames = map[string]string{"repo1": "Repo One"}
	m.confirmIdx = 2 // confirming delete of "feat"

	m, cmd := m.handleKey(keyMsg("y"))

	if m.confirmIdx != -1 {
		t.Errorf("confirmIdx = %d, want -1", m.confirmIdx)
	}
	// The cmd should be a function (async delete). We can't easily test
	// the tmux kill inside it without mocking, but we verify the flow works.
	if cmd == nil {
		t.Error("expected a cmd for delete action")
	}
}
```

**Step 2: Modify the delete flow to kill tmux window**

The delete confirmation handler (in `handleKey`, the `case "y":` block) fires an async command that calls `ws.RemoveWorktree`. We need to kill the tmux window *before* removing the worktree.

Modify the `case "y":` block in `handleKey`:

```go
		case "y":
			row := m.rows[m.confirmIdx]
			idx := m.confirmIdx
			m.confirmIdx = -1
			m.actionSpinner = idx
			repo := row.repo
			branch := row.wt
			ws := m.ws
			windowName := tmuxpkg.WindowName(m.ws.DisplayNameFor(repo), branch)
			return m, func() tea.Msg {
				// Kill tmux window before removing worktree
				if tmuxpkg.InTmux() {
					windows := tmuxpkg.ListWindows()
					if id, ok := windows[windowName]; ok {
						_ = tmuxpkg.KillWindow(id)
					}
				}
				err := ws.RemoveWorktree(repo, branch)
				return mcWorktreeDeletedMsg{rowIdx: idx, repo: repo, branch: branch, err: err}
			}
```

**Step 3: Also kill windows during debrief (doRefresh)**

In `doRefresh()`, before calling `ws.RemoveWorktree`, kill the window. Modify the loop in `doRefresh`:

```go
func (m mcModel) doRefresh() (mcModel, tea.Cmd) {
	capsules := m.ws.FindAllCapsules(14, "")

	var windowsToKill []string
	if tmuxpkg.InTmux() {
		windows := tmuxpkg.ListWindows()
		for _, c := range capsules {
			if (c.Merged || c.Inactive) && !c.Dirty {
				name := tmuxpkg.WindowName(m.ws.DisplayNameFor(c.Repo), c.Name)
				if id, ok := windows[name]; ok {
					windowsToKill = append(windowsToKill, id)
				}
			}
		}
	}

	for _, id := range windowsToKill {
		_ = tmuxpkg.KillWindow(id)
	}

	boardChanged := false
	for _, c := range capsules {
		if (c.Merged || c.Inactive) && !c.Dirty {
			if m.ws.IsBoarded(c.Repo, c.Name) {
				_ = m.ws.Unboard(c.Repo, c.Name)
				boardChanged = true
			}
			_ = m.ws.RemoveWorktree(c.Repo, c.Name)
		}
	}
	if boardChanged {
		config.SaveBoarded(m.ws.Root, m.ws.Boarded)
		ide.Regenerate(m.ws.Root, m.ws.Boarded, m.ws.DisplayNames, m.ws.Org)
	}
	return m.rebuildModel()
}
```

**Step 4: Run tests**

Run: `just test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/mc_keys.go internal/cli/mc_update.go internal/cli/mc_keys_test.go
git commit -m "feat: kill tmux window when capsule is deleted or debriefed"
```

---

### Task 7: Build and manual smoke test

**Step 1: Build**

Run: `just build`
Expected: Clean build, binary at `bin/workspace`.

**Step 2: Run full test suite**

Run: `just test`
Expected: All tests pass.

**Step 3: Commit any final adjustments**

If any fixups are needed from the build or test run, fix and commit.
