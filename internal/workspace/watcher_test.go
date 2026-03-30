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
		RepoDir:      func(name string) string { return "/fake/repos/" + name },
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
