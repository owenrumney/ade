package web

import (
	"net/http"

	"github.com/owenrumney/ade/internal/sandbox"
)

// handleSandboxOn moves the session already running into a container, or out
// of one. The container is dropped; the conversation is not.
func (s *Server) handleSandboxOn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On bool `json:"on"`
	}
	if !decode(w, r, &req) {
		return
	}

	if why := sandboxFixed(); why != "" && !req.On {
		writeError(w, http.StatusConflict, why)
		return
	}
	state := s.agent.State()
	if state.Sandbox == req.On {
		s.writeSandbox(w)
		return
	}
	// Flipping it kills the subprocess, which would take a turn in flight
	// with it. Refused rather than queued: this is a decision, not a message.
	if state.Running {
		writeError(w, http.StatusConflict, "the agent is running; wait for it or cancel it first")
		return
	}
	if req.On {
		if err := s.agent.ensureRuntime(r.Context()); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	s.agent.setSandbox(req.On)
	// A container going out from under a session is otherwise invisible: the
	// note in ensure that announces the new one waits for the next turn.
	if req.On {
		s.agent.step("the sandbox is on; the conversation resumes in a container on the next turn")
	} else {
		s.agent.step("the sandbox is off; the conversation resumes on the host on the next turn")
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	s.writeSandbox(w)
}

func (s *Server) writeSandbox(w http.ResponseWriter) {
	ws, err := sandbox.LoadWorkspace(s.agent.workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.sandboxView(ws))
}
