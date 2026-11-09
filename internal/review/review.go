package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brudil/workspace/internal/mission"
)

// A review is one pass over a mission capsule's changes: comments anchored
// to lines, then a verdict. It's a draft (yours alone) until submitted;
// then crew read it (ws review show) and the orchestrator decides what
// follows. Reviews live with their mission, in reviews/<id>.json; a
// submitted one is also rendered for agents as mission:reviews/<id>.md.

// Review states, verdicts and comment kinds.
const (
	Draft     = "draft"
	Submitted = "submitted"

	Approve = "approve"
	Changes = "changes"
	Comment = "comment"
)

// Kinds a comment can be, in the order they matter.
var Kinds = []string{"blocking", "suggestion", "question", "nit"}

// Verdicts a review can end with.
var Verdicts = []string{Approve, Changes, Comment}

// Review is one review of a capsule.
type Review struct {
	ID        string     `json:"id"`      // r1, r2, … per mission
	Capsule   string     `json:"capsule"` // repo/name
	BaseRef   string     `json:"baseRef"`
	Base      string     `json:"base"`
	Head      string     `json:"head"` // the commit reviewed
	State     string     `json:"state"`
	Verdict   string     `json:"verdict,omitempty"`
	Summary   string     `json:"summary,omitempty"`
	Created   time.Time  `json:"created"`
	Submitted *time.Time `json:"submitted,omitempty"`
	Comments  []Note     `json:"comments"`
}

// Note is a comment on lines of the diff. (Comment is the verdict.)
type Note struct {
	ID   string `json:"id"` // r3.c1
	Path string `json:"path"`
	// Side is "new" for lines as they are at the reviewed commit, "old" for
	// lines as they were (deleted ones).
	Side    string `json:"side"`
	Line    int    `json:"line"`
	EndLine int    `json:"endLine"`
	Commit  string `json:"commit"` // the commit Side's lines are from
	// Excerpt is the lines as they were when commented on, so the comment
	// reads the same however the code moves after.
	Excerpt []string `json:"excerpt"`
	Kind    string   `json:"kind"`
	Body    string   `json:"body"`
	// Suggestion replaces the lines, when set ("" suggests deleting them).
	Suggestion *string `json:"suggestion,omitempty"`
	Status     string  `json:"status"` // open (more in phase 4)
	Thread     []Reply `json:"thread"`
}

// Reply is a message in a comment's thread.
type Reply struct {
	By     string    `json:"by"`
	At     time.Time `json:"at"`
	Body   string    `json:"body"`
	Commit string    `json:"commit,omitempty"`
	Status string    `json:"status,omitempty"`
}

// Counts says how many comments of each kind a review has.
func (r *Review) Counts() map[string]int {
	c := map[string]int{}
	for _, n := range r.Comments {
		c[n.Kind]++
	}
	return c
}

// Store keeps one mission's reviews.
type Store struct {
	Missions mission.Store
	Slug     string
}

// For is the review store of a mission.
func For(root, slug string) Store { return Store{Missions: mission.Store{Root: root}, Slug: slug} }

func (s Store) dir() string { return filepath.Join(s.Missions.Dir(), s.Slug, "reviews") }

func (s Store) path(id string) string { return filepath.Join(s.dir(), id+".json") }

// RenderedRef is where a submitted review is kept for agents, as an
// artifact reference.
func RenderedRef(id string) string { return "mission:reviews/" + id + ".md" }

func (s Store) renderedPath(id string) string {
	return filepath.Join(s.Missions.FilesDir(s.Slug), "reviews", id+".md")
}

var idShape = regexp.MustCompile(`^r([1-9][0-9]*)$`)

// ErrNotFound is the answer for a review, draft or comment that isn't there.
var ErrNotFound = errors.New("not found")

// Load reads one review.
func (s Store) Load(id string) (*Review, error) {
	if !idShape.MatchString(id) {
		return nil, fmt.Errorf("%w: no review %q (they're named r1, r2, …)", ErrNotFound, id)
	}
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: mission %s has no review %s", ErrNotFound, s.Slug, id)
	}
	if err != nil {
		return nil, err
	}
	var r Review
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("review %s: %w", id, err)
	}
	return &r, nil
}

// List reads the mission's reviews, oldest first.
func (s Store) List() ([]*Review, error) {
	entries, err := os.ReadDir(s.dir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Review
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !idShape.MatchString(id) {
			continue
		}
		if r, err := s.Load(id); err == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return num(out[i].ID) < num(out[j].ID) })
	return out, nil
}

func num(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "r"))
	return n
}

// Draft is the capsule's draft review, or nil.
func (s Store) Draft(capsule string) (*Review, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, r := range all {
		if r.Capsule == capsule && r.State == Draft {
			return r, nil
		}
	}
	return nil, nil
}

// Latest is the most recently submitted review, of capsule if it's given,
// or nil.
func (s Store) Latest(capsule string) (*Review, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var latest *Review
	for _, r := range all {
		if r.State != Submitted || (capsule != "" && r.Capsule != capsule) {
			continue
		}
		if latest == nil || r.Submitted.After(*latest.Submitted) {
			latest = r
		}
	}
	return latest, nil
}

