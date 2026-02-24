# Command Palette Overhaul Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the Mission Control command palette feel polished — human-readable labels, descriptions, keybinding hints, scope tags, smart sorting, fuzzy match highlighting, and scroll tracking.

**Architecture:** Enrich the existing `paletteCommand` struct with `label`, `desc`, and `key` fields. Add `FuzzyMatchPositions()` to return match indices for highlight rendering. Add `paletteOffset` for scroll window tracking. All changes stay within the existing bottom-anchored palette pattern.

**Tech Stack:** Go, Bubbletea, Lipgloss

---

### Task 1: Add FuzzyMatchPositions helper

**Files:**
- Modify: `internal/workspace/workspace.go:58-77`
- Test: `internal/workspace/workspace_test.go`

**Step 1: Write the failing test**

Add to `internal/workspace/workspace_test.go`:

```go
func TestFuzzyMatchPositions(t *testing.T) {
	tests := []struct {
		pattern, target string
		want            []int
	}{
		{"fnt", "frontend", []int{0, 3, 7}},
		{"fe", "frontend", []int{0, 5}},
		{"FE", "frontend", []int{0, 5}},       // case insensitive
		{"xyz", "frontend", nil},
		{"", "frontend", nil},                  // empty pattern = no positions
		{"go", "Go", []int{0, 1}},
		{"fil", "Filter: Local", []int{0, 1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.target, func(t *testing.T) {
			got := FuzzyMatchPositions(tt.pattern, tt.target)
			if len(got) != len(tt.want) {
				t.Fatalf("FuzzyMatchPositions(%q, %q) = %v, want %v", tt.pattern, tt.target, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("FuzzyMatchPositions(%q, %q)[%d] = %d, want %d", tt.pattern, tt.target, i, got[i], tt.want[i])
				}
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test`
Expected: FAIL — `FuzzyMatchPositions` undefined

**Step 3: Write minimal implementation**

Add to `internal/workspace/workspace.go` after `FuzzyMatch`:

