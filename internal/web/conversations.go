package web

import (
	"net/http"

	"github.com/owenrumney/ade/internal/claudelog"
)

// handleContinue moves this checkout's agent onto another conversation claude holds for it.
func (s *Server) handleContinue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if s.agent.isCodex() {
		writeError(w, http.StatusBadRequest, "this session runs codex; only claude's conversations can be continued")
		return
	}
	if !claudelog.IsID(req.ID) || !claudelog.Held(s.agent.dir, req.ID) {
		writeError(w, http.StatusBadRequest, "that is not one of this checkout's conversations")
		return
	}
	mine := req.ID == s.agent.Current()
	// Two claudes on one transcript write over each other.
	if claudelog.Open()[req.ID] && (!mine || !s.agentAlive()) {
		writeError(w, http.StatusConflict, "that conversation is open in another terminal; quit claude there first")
		return
	}
	if !mine {
		// An agent nobody has typed at holds nothing: it gives way.
		if s.agentAlive() && !spokenIn(s.agent.dir, "claude", s.agent.Current()) {
			s.dropPane(paneKeyFor(takeoverPane))
			s.agent.endTakeover()
		}
		if !s.yieldAgentPane(w) {
			return
		}
		s.agent.note(req.ID)
		s.badgeChanged()
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": req.ID})
}