func (s Store) save(r *Review) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(s.path(r.ID), append(data, '\n'))
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp.Name(), 0o644)
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		os.Remove(tmp.Name())
	}
	return werr
}

const (
	lockWait  = 5 * time.Second
	lockStale = 30 * time.Second
)

// locked runs fn holding the mission's review lock: the web UI and crew's
// ws review commands write the same files.
func (s Store) locked(fn func() error) error {
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return err
	}
	path := filepath.Join(s.dir(), ".lock")
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > lockStale {
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("reviews of %s are locked (remove %s if nothing is writing one)", s.Slug, path)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer os.Remove(path)
	return fn()
}

// NewNote is a comment to add to a capsule's draft.
type NewNote struct {
	Path       string
	Side       string
	Line       int
	EndLine    int
	Kind       string
	Body       string
	Suggestion *string
}

// Diffed is the diff the comment was made on: what a new draft is of, and
// what the comment's lines are read from.
type Diffed struct {
	Dir, BaseRef, Base, Head string
}

func validKind(k string) bool {
	for _, v := range Kinds {
		if k == v {
			return true
		}
	}
	return false
}

// AddNote adds a comment to the capsule's draft, starting one if there's
// none. Its excerpt is read from git: the new side at d.Head, the old at
// d.Base.
func (s Store) AddNote(capsule string, d Diffed, n NewNote, at time.Time) (*Review, error) {
	if n.Side != "new" && n.Side != "old" {
		return nil, fmt.Errorf("side is new or old, not %q", n.Side)
	}
	if n.EndLine == 0 {
		n.EndLine = n.Line
	}
	if n.Line < 1 || n.EndLine < n.Line {
		return nil, fmt.Errorf("lines %d–%d aren't a range", n.Line, n.EndLine)
	}
	if !validKind(n.Kind) {
		return nil, fmt.Errorf("a comment is %s, not %q", strings.Join(Kinds, ", "), n.Kind)
	}
	if strings.TrimSpace(n.Body) == "" && n.Suggestion == nil {
		return nil, errors.New("say something, or suggest a change")
	}
	commit := d.Head
	if n.Side == "old" {
		commit = d.Base
	}
	lines, err := Blob(d.Dir, commit, n.Path)
	if err != nil {
		return nil, err
	}
	if n.EndLine > len(lines) {
		return nil, fmt.Errorf("%s has %d lines at %s, not %d", n.Path, len(lines), short(commit), n.EndLine)
	}
	var r *Review
	err = s.locked(func() error {
		if r, err = s.Draft(capsule); err != nil {
			return err
		}
		if r == nil {
			if r, err = s.newDraft(capsule, d, at); err != nil {
				return err
			}
		}
		r.Base, r.Head, r.BaseRef = d.Base, d.Head, d.BaseRef
		next := 1
		for _, c := range r.Comments {
			if k, err := strconv.Atoi(strings.TrimPrefix(c.ID, r.ID+".c")); err == nil && k >= next {
				next = k + 1
			}
		}
		r.Comments = append(r.Comments, Note{
			ID: fmt.Sprintf("%s.c%d", r.ID, next), Path: n.Path, Side: n.Side, Line: n.Line, EndLine: n.EndLine,
			Commit: commit, Excerpt: lines[n.Line-1 : n.EndLine], Kind: n.Kind, Body: strings.TrimSpace(n.Body),
			Suggestion: n.Suggestion, Status: "open", Thread: []Reply{},
		})
		return s.save(r)
	})
	return r, err
}

func (s Store) newDraft(capsule string, d Diffed, at time.Time) (*Review, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	next := 1
	for _, r := range all {
		if n := num(r.ID); n >= next {
			next = n + 1
		}
	}
	return &Review{
		ID: "r" + strconv.Itoa(next), Capsule: capsule, BaseRef: d.BaseRef, Base: d.Base, Head: d.Head,
		State: Draft, Created: at, Comments: []Note{},
	}, nil
}

// draftNote finds a comment in the capsule's draft, to change it.
func (s Store) draftNote(capsule, id string, fn func(r *Review, i int) error) (*Review, error) {
	var r *Review
	err := s.locked(func() error {
		var err error
		if r, err = s.Draft(capsule); err != nil {
			return err
		}
		if r == nil {
			return fmt.Errorf("%w: %s has no draft review", ErrNotFound, capsule)
		}
		for i := range r.Comments {
			if r.Comments[i].ID == id {
				if err := fn(r, i); err != nil {
					return err
				}
				return s.save(r)
			}
		}
		return fmt.Errorf("%w: the draft has no comment %s", ErrNotFound, id)
	})
	return r, err
}

