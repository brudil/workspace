# Silo Watcher Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix two silo watcher bugs: (1) cross-worktree git events triggering unnecessary resyncs, and (2) TUI not reflecting config changes when `ws silo point` is run in another terminal.

**Architecture:** Bug 1 is fixed by watching the per-worktree `HEAD` file instead of the `index` file — HEAD only changes for operations that modify the working tree (checkout, pull, rebase), while index changes for many reasons including git maintenance and stat cache refreshes that fire across all worktrees. Bug 2 is fixed by adding an `OnTargetsChanged` callback from the watcher's `reloadTargets` method, piping it through a channel to the Bubble Tea TUI model, which rebuilds its repo list on receipt.

**Tech Stack:** Go, fsnotify, Bubble Tea (charmbracelet)

---

### Task 1: Watch HEAD instead of index to fix cross-worktree triggers

**Files:**
- Modify: `internal/workspace/watcher.go:225-243` (handleEvent git detection)
- Modify: `internal/workspace/watcher.go:140-154` (rename watchGitIndex, update comments)
- Create: `internal/workspace/watcher_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/workspace/watcher_test.go`:

```go
package workspace

import (
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestHandleEvent_HeadChangeTriggers(t *testing.T) {
	sw := &SiloWatcher{
		gitdirToRepo: map[string]string{"/fake/gitdir": "myrepo"},
		gitDebounce:  make(map[string]*time.Timer),
		lastFullSync: make(map[string]time.Time),
		targets:      map[string]string{"myrepo": "capsule-a"},
		log:          log.New(os.Stderr, "", 0),
	}

	event := fsnotify.Event{
		Name: "/fake/gitdir/HEAD",
		Op:   fsnotify.Write,
	}
	sw.handleEvent(event, "/nonexistent/localpath")

	sw.mu.Lock()
	timer, ok := sw.gitDebounce["myrepo"]
	sw.mu.Unlock()

	if !ok {
		t.Fatal("HEAD event should schedule a git debounce timer")
	}
	timer.Stop()
}

func TestHandleEvent_IndexChangeIgnored(t *testing.T) {
	sw := &SiloWatcher{
		gitdirToRepo: map[string]string{"/fake/gitdir": "myrepo"},
		gitDebounce:  make(map[string]*time.Timer),
		lastFullSync: make(map[string]time.Time),
		targets:      map[string]string{"myrepo": "capsule-a"},
		log:          log.New(os.Stderr, "", 0),
	}

	event := fsnotify.Event{
		Name: "/fake/gitdir/index",
		Op:   fsnotify.Write,
	}
	sw.handleEvent(event, "/nonexistent/localpath")

	sw.mu.Lock()
	_, ok := sw.gitDebounce["myrepo"]
	sw.mu.Unlock()

	if ok {
		t.Fatal("index events should NOT trigger git debounce (only HEAD events should)")
	}
}

func TestHandleEvent_HeadSuppressedAfterResync(t *testing.T) {
	sw := &SiloWatcher{
		gitdirToRepo: map[string]string{"/fake/gitdir": "myrepo"},
		gitDebounce:  make(map[string]*time.Timer),
		lastFullSync: map[string]time.Time{"myrepo": time.Now()},
		targets:      map[string]string{"myrepo": "capsule-a"},
		log:          log.New(os.Stderr, "", 0),
	}

	event := fsnotify.Event{
		Name: "/fake/gitdir/HEAD",
		Op:   fsnotify.Write,
	}
	sw.handleEvent(event, "/nonexistent/localpath")

	sw.mu.Lock()
	_, ok := sw.gitDebounce["myrepo"]
	sw.mu.Unlock()

	if ok {
		t.Fatal("HEAD events should be suppressed within 2s of a fullResync")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test`

Expected: `TestHandleEvent_HeadChangeTriggers` FAILS (HEAD events are not detected — code checks for "index"). `TestHandleEvent_IndexChangeIgnored` PASSES (index events DO trigger, which is current behavior — this test asserts they should NOT). `TestHandleEvent_HeadSuppressedAfterResync` FAILS.

- [ ] **Step 3: Change handleEvent to watch HEAD instead of index**

