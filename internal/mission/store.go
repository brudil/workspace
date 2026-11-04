package mission

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// ErrNotFound is returned when a mission slug doesn't exist.
var ErrNotFound = errors.New("mission not found")

// now is replaced in tests.
var now = func() time.Time { return time.Now().UTC().Truncate(time.Second) }

const (
	missionFile = "mission.toml"
	briefFile   = "brief.md"
	logFile     = "log.jsonl"
	lockFile    = ".lock"
)

// Store reads and writes missions for one workspace.
type Store struct {
	Root string // workspace root
}

// Dir is the directory holding every mission.
func (s Store) Dir() string { return filepath.Join(s.Root, ".ws", "missions") }

func (s Store) missionDir(slug string) string { return filepath.Join(s.Dir(), slug) }

// BriefPath is the path of a mission's brief.
func (s Store) BriefPath(slug string) string { return filepath.Join(s.missionDir(slug), briefFile) }

func (s Store) logPath(slug string) string { return filepath.Join(s.missionDir(slug), logFile) }

// Create makes a new mission with a templated brief and a "created" event.
// The slug is derived from the title and made unique.
func (s Store) Create(title string) (*Mission, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, errors.New("mission title is required")
	}
	if err := os.MkdirAll(s.Dir(), 0o755); err != nil {
		return nil, err
	}
	ensureExcluded(s.Root)

	base := Slugify(title)
	slug := base
	for i := 2; ; i++ {
		err := os.Mkdir(s.missionDir(slug), 0o755)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}

	m := &Mission{Slug: slug, Title: title, Created: now()}
	if err := s.write(m); err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.BriefPath(slug), []byte(briefTemplate(title)), 0o644); err != nil {
		return nil, err
	}
	if err := s.AppendLog(slug, Event{Kind: EventCreated, Text: title}); err != nil {
		return nil, err
	}
	return m, nil
}

func briefTemplate(title string) string {
	return "# " + title + "\n\n## Goal\n\n\n## Constraints\n\n\n## Acceptance\n\n"
}

// Load reads one mission.
func (s Store) Load(slug string) (*Mission, error) {
	if !validSlug(slug) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, slug)
	}
	data, err := os.ReadFile(filepath.Join(s.missionDir(slug), missionFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, slug)
	}
	if err != nil {
		return nil, err
	}
	var m Mission
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("mission %s: %w", slug, err)
	}
	m.Slug = slug
	return &m, nil
}

// List returns every mission, oldest first. Unreadable missions are skipped.
func (s Store) List() ([]*Mission, error) {
	entries, err := os.ReadDir(s.Dir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Mission
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := s.Load(e.Name())
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.Before(out[j].Created)
		}
		return out[i].Slug < out[j].Slug
	})
	return out, nil
}

// Update loads a mission, applies fn and saves it, holding the mission's lock
// so concurrent crew don't lose each other's writes. fn returning an error
// aborts without saving.
func (s Store) Update(slug string, fn func(*Mission) error) (*Mission, error) {
	unlock, err := s.lock(slug)
	if err != nil {
		return nil, err
	}
	defer unlock()

	m, err := s.Load(slug)
	if err != nil {
		return nil, err
	}
	if err := fn(m); err != nil {
		return nil, err
	}
	if err := s.write(m); err != nil {
		return nil, err
	}
	return m, nil
}

// FindByCapsule returns the mission owning repo/name, if any.
func (s Store) FindByCapsule(repo, name string) (*Mission, bool) {
	missions, err := s.List()
	if err != nil {
		return nil, false
	}
	for _, m := range missions {
		if m.HasCapsule(repo, name) {
			return m, true
		}
	}
	return nil, false
}

// ReadBrief returns a mission's brief, or "" if it has none.
func (s Store) ReadBrief(slug string) (string, error) {
	data, err := os.ReadFile(s.BriefPath(slug))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}

// write saves mission.toml atomically (temp file + rename).
func (s Store) write(m *Mission) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return err
	}
	dir := s.missionDir(m.Slug)
	tmp, err := os.CreateTemp(dir, ".mission-*.toml")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, missionFile))
}

const (
	lockWait  = 5 * time.Second
	lockStale = 30 * time.Second
)

