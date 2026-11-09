package web

import (
	"errors"
	"net/http"
)

// mission is the mission's page: everything the deck's mission view shows.
func (s *Server) mission(w http.ResponseWriter, r *http.Request) {
	if s.Mission == nil {
		jsonError(w, http.StatusNotFound, "missions aren't available")
		return
	}
	d, err := s.Mission(r.PathValue("slug"), r.URL.Query().Get("github") == "1")
	switch {
	case errors.Is(err, ErrNotFound):
		jsonError(w, http.StatusNotFound, "no mission "+r.PathValue("slug"))
	case err != nil:
		jsonError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, d)
	}
}

// artifact is a readable artifact's text, by its reference: only the
// mission's own artifacts, so this can't be pointed at any file.
func (s *Server) artifact(w http.ResponseWriter, r *http.Request) {
	ref := r.URL.Query().Get("ref")
	if s.Artifact == nil || ref == "" {
		jsonError(w, http.StatusBadRequest, "need an artifact's ref")
		return
	}
	a, err := s.Artifact(r.PathValue("slug"), ref)
	switch {
	case errors.Is(err, ErrNotFound):
		jsonError(w, http.StatusNotFound, err.Error())
	case err != nil:
		jsonError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeJSON(w, http.StatusOK, a)
	}
}