In `internal/workspace/watcher.go`, replace the git event detection block (lines 223-245):

```go
	// Check if this is a HEAD change from a watched gitdir.
	// HEAD lives at <gitdir>/HEAD (e.g. .bare/worktrees/<name>/HEAD).
	// We watch HEAD instead of the index because index changes fire for many
	// reasons (git gc, stat cache refreshes) including from OTHER worktrees,
	// while HEAD only changes for operations that modify the working tree
	// (checkout, pull, rebase, merge, reset) in THIS worktree.
	if filepath.Base(event.Name) == "HEAD" {
		gitdir := filepath.Dir(event.Name)
		if repo, ok := sw.gitdirToRepo[gitdir]; ok {
			sw.verbose("%s: HEAD event", repo)
			// Ignore HEAD events that arrive shortly after a full resync
			// to prevent potential feedback loops.
			if last, ok := sw.lastFullSync[repo]; ok && time.Since(last) < 2*time.Second {
				sw.verbose("%s: HEAD event suppressed (fullResync %.1fs ago)", repo, time.Since(last).Seconds())
				return
			}
			if t, ok := sw.gitDebounce[repo]; ok {
				t.Stop()
			}
			sw.verbose("%s: HEAD debounce scheduled (500ms)", repo)
			r := repo
			sw.gitDebounce[repo] = time.AfterFunc(500*time.Millisecond, func() {
				sw.fullResync(r, "HEAD change")
			})
			return
		}
	}
```

- [ ] **Step 4: Update watchGitIndex comment**

In `internal/workspace/watcher.go`, update the comment on `watchGitIndex` (line 141-142):

```go
// watchGitIndex resolves the real gitdir for a worktree and watches it
// for HEAD changes (triggered by checkout, pull, rebase, etc.).
```

And the comment on `gitdirToRepo` (line 44):

```go
	// Git HEAD watching: map from resolved gitdir path to repo name,
	// so we can match .bare/worktrees/<name>/HEAD events back to a repo.
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `just test`

Expected: All three new tests PASS. All existing tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/workspace/watcher.go internal/workspace/watcher_test.go
git commit -m "fix(silo): watch HEAD instead of index to prevent cross-worktree triggers"
```

---

### Task 2: Add OnTargetsChanged callback to SiloWatcher

**Files:**
- Modify: `internal/workspace/watcher.go:26-48` (add callback field)
- Modify: `internal/workspace/watcher.go:436-481` (call callback from reloadTargets)
- Modify: `internal/workspace/watcher_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/workspace/watcher_test.go`:

```go
func TestReloadTargets_CallsOnTargetsChanged(t *testing.T) {
	root := t.TempDir()

	// Create directories that reloadTargets/addWatch will walk
	os.MkdirAll(filepath.Join(root, "repos", "repo-a", "capsule-2"), 0755)
	os.MkdirAll(filepath.Join(root, "repos", "repo-a", ".silo"), 0755)
	os.MkdirAll(filepath.Join(root, "repos", "repo-a", ".ground"), 0755)

	// Write ws.local.toml with a CHANGED target
	os.WriteFile(filepath.Join(root, "ws.local.toml"), []byte("[silo]\nrepo-a = \"capsule-2\"\n"), 0644)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	var received map[string]string
	sw := &SiloWatcher{
		Root:         root,
		RepoDir:      func(name string) string { return filepath.Join(root, "repos", name) },
		SiloWorktree: func(name string) string { return filepath.Join(root, "repos", name, ".silo") },
		MainWorktree: func(name string) string { return filepath.Join(root, "repos", name, ".ground") },
		watcher:      watcher,
		targets:      map[string]string{"repo-a": "capsule-1"},
		gitdirToRepo: make(map[string]string),
		gitDebounce:  make(map[string]*time.Timer),
		lastFullSync: make(map[string]time.Time),
		debounce:     make(map[string]*time.Timer),
		pending:      make(map[string]map[string]bool),
		log:          log.New(os.Stderr, "", 0),
		OnTargetsChanged: func(targets map[string]string) {
			received = targets
		},
	}

	sw.reloadTargets()

	if received == nil {
		t.Fatal("OnTargetsChanged was not called")
	}
	if received["repo-a"] != "capsule-2" {
		t.Errorf("repo-a = %q, want %q", received["repo-a"], "capsule-2")
	}
}

func TestReloadTargets_NoChangeNoCallback(t *testing.T) {
	root := t.TempDir()

	// Write ws.local.toml with the SAME target (no change)
	os.WriteFile(filepath.Join(root, "ws.local.toml"), []byte("[silo]\nrepo-a = \"capsule-1\"\n"), 0644)

	called := false
	sw := &SiloWatcher{
		Root:         root,
		RepoDir:      func(name string) string { return filepath.Join(root, "repos", name) },
		SiloWorktree: func(name string) string { return filepath.Join(root, "repos", name, ".silo") },
		MainWorktree: func(name string) string { return filepath.Join(root, "repos", name, ".ground") },
		targets:      map[string]string{"repo-a": "capsule-1"},
		gitdirToRepo: make(map[string]string),
		gitDebounce:  make(map[string]*time.Timer),
		lastFullSync: make(map[string]time.Time),
		debounce:     make(map[string]*time.Timer),
		pending:      make(map[string]map[string]bool),
		log:          log.New(os.Stderr, "", 0),
		OnTargetsChanged: func(targets map[string]string) {
			called = true
		},
	}

	sw.reloadTargets()

	if called {
		t.Fatal("OnTargetsChanged should not be called when targets are unchanged")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test`