// EditNote changes a draft comment's kind, words and suggestion.
func (s Store) EditNote(capsule, id, kind, body string, suggestion *string) (*Review, error) {
	if !validKind(kind) {
		return nil, fmt.Errorf("a comment is %s, not %q", strings.Join(Kinds, ", "), kind)
	}
	if strings.TrimSpace(body) == "" && suggestion == nil {
		return nil, errors.New("say something, or suggest a change")
	}
	return s.draftNote(capsule, id, func(r *Review, i int) error {
		r.Comments[i].Kind, r.Comments[i].Body, r.Comments[i].Suggestion = kind, strings.TrimSpace(body), suggestion
		return nil
	})
}

// DeleteNote removes a draft comment.
func (s Store) DeleteNote(capsule, id string) (*Review, error) {
	return s.draftNote(capsule, id, func(r *Review, i int) error {
		r.Comments = append(r.Comments[:i], r.Comments[i+1:]...)
		return nil
	})
}

// Discard throws the capsule's draft away.
func (s Store) Discard(capsule string) error {
	return s.locked(func() error {
		r, err := s.Draft(capsule)
		if err != nil || r == nil {
			return err
		}
		return os.Remove(s.path(r.ID))
	})
}

// Submit ends the capsule's draft with a verdict, at the commit reviewed:
// it's kept, rendered for agents as an artifact, and logged. An approval
// needs no comments; anything else does, or a summary. A capsule with no
// draft can be approved: the review is made then.
func (s Store) Submit(capsule string, d Diffed, verdict, summary string, at time.Time) (*Review, error) {
	ok := false
	for _, v := range Verdicts {
		ok = ok || v == verdict
	}
	if !ok {
		return nil, fmt.Errorf("a review ends with %s, not %q", strings.Join(Verdicts, ", "), verdict)
	}
	var r *Review
	err := s.locked(func() error {
		var err error
		if r, err = s.Draft(capsule); err != nil {
			return err
		}
		if r == nil {
			if r, err = s.newDraft(capsule, d, at); err != nil {
				return err
			}
		}
		if verdict != Approve && len(r.Comments) == 0 && strings.TrimSpace(summary) == "" {
			return errors.New("say what needs changing: comment on the lines, or write a summary")
		}
		r.Base, r.Head, r.BaseRef = d.Base, d.Head, d.BaseRef
		r.State, r.Verdict, r.Summary, r.Submitted = Submitted, verdict, strings.TrimSpace(summary), &at
		if err := s.save(r); err != nil {
			return err
		}
		return writeFile(s.renderedPath(r.ID), []byte(Render(r)))
	})
	if err != nil {
		return nil, err
	}
	// The artifact is recorded without its own log line: the review's says it.
	if _, err := s.Missions.Update(s.Slug, func(m *mission.Mission) error {
		m.Artifacts = append(m.Artifacts, mission.Artifact{Ref: RenderedRef(r.ID), Kind: "review", Label: Title(r), Added: at})
		return nil
	}); err != nil {
		return r, err
	}
	repo, name, _ := strings.Cut(capsule, "/")
	return r, s.Missions.AppendLog(s.Slug, mission.Event{Time: at, Kind: mission.EventReview, Repo: repo, Capsule: name, Text: r.ID + ": " + verdictWords[r.Verdict] + countWords(r)})
}

var verdictWords = map[string]string{Approve: "approved", Changes: "changes requested", Comment: "comments"}

// Title is a review's one-line name.
func Title(r *Review) string {
	return fmt.Sprintf("Review %s of %s: %s", r.ID, r.Capsule, verdictWords[r.Verdict])
}

// countWords is " (2 blocking, 1 nit)", or "" for no comments.
func countWords(r *Review) string {
	c := r.Counts()
	var parts []string
	for _, k := range Kinds {
		if c[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c[k], k))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// Viewed is which files of a capsule you've marked viewed, by their blob
// at the time: a file that changes again isn't viewed any more.
func (s Store) Viewed(capsule string) (map[string]string, error) {
	all, err := s.allViewed()
	if err != nil {
		return nil, err
	}
	if v := all[capsule]; v != nil {
		return v, nil
	}
	return map[string]string{}, nil
}

func (s Store) viewedPath() string { return filepath.Join(s.dir(), "viewed.json") }

func (s Store) allViewed() (map[string]map[string]string, error) {
	all := map[string]map[string]string{}
	data, err := os.ReadFile(s.viewedPath())
	if errors.Is(err, os.ErrNotExist) {
		return all, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("%s: %w", s.viewedPath(), err)
	}
	return all, nil
}

// SetViewed marks a file of the capsule viewed at blob, or not.
func (s Store) SetViewed(capsule, path, blob string, viewed bool) (map[string]string, error) {
	var out map[string]string
	err := s.locked(func() error {
		all, err := s.allViewed()
		if err != nil {
			return err
		}
		if all[capsule] == nil {
			all[capsule] = map[string]string{}
		}
		if viewed {
			all[capsule][path] = blob
		} else {
			delete(all[capsule], path)
		}
		out = all[capsule]
		data, err := json.MarshalIndent(all, "", "  ")
		if err != nil {
			return err
		}
		return writeFile(s.viewedPath(), append(data, '\n'))
	})
	return out, err
}
