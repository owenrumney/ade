package web

import (
	"errors"
	"net/http"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/fixtures"
)

// handleSaveFixture captures one of your turns as a prompt fixture: its
// prompt, and the tree as it was just before it ran. One click, no name asked.
func (s *Server) handleSaveFixture(w http.ResponseWriter, r *http.Request) {
	var msg ChatMessage
	for _, m := range s.chatStore().List(s.agent.Current()) {
		if m.ID == r.PathValue("id") && m.Kind == chat.KindYou {
			msg = m
		}
	}
	if msg.ID == "" {
		writeError(w, http.StatusNotFound, "that is not one of your messages")
		return
	}
	if msg.Undo == "" {
		writeError(w, http.StatusConflict, "there is no snapshot of the tree before that message")
		return
	}
	dir := fixtures.Dir(s.commonGitDir)
	name := fixtures.Name(dir, msg.Body)
	err := fixtures.Capture(r.Context(), s.space().homeRoot, dir, name, msg.Body, msg.Undo)
	switch {
	case errors.Is(err, fixtures.ErrNoSnapshot):
		writeError(w, http.StatusGone, "that snapshot is gone; only the last 20 are kept")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name, "dir": dir})
}