Expected: `TestReloadTargets_CallsOnTargetsChanged` FAILS — `OnTargetsChanged` field does not exist on `SiloWatcher`. Compilation error.

- [ ] **Step 3: Add OnTargetsChanged field to SiloWatcher**

In `internal/workspace/watcher.go`, add the field to the struct (after `OnSync`):

```go
	OnSync           func(SyncEvent) // optional callback for sync events
	OnTargetsChanged func(map[string]string) // optional callback when targets change
```

- [ ] **Step 4: Call OnTargetsChanged from reloadTargets**

In `internal/workspace/watcher.go`, modify `reloadTargets` to track changes and fire the callback. Replace the function body:

```go
func (sw *SiloWatcher) reloadTargets() {
	localPath := filepath.Join(sw.Root, "ws.local.toml")
	var local struct {
		Silo map[string]string `toml:"silo"`
	}
	if _, err := toml.DecodeFile(localPath, &local); err != nil {
		sw.log.Printf("  error reading config: %v", err)
		return
	}
	if local.Silo == nil {
		local.Silo = make(map[string]string)
	}

	sw.mu.Lock()
	oldTargets := make(map[string]string)
	for k, v := range sw.targets {
		oldTargets[k] = v
	}
	sw.mu.Unlock()

	changed := false

	for repo := range oldTargets {
		if _, ok := local.Silo[repo]; !ok {
			sw.log.Printf("  removing watch for %s", repo)
			sw.removeWatch(repo)
			changed = true
		}
	}

	for repo, capsule := range local.Silo {
		old, existed := oldTargets[repo]
		if !existed || old != capsule {
			if existed {
				sw.removeWatch(repo)
			}
			sw.log.Printf("  target changed: %s -> %s", repo, capsule)
			capsuleDir := filepath.Join(sw.RepoDir(repo), capsule)
			siloDir := sw.SiloWorktree(repo)
			if _, err := FullSync(capsuleDir, siloDir); err != nil {
				sw.log.Printf("  re-sync error for %s: %v", repo, err)
			}
			sw.runSwitchHooks(repo, siloDir)
			if err := sw.addWatch(repo, capsule); err != nil {
				sw.log.Printf("  warning: could not watch %s: %v", repo, err)
			}
			changed = true
		}
	}

	if changed && sw.OnTargetsChanged != nil {
		sw.mu.Lock()
		snapshot := make(map[string]string, len(sw.targets))
		for k, v := range sw.targets {
			snapshot[k] = v
		}
		sw.mu.Unlock()
		sw.OnTargetsChanged(snapshot)
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `just test`

Expected: `TestReloadTargets_CallsOnTargetsChanged` and `TestReloadTargets_NoChangeNoCallback` PASS. All existing tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/workspace/watcher.go internal/workspace/watcher_test.go
git commit -m "feat(silo): add OnTargetsChanged callback to SiloWatcher"
```

