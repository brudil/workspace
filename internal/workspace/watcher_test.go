package workspace

import (
	"log"
	"os"
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
