package web

import (
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
