package web

import (
	"errors"
	"net/http"

	"github.com/owenrumney/ade/internal/dash"
)

// handleDismissNotification puts a notification aside as it stands, or brings
// it back.
func (s *Server) handleDismissNotification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID        string `json:"id"`
		Dismissed bool   `json:"dismissed"`
	}
	if !decode(w, r, &req) {
		return
	}
	switch err := s.board.Dismiss(req.ID, req.Dismissed); {
	case errors.Is(err, dash.ErrNotInInbox):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
