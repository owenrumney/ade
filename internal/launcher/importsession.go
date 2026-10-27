package launcher

import (
	"errors"
	"net/http"

	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/spaces"
)

// status is what each refusal reads as over HTTP.
var status = map[error]int{
	sessions.ErrNotAnID:    http.StatusBadRequest,
	sessions.ErrNoSuchID:   http.StatusNotFound,
	sessions.ErrUnreadable: http.StatusUnprocessableEntity,
	sessions.ErrNotYours:   http.StatusUnprocessableEntity,
	sessions.ErrGone:       http.StatusGone,
}

// handleSessionImport pins a conversation into a space by id, for one the list
// window drops. One from another repository comes back named, not refused.
func (s *Server) handleSessionImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo string `json:"repo"`
		ID   string `json:"id"`
		// Into is the checkout adopting it, or empty for where it was recorded.
		Into string `json:"into"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	all, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Into != "" {
		if _, ok := sessions.FindCheckout(all, req.Into); !ok {
			writeError(w, http.StatusBadRequest, "that is not one of the checkouts on this machine")
			return
		}
	}

	found, err := sessions.Resolve(req.ID, req.Into)
	if err != nil {
		for known, code := range status {
			if errors.Is(err, known) {
				writeError(w, code, err.Error())
				return
			}
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if found.Live {
		writeError(w, http.StatusConflict, "that session has an agent attached")
		return
	}

	holding := spaces.KeyOf(repoHolding(all, found.Dir))
	if want := spaces.Clean([]string{req.Repo}); len(want) > 0 && holding != want[0] {
		writeJSON(w, http.StatusOK, map[string]any{"session": found, "elsewhere": holding})
		return
	}
	if err := sessions.Pin(found.ID, found.Dir); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": found, "repo": holding})
}
