package workspace

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/fsnotify/fsnotify"
)

// SyncEvent is emitted after a sync operation completes.
type SyncEvent struct {
	Repo      string
	Capsule   string
	FileCount int
	Time      time.Time
}

// SiloWatcher manages file watchers for all active silos.
type SiloWatcher struct {
	Root             string
	RepoDir          func(name string) string
	SiloWorktree     func(name string) string
	MainWorktree     func(name string) string
	AfterCreateHooks map[string]string
	DefaultBranch    string
	OnSync           func(SyncEvent) // optional callback for sync events

	watcher  *fsnotify.Watcher
	targets  map[string]string // repo -> capsule
	mu       sync.Mutex
	log      *log.Logger
	debounce map[string]*time.Timer     // repo -> debounce timer
	pending  map[string]map[string]bool // repo -> set of changed relative paths

	// Git index watching: map from resolved gitdir path to repo name,
	// so we can match .bare/worktrees/<name>/index events back to a repo.
	gitdirToRepo map[string]string      // gitdir path -> repo name
	gitDebounce  map[string]*time.Timer // repo -> git index debounce timer
}

func NewSiloWatcher(w *Workspace, logger *log.Logger) (*SiloWatcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating fsnotify watcher: %w", err)
	}
	return &SiloWatcher{
		Root:             w.Root,
		RepoDir:          w.RepoDir,
		SiloWorktree:     w.SiloWorktree,
		MainWorktree:     w.MainWorktree,
		AfterCreateHooks: w.AfterCreateHooks,
		DefaultBranch:    w.DefaultBranch,
		watcher:          fsw,
		targets:          make(map[string]string),
		log:              logger,
		debounce:         make(map[string]*time.Timer),
		pending:          make(map[string]map[string]bool),
		gitdirToRepo:     make(map[string]string),
		gitDebounce:      make(map[string]*time.Timer),
	}, nil
}

func (sw *SiloWatcher) Watch(stop <-chan struct{}, silo map[string]string) error {
	for repo, capsule := range silo {
		if err := sw.addWatch(repo, capsule); err != nil {
			sw.log.Printf("warning: could not watch %s: %v", repo, err)
		}
	}

	localPath := filepath.Join(sw.Root, "ws.local.toml")
	sw.watcher.Add(localPath)

	sw.log.Printf("Watching %d silo(s). Press Ctrl+C to stop.", len(silo))

	for {
		select {
		case <-stop:
			sw.watcher.Close()
			return nil
		case event, ok := <-sw.watcher.Events:
			if !ok {
				return nil
			}
			sw.handleEvent(event, localPath)
		case err, ok := <-sw.watcher.Errors:
			if !ok {
				return nil
			}
			sw.log.Printf("watcher error: %v", err)
		}
	}
}

func (sw *SiloWatcher) addWatch(repo, capsule string) error {
	capsuleDir := filepath.Join(sw.RepoDir(repo), capsule)
	err := filepath.Walk(capsuleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == "node_modules" || name == ".next" || name == ".git" {
				return filepath.SkipDir
			}
			return sw.watcher.Add(path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Watch the real gitdir for index changes. In a bare-repo worktree layout,
	// .git is a file containing "gitdir: <path>" pointing to .bare/worktrees/<name>/.
	sw.watchGitIndex(repo, capsuleDir)

	sw.mu.Lock()
	sw.targets[repo] = capsule
	sw.mu.Unlock()
	sw.log.Printf("  watching %s -> %s", repo, capsule)
	return nil
}

// watchGitIndex resolves the real gitdir for a worktree and watches it
// for index changes (triggered by pull, checkout, rebase, etc.).
func (sw *SiloWatcher) watchGitIndex(repo, capsuleDir string) {
	gitdir := resolveGitDir(capsuleDir)
	if gitdir == "" {
		return
	}
	if err := sw.watcher.Add(gitdir); err != nil {
		sw.log.Printf("  warning: could not watch gitdir for %s: %v", repo, err)
		return
	}
	sw.mu.Lock()
	sw.gitdirToRepo[gitdir] = repo
	sw.mu.Unlock()
}

// resolveGitDir returns the actual git directory for a worktree.
// For linked worktrees, .git is a file containing "gitdir: <path>".
// For regular repos, .git is a directory.
func resolveGitDir(wtPath string) string {
	gitPath := filepath.Join(wtPath, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return gitPath
	}
	// Linked worktree — .git is a file
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir: ") {
		return ""
	}
	gitdir := strings.TrimPrefix(line, "gitdir: ")
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(wtPath, gitdir)
	}
	gitdir = filepath.Clean(gitdir)
	return gitdir
}