// lock takes an exclusive lock file in the mission directory. A lock older
// than lockStale is assumed to belong to a crashed process and is broken.
func (s Store) lock(slug string) (func(), error) {
	if !validSlug(slug) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, slug)
	}
	if _, err := os.Stat(s.missionDir(slug)); err != nil {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, slug)
	}
	path := filepath.Join(s.missionDir(slug), lockFile)
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > lockStale {
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("mission %s is locked (remove %s if no ws command is running)", slug, path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

var (
	nonSlug   = regexp.MustCompile(`[^a-z0-9]+`)
	slugShape = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

const maxSlugLen = 40

// Slugify turns a title into a directory-safe slug.
func Slugify(title string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > maxSlugLen {
		cut := s[:maxSlugLen]
		// Prefer ending on a word boundary over cutting a word in half.
		if s[maxSlugLen] != '-' {
			if i := strings.LastIndexByte(cut, '-'); i > 0 {
				cut = cut[:i]
			}
		}
		s = strings.TrimRight(cut, "-")
	}
	if s == "" {
		s = "mission"
	}
	return s
}

func validSlug(slug string) bool { return slugShape.MatchString(slug) }

// ensureExcluded keeps .ws/ out of the config repo that the workspace root is
// a clone of, by adding it to that repo's info/exclude. Best effort: a root
// that isn't a git repo is left alone.
func ensureExcluded(root string) {
	if IsExcluded(root) {
		return
	}
	path, ok := excludePath(root)
	if !ok {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if data, _ := os.ReadFile(path); len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		f.WriteString("\n")
	}
	f.WriteString(excludeEntry + "\n")
}

const excludeEntry = "/.ws/"

// IsExcluded reports whether .ws/ is ignored by the workspace root's git repo.
// A root that isn't a git repo counts as excluded (nothing to leak into).
func IsExcluded(root string) bool {
	if _, ok := excludePath(root); !ok {
		return true
	}
	cmd := exec.Command("git", "-C", root, "check-ignore", "-q", ".ws/")
	return cmd.Run() == nil
}

func excludePath(root string) (string, bool) {
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return "", false
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return "", false
	}
	p := strings.TrimSpace(string(out))
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	return p, true
}

// EnsureExcluded is the exported form of ensureExcluded, for ws doctor --fix.
func EnsureExcluded(root string) { ensureExcluded(root) }

// StartCrew records new crew on a mission and logs it. IDs are c1, c2, … in
// order of launch, so they stay short enough to show in the sidebar.
func (s Store) StartCrew(slug, harness, conversation string) (Crew, error) {
	var crew Crew
	_, err := s.Update(slug, func(m *Mission) error {
		crew = Crew{
			ID:           fmt.Sprintf("c%d", len(m.Crew)+1),
			Harness:      harness,
			Conversation: conversation,
			Started:      now(),
		}
		m.Crew = append(m.Crew, crew)
		return nil
	})
	if err != nil {
		return Crew{}, err
	}
	text := harness
	if conversation != "" {
		text += " " + conversation
	}
	return crew, s.AppendLog(slug, Event{Kind: EventCrewStarted, Crew: crew.ID, Text: text})
}

// EndCrew records that crew stopped, with a short exit summary.
func (s Store) EndCrew(slug, id, exit string) error {
	found := false
	if _, err := s.Update(slug, func(m *Mission) error {
		for i := range m.Crew {
			if m.Crew[i].ID == id {
				t := now()
				m.Crew[i].Ended = &t
				m.Crew[i].Exit = exit
				found = true
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("mission %s has no crew %s", slug, id)
	}
	return s.AppendLog(slug, Event{Kind: EventCrewEnded, Crew: id, Text: exit})
}

// LastResumable returns the most recent crew of the given harness that has a
// conversation id, if any.
func (m *Mission) LastResumable(harness string) (Crew, bool) {
	for i := len(m.Crew) - 1; i >= 0; i-- {
		if c := m.Crew[i]; c.Harness == harness && c.Conversation != "" {
			return c, true
		}
	}
	return Crew{}, false
}

// SetCrewPID records the process id of running crew.
func (s Store) SetCrewPID(slug, id string, pid int) error {
	_, err := s.Update(slug, func(m *Mission) error {
		for i := range m.Crew {
			if m.Crew[i].ID == id {
				m.Crew[i].PID = pid
			}
		}
		return nil
	})
	return err
}

// ReconcileCrew closes crew records left open by a deck that died without
// recording their end: any open crew whose process isn't alive is ended as
// "lost". It returns how many were closed.
func (s Store) ReconcileCrew(alive func(pid int) bool) int {
	missions, err := s.List()
	if err != nil {
		return 0
	}
	closed := 0
	for _, m := range missions {
		for _, c := range m.Crew {
			if c.Ended != nil || (c.PID != 0 && alive(c.PID)) {
				continue
			}
			if s.EndCrew(m.Slug, c.ID, "lost") == nil {
				closed++
			}
		}
	}
	return closed
}