---

### Task 3: Wire targets-changed notification through to TUI

**Files:**
- Modify: `internal/cli/silo_watch.go:16-61` (add targetsCh, repoNames, formatRepoName to model)
- Modify: `internal/cli/silo_watch.go:63-120` (add waitForTargetsChanged, handle message in Update)
- Modify: `internal/cli/silo.go:229-256` (create targetsCh, wire callback, update constructor)
- Create: `internal/cli/silo_watch_test.go`

- [ ] **Step 1: Write failing tests for TUI model**

Create `internal/cli/silo_watch_test.go`:

```go
package cli

import (
	"testing"
	"time"
)

func TestSiloWatchModel_TargetsChanged_UpdatesCapsule(t *testing.T) {
	m := siloWatchModel{
		repoNames:      []string{"repo-a", "repo-b"},
		formatRepoName: func(name string) string { return name },
		repos: []siloRepoView{
			{name: "repo-a", displayName: "repo-a", capsule: "capsule-old",
				history: []syncEntry{{fileCount: 5, time: time.Now()}}},
		},
	}

	msg := targetsChangedMsg(map[string]string{
		"repo-a": "capsule-new",
		"repo-b": "capsule-b",
	})

	updated, _ := m.Update(msg)
	m = updated.(siloWatchModel)

	if len(m.repos) != 2 {
		t.Fatalf("repos count = %d, want 2", len(m.repos))
	}
	if m.repos[0].capsule != "capsule-new" {
		t.Errorf("repo-a capsule = %q, want %q", m.repos[0].capsule, "capsule-new")
	}
	// History preserved for existing repo
	if len(m.repos[0].history) != 1 {
		t.Errorf("repo-a history len = %d, want 1", len(m.repos[0].history))
	}
	if m.repos[1].name != "repo-b" {
		t.Errorf("repos[1].name = %q, want %q", m.repos[1].name, "repo-b")
	}
	if m.repos[1].capsule != "capsule-b" {
		t.Errorf("repos[1].capsule = %q, want %q", m.repos[1].capsule, "capsule-b")
	}
}

func TestSiloWatchModel_TargetsChanged_RepoRemoved(t *testing.T) {
	m := siloWatchModel{
		repoNames:      []string{"repo-a", "repo-b"},
		formatRepoName: func(name string) string { return name },
		repos: []siloRepoView{
			{name: "repo-a", displayName: "repo-a", capsule: "capsule-a"},
			{name: "repo-b", displayName: "repo-b", capsule: "capsule-b"},
		},
	}

	msg := targetsChangedMsg(map[string]string{
		"repo-a": "capsule-a",
	})

	updated, _ := m.Update(msg)
	m = updated.(siloWatchModel)

	if len(m.repos) != 1 {
		t.Fatalf("repos count = %d, want 1", len(m.repos))
	}
	if m.repos[0].name != "repo-a" {
		t.Errorf("repos[0].name = %q, want %q", m.repos[0].name, "repo-a")
	}
}

func TestSiloWatchModel_TargetsChanged_OrderFollowsRepoNames(t *testing.T) {
	m := siloWatchModel{
		repoNames:      []string{"aaa", "bbb", "ccc"},
		formatRepoName: func(name string) string { return name },
		repos:          []siloRepoView{},
	}

	// Add repos in reverse order — output should follow repoNames order
	msg := targetsChangedMsg(map[string]string{
		"ccc": "capsule-c",
		"aaa": "capsule-a",
	})

	updated, _ := m.Update(msg)
	m = updated.(siloWatchModel)

	if len(m.repos) != 2 {
		t.Fatalf("repos count = %d, want 2", len(m.repos))
	}
	if m.repos[0].name != "aaa" {
		t.Errorf("repos[0].name = %q, want %q", m.repos[0].name, "aaa")
	}
	if m.repos[1].name != "ccc" {
		t.Errorf("repos[1].name = %q, want %q", m.repos[1].name, "ccc")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test`

Expected: Compilation error — `targetsChangedMsg`, `repoNames`, and `formatRepoName` do not exist.

