package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/rewind"
)

// snapshotWait bounds the snapshot a message waits on before it is said.
const snapshotWait = 20 * time.Second

// snapshotTurn saves the working tree before a message runs, and answers the
// name to undo by. Empty when there is nothing local to save.
func (s *Server) snapshotTurn() string {
	ws := s.space()
	if ws.snapshot || ws.homeRoot == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(s.ctx, snapshotWait)
	defer cancel()
	name, err := rewind.Save(ctx, ws.homeRoot)
	if err != nil {
		slog.Debug("snapshot before the turn", "err", err)
		return ""
	}
	return name
}

// undoTarget is the message and the snapshot an undo of it restores: the one
// taken before it ran, or the one taken before it was undone.
func (s *Server) undoTarget(id string) (ChatMessage, string, bool) {
	for _, msg := range s.chatStore().List(s.agent.Current()) {
		if msg.ID != id || msg.Kind != chat.KindYou {
			continue
		}
		if msg.Undone != "" {
			return msg, msg.Undone, true
		}
		return msg, msg.Undo, msg.Undo != ""
	}
	return ChatMessage{}, "", false
}

// handleUndoPlan is what undoing a turn would change, for the confirmation.
func (s *Server) handleUndoPlan(w http.ResponseWriter, r *http.Request) {
	_, name, ok := s.undoTarget(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "there is no snapshot for that message")
		return
	}
	changes, err := rewind.Plan(r.Context(), s.space().homeRoot, name)
	if err != nil {
		writeRewindError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changes": changesOrEmpty(changes)})
}

// handleUndo puts the files back to how they were before a message, or back
// again when that was already done. The conversation is not edited.
func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) {
	msg, name, ok := s.undoTarget(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "there is no snapshot for that message")
		return
	}
	if s.agent.State().Running || len(s.sideRuns()) > 0 {
		writeError(w, http.StatusConflict, "a turn is running; stop it first")
		return
	}
	changes, before, err := rewind.Restore(r.Context(), s.space().homeRoot, name)
	if err != nil {
		writeRewindError(w, err)
		return
	}
	redo := msg.Undone != ""
	if len(changes) > 0 {
		msg.Undone = before
		if redo {
			msg.Undone = ""
		}
		s.amend(msg)
		s.tellAgentOfUndo(msg, changes, redo)
	}
	if err := s.refresh(s.ctx); err != nil {
		slog.Debug("refresh after an undo", "err", err)
	}
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"changes": changesOrEmpty(changes)})
}

// tellAgentOfUndo rides on the next message: an agent that does not know its
// edits are gone builds on them.
func (s *Server) tellAgentOfUndo(msg ChatMessage, changes []rewind.Change, redo bool) {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		paths = append(paths, change.Path)
	}
	const most = 30
	listed := strings.Join(paths[:min(most, len(paths))], ", ")
	if len(paths) > most {
		listed += fmt.Sprintf(", and %d more", len(paths)-most)
	}
	what := "undid every file change made since"
	if redo {
		what = "put back the file changes they had undone, made since"
	}
	text := fmt.Sprintf("The user %s their message %q. These files are not as you left them: %s. Read a file again before you build on it.",
		what, firstLine(msg.Body), listed)
	s.mu.Lock()
	s.heard = append(s.heard, design.Heard{Who: "ade", Text: text})
	s.mu.Unlock()
}

func writeRewindError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, rewind.ErrGone):
		writeError(w, http.StatusGone, "that snapshot is gone; only the last 20 are kept")
	case errors.Is(err, rewind.ErrMoved):
		writeError(w, http.StatusConflict, "a commit was made since that message, so the files cannot be put back safely")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func changesOrEmpty(changes []rewind.Change) []rewind.Change {
	if changes == nil {
		return []rewind.Change{}
	}
	return changes
}
