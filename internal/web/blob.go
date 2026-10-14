package web

import (
	"errors"
	"net/http"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// handleBlob is one file as one side of the current diff sees it. The editor
// renders its own red and green from the two, which no frame could give it.
func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	if ws.snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot, so there is no file on disk to read")
		return
	}
	side := r.URL.Query().Get("side")
	if side != gitdiff.SideLeft && side != gitdiff.SideRight {
		writeError(w, http.StatusBadRequest, "side is left or right")
		return
	}
	data, err := gitdiff.Blob(r.Context(), ws.diffRoot(), ws.opts, r.URL.Query().Get("path"), side)
	if errors.Is(err, gitdiff.ErrNotInDiff) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Plain text: the editor decides the language from the path it opened.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := w.Write(data); err != nil {
		return
	}
}