func (sw *SiloWatcher) removeWatch(repo string) {
	sw.mu.Lock()
	capsule, ok := sw.targets[repo]
	if ok {
		delete(sw.targets, repo)
	}
	// Clean up gitdir mapping
	for gitdir, r := range sw.gitdirToRepo {
		if r == repo {
			sw.watcher.Remove(gitdir)
			delete(sw.gitdirToRepo, gitdir)
			break
		}
	}
	sw.mu.Unlock()
	if ok {
		capsuleDir := filepath.Join(sw.RepoDir(repo), capsule)
		filepath.Walk(capsuleDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || !info.IsDir() {
				return nil
			}
			sw.watcher.Remove(path)
			return nil
		})
	}
}

func (sw *SiloWatcher) handleEvent(event fsnotify.Event, localPath string) {
	if event.Name == localPath {
		if event.Has(fsnotify.Write) {
			sw.reloadTargets()
		}
		return
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	// Check if this is a git index change from a watched gitdir.
	// The index lives at <gitdir>/index (e.g. .bare/worktrees/<name>/index).
	if filepath.Base(event.Name) == "index" {
		gitdir := filepath.Dir(event.Name)
		if repo, ok := sw.gitdirToRepo[gitdir]; ok {
			if t, ok := sw.gitDebounce[repo]; ok {
				t.Stop()
			}
			r := repo
			sw.gitDebounce[repo] = time.AfterFunc(500*time.Millisecond, func() {
				sw.fullResync(r)
			})
			return
		}
	}

	for repo, capsule := range sw.targets {
		capsuleDir := filepath.Join(sw.RepoDir(repo), capsule)
		if !strings.HasPrefix(event.Name, capsuleDir+string(os.PathSeparator)) {
			continue
		}

		relPath, _ := filepath.Rel(capsuleDir, event.Name)

		// Skip .git events (the .git file itself, not the resolved gitdir)
		if strings.HasPrefix(relPath, ".git"+string(os.PathSeparator)) || relPath == ".git" {
			return
		}

		// Watch newly created directories
		if event.Has(fsnotify.Create) {
			if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
				sw.watcher.Add(event.Name)
				return
			}
		}

		if !IsGitTracked(capsuleDir, relPath) {
			return
		}

		if sw.pending[repo] == nil {
			sw.pending[repo] = make(map[string]bool)
		}

		if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
			// Don't blindly delete — the file may have been replaced (git uses
			// rename-to-temp + rename-into-place). Mark it for a check at flush time.
			sw.pending[repo]["CHECK:"+relPath] = true
		} else {
			sw.pending[repo][relPath] = true
		}

		if t, ok := sw.debounce[repo]; ok {
			t.Stop()
		}
		r := repo
		sw.debounce[repo] = time.AfterFunc(200*time.Millisecond, func() {
			sw.flushPending(r)
		})
		return
	}
}

func (sw *SiloWatcher) flushPending(repo string) {
	sw.mu.Lock()
	pending := sw.pending[repo]
	sw.pending[repo] = nil
	capsule := sw.targets[repo]
	sw.mu.Unlock()

	if len(pending) == 0 || capsule == "" {
		return
	}

	capsuleDir := filepath.Join(sw.RepoDir(repo), capsule)
	siloDir := sw.SiloWorktree(repo)
	synced := 0

	for path := range pending {
		if strings.HasPrefix(path, "CHECK:") {
			// File was renamed/removed. Check if it still exists in the capsule.
			relPath := strings.TrimPrefix(path, "CHECK:")
			srcPath := filepath.Join(capsuleDir, relPath)
			if _, err := os.Stat(srcPath); err == nil {
				// File still exists (was replaced, not deleted). Sync it.
				if err := SyncFile(capsuleDir, siloDir, relPath); err != nil {
					sw.log.Printf("  sync error %s/%s: %v", repo, relPath, err)
				} else {
					synced++
				}
			} else {
				// File is actually gone. Remove from silo.
				RemoveSyncedFile(siloDir, relPath)
				synced++
			}
		} else {
			if err := SyncFile(capsuleDir, siloDir, path); err != nil {
				sw.log.Printf("  sync error %s/%s: %v", repo, path, err)
			} else {
				synced++
			}
		}
	}
	sw.log.Printf("  %s: synced %d file(s)", repo, synced)

	if synced > 0 {
		if sw.OnSync != nil {
			sw.OnSync(SyncEvent{
				Repo:      repo,
				Capsule:   capsule,
				FileCount: synced,
				Time:      time.Now(),
			})
		}
		sw.runChangeHook(repo, siloDir)
	}
}

