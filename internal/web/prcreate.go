package web

import (
	"net/http"

	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
)

// prCreateSkill is the slash command that opens the draft and writes it up.
const prCreateSkill = "/pr-create"

// handleCreatePR opens a draft pull request for the branch in the window. It
// pushes first: gh has nothing to open one from until the branch is on origin.
func (s *Server) handleCreatePR(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	if ws.snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot, so there is no branch to open one from")
		return
	}
	if ws.pr != nil {
		writeError(w, http.StatusBadRequest, "this checkout is already showing a pull request")
		return
	}
	var req struct {
		// A ready-for-review one is asked for; a draft is what you get otherwise.
		Ready bool `json:"ready"`
	}
	if r.Body != nil && r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}

	if _, err := gitdiff.Push(r.Context(), ws.diffRoot()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The agent's own skill writes the description; gh --fill is the fallback
	// for an agent that cannot take a line right now.
	if !req.Ready {
		if _, err := s.agentPane(r.Context(), prCreateSkill); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"asked": true})
			return
		}
	}
	made, err := ghpr.Create(r.Context(), ws.homeRoot, !req.Ready)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	s.refreshWorktree(s.ctx)
	s.board.RefreshReviews(s.ctx)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"number": made.Number, "url": made.URL})
}