- [ ] **Step 3: Add new fields and message type to silo_watch.go**

In `internal/cli/silo_watch.go`, add the message type after `resyncDoneMsg`:

```go
type targetsChangedMsg map[string]string
```

Update the `siloWatchModel` struct to add the new fields:

```go
type siloWatchModel struct {
	repoNames      []string
	formatRepoName func(string) string
	repos          []siloRepoView
	spinner        spinner.Model
	syncCh         <-chan workspace.SyncEvent
	targetsCh      <-chan map[string]string
	onResync       func() // triggers FullResyncAll in a goroutine
	resyncing      bool
	lastResync     time.Time
}
```

- [ ] **Step 4: Update newSiloWatchModel constructor**

Replace the `newSiloWatchModel` function signature and body:

```go
func newSiloWatchModel(ws *workspace.Workspace, syncCh <-chan workspace.SyncEvent, targetsCh <-chan map[string]string, onResync func()) siloWatchModel {
	var repos []siloRepoView
	for _, name := range ws.RepoNames {
		capsule, ok := ws.Silo[name]
		if !ok {
			continue
		}
		repos = append(repos, siloRepoView{
			name:        name,
			displayName: ws.FormatRepoName(name),
			capsule:     capsule,
		})
	}
	s := spinner.New()
	s.Spinner = spinner.Dot
	return siloWatchModel{
		repoNames:      ws.RepoNames,
		formatRepoName: ws.FormatRepoName,
		repos:          repos,
		spinner:        s,
		syncCh:         syncCh,
		targetsCh:      targetsCh,
		onResync:       onResync,
	}
}
```

- [ ] **Step 5: Add waitForTargetsChanged and Init**

Add the new command method:

```go
func (m siloWatchModel) waitForTargetsChanged() tea.Cmd {
	ch := m.targetsCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		targets, ok := <-ch
		if !ok {
			return nil
		}
		return targetsChangedMsg(targets)
	}
}
```

Update `Init` to also wait for target changes:

```go
func (m siloWatchModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.waitForSync(), m.waitForTargetsChanged())
}
```

- [ ] **Step 6: Handle targetsChangedMsg in Update**

Add a new case in the `Update` method's type switch, after the `syncEventMsg` case:

```go
	case targetsChangedMsg:
		oldRepos := make(map[string]siloRepoView)
		for _, r := range m.repos {
			oldRepos[r.name] = r
		}
		var newRepos []siloRepoView
		for _, name := range m.repoNames {
			capsule, ok := msg[name]
			if !ok {
				continue
			}
			if old, existed := oldRepos[name]; existed {
				old.capsule = capsule
				newRepos = append(newRepos, old)
			} else {
				newRepos = append(newRepos, siloRepoView{
					name:        name,
					displayName: m.formatRepoName(name),
					capsule:     capsule,
				})
			}
		}
		m.repos = newRepos
		return m, m.waitForTargetsChanged()
```

- [ ] **Step 7: Wire targetsCh in silo.go**

In `internal/cli/silo.go`, in the `newSiloWatchCmd` function's interactive branch (around line 237), create the targets channel, wire the callback, and update the constructor call:

```go
			syncCh := make(chan workspace.SyncEvent, 64)
			watcher.OnSync = func(ev workspace.SyncEvent) {
				select {
				case syncCh <- ev:
				default: // drop if UI is behind
				}
			}

			targetsCh := make(chan map[string]string, 8)
			watcher.OnTargetsChanged = func(targets map[string]string) {
				select {
				case targetsCh <- targets:
				default:
				}
			}

			go func() {
				watcher.Watch(stop, ctx.WS.Silo)
				close(syncCh)
				close(targetsCh)
			}()

			m := newSiloWatchModel(ctx.WS, syncCh, targetsCh, watcher.FullResyncAll)
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `just test`

Expected: All new tests PASS. All existing tests PASS.

- [ ] **Step 9: Build and verify**

Run: `just build`

Expected: Clean build.

- [ ] **Step 10: Commit**

```bash
git add internal/cli/silo.go internal/cli/silo_watch.go internal/cli/silo_watch_test.go
git commit -m "feat(silo): wire targets-changed events to TUI for live config updates"
```
