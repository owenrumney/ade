package web

import (
	"errors"
	"net/http"

	"github.com/owenrumney/ade/internal/dash"
)

// handleMuteReview puts a pull request aside, or brings it back.
func (s *Server) handleMuteReview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo   string `json:"repo"`
		Number int    `json:"number"`
		Muted  bool   `json:"muted"`
	}
	if !decode(w, r, &req) {
		return
	}
	switch err := s.board.Mute(req.Repo, req.Number, req.Muted); {
	case errors.Is(err, dash.ErrNotOffered):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleOpenReview starts a session for a requested review: the pull request
// checked out in a worktree of the repository, under the ade serving that
// worktree, whose link comes back.
func (s *Server) handleOpenReview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo   string `json:"repo"`
		Number int    `json:"number"`
	}
	if !decode(w, r, &req) {
		return
	}
	url, path, err := s.board.OpenReview(r.Context(), req.Repo, req.Number)
	switch {
	case errors.Is(err, dash.ErrGone):
		writeError(w, http.StatusNotFound, err.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]any{"url": url, "path": path})
	}
}