```go
// FuzzyMatchPositions returns the indices in target where each pattern character
// matched (case-insensitive). Returns nil if no match.
func FuzzyMatchPositions(pattern, target string) []int {
	if pattern == "" {
		return nil
	}
	patternLower := []rune(strings.ToLower(pattern))
	targetLower := []rune(strings.ToLower(target))
	positions := make([]int, 0, len(patternLower))
	pi := 0
	for i, r := range targetLower {
		if r == patternLower[pi] {
			positions = append(positions, i)
			pi++
			if pi == len(patternLower) {
				return positions
			}
		}
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `just test`
Expected: PASS

**Step 5: Commit**

```
feat: add FuzzyMatchPositions for highlight support
```

---

### Task 2: Extend paletteCommand struct with label, desc, key

**Files:**
- Modify: `internal/cli/mc_palette.go:39-83`

**Step 1: Update the struct**

In `internal/cli/mc_palette.go`, change `paletteCommand`:

```go
type paletteCommand struct {
	name  string         // internal identifier (kept for test compat)
	label string         // human-readable display name
	desc  string         // short description
	key   string         // direct keybinding hint (e.g. "⏎", "o")
	scope paletteScope
	run   func(m mcModel) (mcModel, tea.Cmd)
}
```

**Step 2: Update the registry**

Replace `paletteCommands()` with enriched entries:

```go
func paletteCommands() []paletteCommand {
	return []paletteCommand{
		{name: "go", label: "Go", desc: "cd into worktree", key: "⏎", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doGo() }},
		{name: "open", label: "Open in Editor", desc: "open in $EDITOR", key: "o", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doOpen() }},
		{name: "github", label: "View on GitHub", desc: "open PR in browser", key: "", scope: scopeHasPR, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doOpenPR() }},
		{name: "board", label: "Board", desc: "add to IDE workspace", key: "b", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doBoard() }},
		{name: "unboard", label: "Unboard", desc: "remove from IDE workspace", key: "b", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doUnboard() }},
		{name: "undock", label: "Undock", desc: "remove worktree", key: "d", scope: scopeWorktree, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doDelete() }},
		{name: "dock", label: "Dock", desc: "create worktree from PR", key: "d", scope: scopeGhostPR, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doCreateWorktree() }},
		{name: "copy-path", label: "Copy Path", desc: "copy worktree path", key: "", scope: scopeWorktree, run: paletteCmdCopyPath},
		{name: "open-repo", label: "View Repo on GitHub", desc: "open repo in browser", key: "", scope: scopeRepo, run: paletteCmdOpenRepo},
		{name: "fetch", label: "Fetch", desc: "fetch PR data for repo", key: "", scope: scopeRepo, run: paletteCmdFetch},
		{name: "filter-local", label: "Filter: Local", desc: "toggle local filter", key: "1", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) {
			m.activeFilters ^= filterLocal
			m.ensureCursorOnVisible()
			return m, nil
		}},
		{name: "filter-mine", label: "Filter: Mine", desc: "toggle my PRs filter", key: "2", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) {
			m.activeFilters ^= filterMine
			m.ensureCursorOnVisible()
			return m, nil
		}},
		{name: "filter-review", label: "Filter: Review Requested", desc: "toggle review filter", key: "3", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) {
			m.activeFilters ^= filterReviewReq
			m.ensureCursorOnVisible()
			return m, nil
		}},
		{name: "filter-dirty", label: "Filter: Dirty", desc: "toggle dirty filter", key: "4", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) {
			m.activeFilters ^= filterDirty
			m.ensureCursorOnVisible()
			return m, nil
		}},
		{name: "refresh", label: "Refresh", desc: "refresh all data", key: "r", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doRefresh() }},
		{name: "debrief", label: "Debrief", desc: "clean up merged branches", key: "", scope: scopeAlways, run: func(m mcModel) (mcModel, tea.Cmd) { return m.doRefresh() }},
		{name: "fetch-all", label: "Fetch All", desc: "fetch all repos", key: "", scope: scopeAlways, run: paletteCmdFetchAll},
	}
}
```

**Step 3: Update fuzzy filter to match against label**

In `availableCommands()`, change the text filter line from:

```go
if filter != "" && !workspace.FuzzyMatch(filter, cmd.name) {
```

to:

```go
if filter != "" && !workspace.FuzzyMatch(filter, cmd.label) {
```

**Step 4: Run tests to verify nothing breaks**

Run: `just test`
Expected: PASS (existing tests use `cmd.name` which is unchanged)

**Step 5: Commit**

```
feat: enrich paletteCommand with label, desc, key fields
```

---

### Task 3: Smart sort — context-relevant commands first

**Files:**
- Modify: `internal/cli/mc_palette.go` (the `availableCommands` function)
- Test: `internal/cli/mc_palette_test.go`

**Step 1: Write the failing test**

Add to `internal/cli/mc_palette_test.go`:

```go
func TestAvailableCommands_SmartSort(t *testing.T) {
	m := mcModel{
		cursor: 0,
		rows: []mcRow{
			{kind: rowWorktree, repo: "r", wt: "feat", loaded: true},
		},
	}

	cmds := m.availableCommands()

	// Find the boundary: context commands should come before global commands.
	lastContext := -1
	firstGlobal := -1
	for i, cmd := range cmds {
		if cmd.scope != scopeAlways {
			lastContext = i
		}
		if cmd.scope == scopeAlways && firstGlobal == -1 {
			firstGlobal = i
		}
	}

	if lastContext >= firstGlobal && firstGlobal >= 0 {
		t.Errorf("context commands should sort before global: lastContext=%d, firstGlobal=%d", lastContext, firstGlobal)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test`
Expected: FAIL — currently commands are in registry order, not sorted by scope relevance

**Step 3: Implement smart sort**

In `availableCommands()`, after building the `out` slice, add sorting:

```go
// Smart sort: context-relevant commands first, then global.
sort.SliceStable(out, func(i, j int) bool {
	iContext := out[i].scope != scopeAlways
	jContext := out[j].scope != scopeAlways
	if iContext != jContext {
		return iContext
	}
	return false
})
```

Add `"sort"` to the imports.

**Step 4: Run tests to verify pass**

Run: `just test`
Expected: PASS

**Step 5: Commit**

```
feat: smart-sort palette commands — context first, then global
```

---

### Task 4: Add paletteOffset for scroll window tracking

**Files:**
- Modify: `internal/cli/mc_model.go:98-100`
- Modify: `internal/cli/mc_palette.go` (handlePaletteKey, paletteHeight)

**Step 1: Add paletteOffset field**

In `internal/cli/mc_model.go`, add `paletteOffset int` to the model struct, below `paletteCursor`:

```go
paletteActive bool
paletteInput  textinput.Model
paletteCursor int
paletteOffset int
```

**Step 2: Update handlePaletteKey to maintain scroll window**

In `internal/cli/mc_palette.go`, update the key handler. After cursor movement (up/down), adjust offset:

For the `"up"` case, after decrementing cursor:
```go
case "up":
	if m.paletteCursor > 0 {
		m.paletteCursor--
	}
	if m.paletteCursor < m.paletteOffset {
		m.paletteOffset = m.paletteCursor
	}
	return m, nil
```

For the `"down"` case:
```go
case "down":
	cmds := m.availableCommands()
	if m.paletteCursor < len(cmds)-1 {
		m.paletteCursor++
	}
	if m.paletteCursor >= m.paletteOffset+paletteMaxVisible {
		m.paletteOffset = m.paletteCursor - paletteMaxVisible + 1
	}
	return m, nil
```

For the `default` case (text input changes), reset offset:
```go
default:
	var cmd tea.Cmd
	m.paletteInput, cmd = m.paletteInput.Update(msg)
	m.paletteCursor = 0
	m.paletteOffset = 0
	return m, cmd
```

For `"esc"`, reset offset:
```go
case "esc":
	m.paletteInput.SetValue("")
	m.paletteInput.Blur()
	m.paletteActive = false
	m.paletteCursor = 0
	m.paletteOffset = 0
	return m, nil
```

For `"enter"`, reset offset:
```go
m.paletteCursor = 0
m.paletteOffset = 0
```

**Step 3: Run tests to verify nothing breaks**

Run: `just test`
Expected: PASS

**Step 4: Commit**

```
feat: add paletteOffset for scroll window tracking
```

---

### Task 5: Rewrite renderPalette with rich rendering

**Files:**
- Modify: `internal/cli/mc_palette.go` (the `renderPalette` and `paletteHeight` functions)
- Modify: `internal/cli/mc_view.go` (import if needed)

**Step 1: Add scope display name helper**

Add to `internal/cli/mc_palette.go`:

```go
func scopeLabel(s paletteScope) string {
	switch s {
	case scopeWorktree:
		return "worktree"
	case scopeGhostPR:
		return "remote"
	case scopeHasPR:
		return "pr"
	case scopeRepo:
		return "repo"
	default:
		return ""
	}
}
```

**Step 2: Add fuzzy highlight renderer**

Add to `internal/cli/mc_palette.go`:

```go
func renderFuzzyHighlight(label string, filter string, baseStyle, matchStyle lipgloss.Style) string {
	if filter == "" {
		return baseStyle.Render(label)
	}
	positions := workspace.FuzzyMatchPositions(filter, label)
	if positions == nil {
		return baseStyle.Render(label)
	}

	posSet := make(map[int]bool, len(positions))
	for _, p := range positions {
		posSet[p] = true
	}

	var b strings.Builder
	for i, r := range label {
		if posSet[i] {
			b.WriteString(matchStyle.Render(string(r)))
		} else {
			b.WriteString(baseStyle.Render(string(r)))
		}
	}
	return b.String()
}
```

**Step 3: Rewrite renderPalette**

Replace the `renderPalette` function:

```go
func (m mcModel) renderPalette() string {
	cmds := m.availableCommands()
	total := len(cmds)

	var b strings.Builder

	border := ui.Dim.Render(strings.Repeat("─", m.width))
	b.WriteString(border + "\n")

	if total == 0 {
		b.WriteString(ui.Dim.Render("  no matches") + "\n")
		prompt := ui.Dim.Render(":") + " " + m.paletteInput.View()
		b.WriteString(prompt)
		return b.String()
	}

	visible := min(total, paletteMaxVisible)
	end := m.paletteOffset + visible
	if end > total {
		end = total
	}

	// Scroll indicator: up
	if m.paletteOffset > 0 {
		b.WriteString(ui.Dim.Render("  ▲") + "\n")
	}

	filter := m.paletteInput.Value()

	selectedBg := lipgloss.NewStyle().Background(lipgloss.Color("236"))
	dimStyle := lipgloss.NewStyle().Faint(true)
	matchStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	keyStyle := lipgloss.NewStyle().Faint(true).Width(3).Align(lipgloss.Right)
	scopeStyle := lipgloss.NewStyle().Faint(true)

	for i := m.paletteOffset; i < end; i++ {
		cmd := cmds[i]
		selected := i == m.paletteCursor

		// Key hint (3 chars wide, right-aligned)
		keyCol := keyStyle.Render(cmd.key)

		// Label with fuzzy highlight
		var labelCol string
		if selected {
			labelBase := lipgloss.NewStyle().Bold(true)
			labelCol = renderFuzzyHighlight(cmd.label, filter, labelBase, matchStyle)
		} else {
			labelCol = renderFuzzyHighlight(cmd.label, filter, lipgloss.NewStyle(), matchStyle)
		}

		// Description (dim)
		descCol := dimStyle.Render(cmd.desc)

		// Scope tag (very dim, only for non-global)
		scopeCol := ""
		if sl := scopeLabel(cmd.scope); sl != "" {
			scopeCol = scopeStyle.Render(sl)
		}

		// Compose the line
		line := "  " + keyCol + "  " + labelCol + "  " + descCol
		if scopeCol != "" {
			lineWidth := lipgloss.Width(line)
			scopeWidth := lipgloss.Width(scopeCol)
			pad := max(m.width-lineWidth-scopeWidth-2, 1)
			line += strings.Repeat(" ", pad) + scopeCol
		}

		if selected {
			// Pad line to full width for background highlight
			lineWidth := lipgloss.Width(line)
			if lineWidth < m.width {
				line += strings.Repeat(" ", m.width-lineWidth)
			}
			line = selectedBg.Render(line)
		}

		b.WriteString(line + "\n")
	}

	// Scroll indicator: down
	if end < total {
		b.WriteString(ui.Dim.Render("  ▼") + "\n")
	}

	prompt := ui.Dim.Render(":") + " " + m.paletteInput.View()
	b.WriteString(prompt)

	return b.String()
}
```

**Step 4: Update paletteHeight to account for scroll indicators and empty state**

```go
func (m mcModel) paletteHeight() int {
	cmds := m.availableCommands()
	total := len(cmds)

	if total == 0 {
		return 3 // border + "no matches" + input line
	}

	visible := min(total, paletteMaxVisible)
	h := visible + 2 // commands + border + input line

	// Scroll indicators
	if m.paletteOffset > 0 {
		h++ // ▲
	}
	if m.paletteOffset+visible < total {
		h++ // ▼
	}

	return h
}
```

**Step 5: Run tests and build**

Run: `just test && just build`
Expected: PASS + successful build

**Step 6: Commit**

```
feat: rich palette rendering with highlights, descriptions, scroll
```

---

### Task 6: Update existing tests for label-based filtering

**Files:**
- Modify: `internal/cli/mc_palette_test.go`

**Step 1: Update TestAvailableCommands_TextFilter**

The text filter now matches against `label` instead of `name`. Update the test:

```go
func TestAvailableCommands_TextFilter(t *testing.T) {
	m := mcModel{
		cursor: 0,
		rows: []mcRow{
			{kind: rowWorktree, repo: "r", wt: "feat"},
		},
	}
	m.paletteInput.SetValue("filt")

	cmds := m.availableCommands()
	names := cmdNames(cmds)

	// Should match Filter: * commands (fuzzy match on label "Filter: Local" etc.)
	if !contains(names, "filter-local") {
		t.Error("expected 'filter-local' to match 'filt' against label 'Filter: Local'")
	}
	// Should not match non-matching commands
	if contains(names, "go") {
		t.Error("'go' (label 'Go') should not match 'filt'")
	}
	if contains(names, "refresh") {
		t.Error("'refresh' (label 'Refresh') should not match 'filt'")
	}
}
```

**Step 2: Run tests**

Run: `just test`
Expected: PASS

**Step 3: Commit**

```
test: update palette tests for label-based filtering
```

---

### Task 7: Manual smoke test and final polish

**Step 1: Build and run**

Run: `just build && bin/workspace mc`

**Step 2: Verify visually**

- Press `:` — palette should open with rich rendering
- Context commands for current row type should appear first
- Each row shows: key hint | label | description | scope tag
- Type a filter — matched characters should highlight in orange/bold
- Arrow up/down — scroll indicators appear when list overflows
- Filter to no matches — "no matches" message appears
- Press Esc — palette closes cleanly

**Step 3: Fix any visual issues found during smoke test**

Adjust padding, alignment, colors as needed.

**Step 4: Commit any fixes**

```
fix: palette visual polish from smoke test
```
