package launcher

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/owenrumney/ade/internal/editor"
)

// openEditor is how a checkout is put in front of the user. A variable so a
// test can stand in for it rather than start an editor.
var openEditor = editor.OpenDesk //nolint:gochecknoglobals // a seam for tests

// handleEditor shows a checkout in the editor. The page asks after it has
// opened a session: the editor is where a session is looked at, not a tab.
func (s *Server) handleEditor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	// A real checkout only: the path is handed to a program as an argument.
	dir, ok := browsed(req.Dir)
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not a checkout on this machine")
		return
	}
	if err := openEditor(dir.Path); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, editor.ErrNone) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
