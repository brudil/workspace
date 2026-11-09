package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/brudil/workspace/internal/review"
)

// ErrNotFound is Capsule's answer for a capsule that isn't the mission's.
var ErrNotFound = errors.New("not found")

// fullSHA is how the API names commits: the client only ever sends back
// what a diff gave it.
var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// capsuleDir resolves the request's mission capsule, or answers for it.
func (s *Server) capsuleDir(w http.ResponseWriter, r *http.Request) (dir, baseRef string, ok bool) {
	if s.Capsule == nil {
		jsonError(w, http.StatusNotFound, "review isn't available")
		return "", "", false
	}
	slug, repo, name := r.PathValue("slug"), r.PathValue("repo"), r.PathValue("name")
	dir, baseRef, err := s.Capsule(slug, repo, name)
	switch {
	case errors.Is(err, ErrNotFound):
		jsonError(w, http.StatusNotFound, "mission "+slug+" has no capsule "+repo+"/"+name)
		return "", "", false
	case err != nil:
		jsonError(w, http.StatusInternalServerError, err.Error())
		return "", "", false
	}
	return dir, baseRef, true
}

// diff is the capsule's committed changes against its base.
func (s *Server) diff(w http.ResponseWriter, r *http.Request) {
	dir, baseRef, ok := s.capsuleDir(w, r)
	if !ok {
		return
	}
	d, err := review.Compare(dir, baseRef)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// fileDiff is one file's changes in full, for a file too large to come
// with the rest.
func (s *Server) fileDiff(w http.ResponseWriter, r *http.Request) {
	dir, _, ok := s.capsuleDir(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	base, head, path := q.Get("base"), q.Get("head"), q.Get("path")
	if !fullSHA.MatchString(base) || !fullSHA.MatchString(head) || !validPath(path) {
		jsonError(w, http.StatusBadRequest, "need base and head (full commit ids) and a path")
		return
	}
	f, err := review.FileDiff(dir, base, head, path)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// blob is a file's lines at a commit, for showing more context.
func (s *Server) blob(w http.ResponseWriter, r *http.Request) {
	dir, _, ok := s.capsuleDir(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	rev, path := q.Get("rev"), q.Get("path")
	if !fullSHA.MatchString(rev) || !validPath(path) {
		jsonError(w, http.StatusBadRequest, "need rev (a full commit id) and a path")
		return
	}
	lines, err := review.Blob(dir, rev, path)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

// validPath is a path in a repo, as git names it: relative, not an option.
func validPath(p string) bool {
	return p != "" && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "-") && !strings.ContainsRune(p, 0)
}

// The review being written, and those submitted: GET .../review and the
// writes under it. Reviews are the mission's, kept by internal/review.

// ReviewState is GET .../review: the capsule's draft (null if none), its
// submitted reviews (newest first), and which files you've viewed.
type ReviewState struct {
	Draft   *review.Review    `json:"draft"`
	Reviews []*review.Review  `json:"reviews"`
	Viewed  map[string]string `json:"viewed"`
}

// reviewing resolves the request's capsule and its mission's reviews.
func (s *Server) reviewing(w http.ResponseWriter, r *http.Request) (store review.Store, capsule, dir, baseRef string, ok bool) {
	dir, baseRef, ok = s.capsuleDir(w, r)
	if !ok {
		return
	}
	return review.For(s.Root, r.PathValue("slug")), r.PathValue("repo") + "/" + r.PathValue("name"), dir, baseRef, true
}

func (s *Server) reviewState(store review.Store, capsule string) (*ReviewState, error) {
	draft, err := store.Draft(capsule)
	if err != nil {
		return nil, err
	}
	all, err := store.List()
	if err != nil {
		return nil, err
	}
	st := &ReviewState{Draft: draft, Reviews: []*review.Review{}}
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Capsule == capsule && all[i].State == review.Submitted {
			st.Reviews = append(st.Reviews, all[i])
		}
	}
	if st.Viewed, err = store.Viewed(capsule); err != nil {
		return nil, err
	}
	return st, nil
}

// answerReview replies with the review state after a write, or the write's
// error: a missing draft or comment is 404, anything else it refused 422.
func (s *Server) answerReview(w http.ResponseWriter, store review.Store, capsule string, err error) {
	switch {
	case errors.Is(err, review.ErrNotFound):
		jsonError(w, http.StatusNotFound, err.Error())
		return
	case err != nil:
		jsonError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	st, err := s.reviewState(store, capsule)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) getReview(w http.ResponseWriter, r *http.Request) {
	store, capsule, _, _, ok := s.reviewing(w, r)
	if ok {
		s.answerReview(w, store, capsule, nil)
	}
}

func (s *Server) discardReview(w http.ResponseWriter, r *http.Request) {
	store, capsule, _, _, ok := s.reviewing(w, r)
	if ok {
		s.answerReview(w, store, capsule, store.Discard(capsule))
	}
}

// readBody decodes a write's JSON body.
func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		jsonError(w, http.StatusBadRequest, "bad request body: "+err.Error())
		return false
	}
	return true
}

// diffed is the diff a write was made on, as the page sends it back: full
// commit ids it was given.
func diffed(w http.ResponseWriter, dir, baseRef, base, head string) (review.Diffed, bool) {
	if !fullSHA.MatchString(base) || !fullSHA.MatchString(head) {
		jsonError(w, http.StatusBadRequest, "need base and head: the full commit ids of the diff")
		return review.Diffed{}, false
	}
	return review.Diffed{Dir: dir, BaseRef: baseRef, Base: base, Head: head}, true
}

type noteBody struct {
	Base       string  `json:"base"`
	Head       string  `json:"head"`
	Path       string  `json:"path"`
	Side       string  `json:"side"`
	Line       int     `json:"line"`
	EndLine    int     `json:"endLine"`
	Kind       string  `json:"kind"`
	Body       string  `json:"body"`
	Suggestion *string `json:"suggestion"`
}

func (s *Server) addNote(w http.ResponseWriter, r *http.Request) {
	store, capsule, dir, baseRef, ok := s.reviewing(w, r)
	var b noteBody
	if !ok || !readBody(w, r, &b) {
		return
	}
	d, ok := diffed(w, dir, baseRef, b.Base, b.Head)
	if !ok {
		return
	}
	if !validPath(b.Path) {
		jsonError(w, http.StatusBadRequest, "need a path")
		return
	}
	_, err := store.AddNote(capsule, d, review.NewNote{Path: b.Path, Side: b.Side, Line: b.Line, EndLine: b.EndLine, Kind: b.Kind, Body: b.Body, Suggestion: b.Suggestion}, s.now())
	s.answerReview(w, store, capsule, err)
}

func (s *Server) editNote(w http.ResponseWriter, r *http.Request) {
	store, capsule, _, _, ok := s.reviewing(w, r)
	var b struct {
		Kind       string  `json:"kind"`
		Body       string  `json:"body"`
		Suggestion *string `json:"suggestion"`
	}
	if !ok || !readBody(w, r, &b) {
		return
	}
	_, err := store.EditNote(capsule, r.PathValue("id"), b.Kind, b.Body, b.Suggestion)
	s.answerReview(w, store, capsule, err)
}

func (s *Server) deleteNote(w http.ResponseWriter, r *http.Request) {
	store, capsule, _, _, ok := s.reviewing(w, r)
	if ok {
		_, err := store.DeleteNote(capsule, r.PathValue("id"))
		s.answerReview(w, store, capsule, err)
	}
}

func (s *Server) setViewed(w http.ResponseWriter, r *http.Request) {
	store, capsule, _, _, ok := s.reviewing(w, r)
	var b struct {
		Path   string `json:"path"`
		Blob   string `json:"blob"`
		Viewed bool   `json:"viewed"`
	}
	if !ok || !readBody(w, r, &b) {
		return
	}
	if !validPath(b.Path) {
		jsonError(w, http.StatusBadRequest, "need a path")
		return
	}
	_, err := store.SetViewed(capsule, b.Path, b.Blob, b.Viewed)
	s.answerReview(w, store, capsule, err)
}

func (s *Server) submitReview(w http.ResponseWriter, r *http.Request) {
	store, capsule, dir, baseRef, ok := s.reviewing(w, r)
	var b struct {
		Base    string `json:"base"`
		Head    string `json:"head"`
		Verdict string `json:"verdict"`
		Summary string `json:"summary"`
	}
	if !ok || !readBody(w, r, &b) {
		return
	}
	d, ok := diffed(w, dir, baseRef, b.Base, b.Head)
	if !ok {
		return
	}
	rv, err := store.Submit(capsule, d, b.Verdict, b.Summary, s.now())
	if err == nil && s.OnReview != nil {
		s.OnReview(r.PathValue("slug"), rv)
	}
	s.answerReview(w, store, capsule, err)
}
