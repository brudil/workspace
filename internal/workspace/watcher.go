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

// SiloWatcher manages file watchers for all active silos.
type SiloWatcher struct {
	Root             string
	RepoDir          func(name string) string
	SiloWorktree     func(name string) string
	MainWorktree     func(name string) string
	AfterCreateHooks map[string]string
	DefaultBranch    string

	watcher  *fsnotify.Watcher
	targets  map[string]string // repo -> capsule
	mu       sync.Mutex
	log      *log.Logger
	debounce map[string]*time.Timer     // repo -> debounce timer
	pending  map[string]map[string]bool // repo -> set of changed relative paths
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
			if name == ".git" || name == "node_modules" || name == ".next" {
				return filepath.SkipDir
			}
			return sw.watcher.Add(path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sw.mu.Lock()
	sw.targets[repo] = capsule
	sw.mu.Unlock()
	sw.log.Printf("  watching %s -> %s", repo, capsule)
	return nil
}

func (sw *SiloWatcher) removeWatch(repo string) {
	sw.mu.Lock()
	capsule, ok := sw.targets[repo]
	if ok {
		delete(sw.targets, repo)
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

	for repo, capsule := range sw.targets {
		capsuleDir := filepath.Join(sw.RepoDir(repo), capsule)
		if strings.HasPrefix(event.Name, capsuleDir+string(os.PathSeparator)) {
			relPath, _ := filepath.Rel(capsuleDir, event.Name)
			if strings.HasPrefix(relPath, ".git"+string(os.PathSeparator)) || relPath == ".git" {
				return
			}
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
				sw.pending[repo]["DELETE:"+relPath] = true
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
		if strings.HasPrefix(path, "DELETE:") {
			relPath := strings.TrimPrefix(path, "DELETE:")
			RemoveSyncedFile(siloDir, relPath)
			synced++
		} else {
			if err := SyncFile(capsuleDir, siloDir, path); err != nil {
				sw.log.Printf("  sync error %s/%s: %v", repo, path, err)
			} else {
				synced++
			}
		}
	}
	sw.log.Printf("  %s: synced %d file(s)", repo, synced)

	// Run after_change hook
	if synced > 0 {
		sw.runChangeHook(repo, siloDir)
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
