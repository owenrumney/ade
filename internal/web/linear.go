package web

import (
	"net/http"
	"regexp"
	"strings"
	"time"
)

// linearInterval is how often your issues are re-read. They move by the
// hour, not the second, and the key is read-only, so slow is fine.
const linearInterval = 2 * time.Minute

// ticketIDRE is a Linear identifier as Linear writes it: TEAM-123. It goes
// onto a command line and into a branch name, so nothing else will do.
var ticketIDRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,9}-[0-9]{1,8}$`)

/* HTTP */

// handleLinearStart is the → on an issue: pick a repository, and its ade
// opens in a new tab on the ticket's worktree with the design drafted.
func (s *Server) handleLinearStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Identifier string `json:"identifier"`
		Dir        string `json:"dir"`
	}
	if !decode(w, r, &req) {
		return
	}
	url, path, err := s.board.StartTicket(r.Context(), strings.TrimSpace(req.Dir), req.Identifier)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url, "path": path})
}

func (s *Server) handleLinearRefresh(w http.ResponseWriter, _ *http.Request) {
	s.board.RefreshTickets(s.ctx)
	writeJSON(w, http.StatusOK, s.board.Tickets())
}