// fullResync runs a complete FullSync for a repo, then re-establishes watches
// for any new directories. Called when a git operation (pull, checkout, etc.)
// is detected via .git/index changes.
func (sw *SiloWatcher) fullResync(repo string) {
	sw.mu.Lock()
	capsule := sw.targets[repo]
	// Clear any pending incremental syncs — the full sync covers everything.
	delete(sw.pending, repo)
	if t, ok := sw.debounce[repo]; ok {
		t.Stop()
		delete(sw.debounce, repo)
	}
	sw.mu.Unlock()

	if capsule == "" {
		return
	}

	capsuleDir := filepath.Join(sw.RepoDir(repo), capsule)
	siloDir := sw.SiloWorktree(repo)

	if err := FullSync(capsuleDir, siloDir); err != nil {
		sw.log.Printf("  full re-sync error for %s: %v", repo, err)
		return
	}

	// Re-walk to pick up any new directories
	filepath.Walk(capsuleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == ".next" {
				return filepath.SkipDir
			}
			sw.watcher.Add(path) // no-op if already watched
		}
		return nil
	})

	files, _ := GitLsFiles(capsuleDir)
	count := len(files)
	sw.log.Printf("  %s: full re-sync %d file(s)", repo, count)

	if count > 0 && sw.OnSync != nil {
		sw.OnSync(SyncEvent{
			Repo:      repo,
			Capsule:   capsule,
			FileCount: count,
			Time:      time.Now(),
		})
	}
	sw.runChangeHook(repo, siloDir)
}

// FullResyncAll triggers a full re-sync for every active silo target.
func (sw *SiloWatcher) FullResyncAll() {
	sw.mu.Lock()
	repos := make([]string, 0, len(sw.targets))
	for repo := range sw.targets {
		repos = append(repos, repo)
	}
	sw.mu.Unlock()

	for _, repo := range repos {
		sw.fullResync(repo)
	}
}

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

	for repo := range oldTargets {
		if _, ok := local.Silo[repo]; !ok {
			sw.log.Printf("  removing watch for %s", repo)
			sw.removeWatch(repo)
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
			if err := FullSync(capsuleDir, siloDir); err != nil {
				sw.log.Printf("  re-sync error for %s: %v", repo, err)
			}
			sw.runSwitchHooks(repo, siloDir)
			if err := sw.addWatch(repo, capsule); err != nil {
				sw.log.Printf("  warning: could not watch %s: %v", repo, err)
			}
		}
	}
}

func (sw *SiloWatcher) runSwitchHooks(repo, siloDir string) {
	if hook, ok := sw.AfterCreateHooks[repo]; ok {
		sw.log.Printf("  running after_create hook for %s", repo)
		RunHook(siloDir, hook, io.Discard, io.Discard)
	}
	repoConfigPath := filepath.Join(sw.MainWorktree(repo), "ws.repo.toml")
	if _, err := os.Stat(repoConfigPath); os.IsNotExist(err) {
		return
	}
	var cfg struct {
		Silo struct {
			AfterSwitch string `toml:"after_switch"`
		} `toml:"silo"`
	}
	if _, err := toml.DecodeFile(repoConfigPath, &cfg); err != nil {
		return
	}
	if cfg.Silo.AfterSwitch != "" {
		sw.log.Printf("  running after_switch hook for %s", repo)
		RunHook(siloDir, cfg.Silo.AfterSwitch, io.Discard, io.Discard)
	}
}

func (sw *SiloWatcher) runChangeHook(repo, siloDir string) {
	repoConfigPath := filepath.Join(sw.MainWorktree(repo), "ws.repo.toml")
	if _, err := os.Stat(repoConfigPath); os.IsNotExist(err) {
		return
	}
	var cfg struct {
		Silo struct {
			AfterChange string `toml:"after_change"`
		} `toml:"silo"`
	}
	if _, err := toml.DecodeFile(repoConfigPath, &cfg); err != nil {
		return
	}
	if cfg.Silo.AfterChange != "" {
		sw.log.Printf("  running after_change hook for %s", repo)
		RunHook(siloDir, cfg.Silo.AfterChange, io.Discard, io.Discard)
	}
}
