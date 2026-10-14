package web

import (
	"net/http"
)

// handleOverlap says how the worktrees relate by the commits they hold.
// Cached per worktree, so a repeat with nothing moved is one rev-parse each.
func (s *Server) handleOverlap(w http.ResponseWriter, r *http.Request) {
	g, err := s.overlap.Build(r.Context(), s.space().homeRoot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, g)
}
