package web

import (
	"errors"
	"net/http"
	"os"

	"github.com/owenrumney/ade/internal/runlog"
)

// briefPath is the brief for this work: the newest file under .git/ade/docs.
func (s *Server) briefPath() string {
	return runlog.Brief(s.space().gitDir, s.commonGitDir)
}

func (s *Server) handleBrief(w http.ResponseWriter, _ *http.Request) {
	path := s.briefPath()
	if path == "" {
		writeError(w, http.StatusNotFound, "this checkout has no brief under .git/ade/docs")
		return
	}
	body, err := os.ReadFile(path) // #nosec G304 -- found by walking the checkout's own git dir
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path, "body": string(body)})
}

// handleSendBrief types one line at the agent's prompt naming the brief.
func (s *Server) handleSendBrief(w http.ResponseWriter, r *http.Request) {
	path := s.briefPath()
	if path == "" {
		writeError(w, http.StatusNotFound, "this checkout has no brief under .git/ade/docs")
		return
	}
	if _, err := s.agentPane(r.Context(), briefAsk(path)); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errMidTurn) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

func briefAsk(path string) string {
	return "Read " + path + ": it is the brief for this work. Say where it leaves off."
}
