# Command Palette Overhaul

## Goal

Make the Mission Control command palette feel polished and delightful — richer information, better visual hierarchy, smarter ordering — while keeping the bottom-anchored vim command-line aesthetic.

## Changes

### 1. Human-readable command labels

Replace slug names (`open-repo`, `filter-local`) with proper labels (`View Repo on GitHub`, `Filter: Local`). Fuzzy matching runs against these labels.

Full label mapping:

- go → Go
- open → Open in Editor
- github → View on GitHub
- board → Board
- unboard → Unboard
- undock → Undock
- dock → Dock
- copy-path → Copy Path
- open-repo → View Repo on GitHub
- fetch → Fetch
- filter-local → Filter: Local
- filter-mine → Filter: Mine
- filter-review → Filter: Review Requested
- filter-dirty → Filter: Dirty
- refresh → Refresh
- debrief → Debrief
- fetch-all → Fetch All

### 2. Enriched data model

Extend `paletteCommand` with:

- `label` — human-readable display name (replaces `name` for rendering)
- `desc` — short description (e.g. "cd into worktree")
- `key` — direct keybinding hint (e.g. "⏎", "o", "1")

Keep `name` as the internal identifier for existing logic.

### 3. Smart sorting

Commands sort context-first:

1. Commands whose scope matches the current row type (worktree actions when on a worktree, ghost PR actions when on a ghost PR)
2. Global (`scopeAlways`) commands after

Within each group, preserve registry order.

### 4. Rich row rendering

Each palette row layout:

```
  ⏎  Go              cd into worktree                 worktree
  o  Open in Editor   open in $EDITOR                  worktree
  b  Board            add to IDE workspace             worktree
```

Columns:
- Key hint (2ch, dim, right-aligned in fixed gutter)
- Label (bold when selected)
- Description (dim)
- Scope tag (very dim, right-aligned)

Selected row: background highlight (color 236) spanning full width, label rendered bold + brighter.

### 5. Fuzzy match highlighting

When user types a filter, matched characters in the label are highlighted with a distinct color/bold style. Unmatched characters render normally.

Requires extending `FuzzyMatch` (or adding a new helper) to return match positions, not just a bool.

### 6. Scroll window tracking

Add `paletteOffset` to track visible window. When cursor moves beyond visible range, offset adjusts. Show dim `▲`/`▼` indicators when scrollable. Show dim "no matches" when filter returns empty.

## Non-goals

- No categories or section headers (flat list with smart sort)
- No extracted tea.Model component (keep inline in mcModel)
- No bubbles/list dependency
