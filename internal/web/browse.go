package web

import (
	"errors"
	"net/http"

	"github.com/owenrumney/ade/internal/browse"
)

// handleBrowse lists the directories under one directory, for picking a path
// to mount.
//
// The server is the user's own, on loopback and behind guard, and the paths it
// lists are ones they could type into the box by hand anyway.
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	view, err := browse.List(r.URL.Query().Get("path"))
	if errors.Is(err, browse.ErrRelative) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}
