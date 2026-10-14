package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/owenrumney/ade/internal/port"
)

// handlePort moves this scratch's work into a repository and opens a ade on
// the worktree it landed in. port.Move is what refuses a non-scratch.
func (s *Server) handlePort(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
		Branch string `json:"branch"`
		Remove bool   `json:"remove"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		writeError(w, http.StatusBadRequest, "name the repository the work belongs in")
		return
	}

	root := s.space().homeRoot
	moved, err := port.Move(r.Context(), root, target, req.Branch)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	url, err := s.openIn(s.ctx, moved.Path, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError,
			"the work is at "+moved.Path+" but its ade did not start: "+err.Error())
		return
	}

	// Last, and only once the work is somewhere else and being served: this
	// server is running in the directory being deleted.
	removed := false
	if req.Remove {
		if err := port.Remove(r.Context(), root); err == nil {
			removed = true
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"url": url, "path": moved.Path, "branch": moved.Branch,
		"commits": moved.Commits, "committed": moved.Committed, "removed": removed,
	})
}
