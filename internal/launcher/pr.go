package launcher

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/owenrumney/ade/internal/ghpr"
)

// handlePR is one pull request in full, with its checks: what a window
// shows when one is picked, with no session server behind it.
func (s *Server) handlePR(w http.ResponseWriter, r *http.Request) {
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	number, err := strconv.Atoi(r.URL.Query().Get("number"))
	if repo == "" || err != nil || number <= 0 {
		writeError(w, http.StatusBadRequest, "repo and number are required")
		return
	}
	repos, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// gh runs in a checkout of that repository, which is how it knows
	// which forge and which credentials to use.
	dir, ok := checkoutNamed(repos, repo)
	if !ok {
		writeError(w, http.StatusNotFound, "this machine has no checkout of "+repo)
		return
	}
	pr, err := ghpr.Fetch(r.Context(), dir, number)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// Checks are a second read, and a failure there is not a failure to
	// show the pull request: the page says what it has.
	checks, err := ghpr.Checks(r.Context(), dir, number)
	if err != nil {
		checks = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"pr": pr, "checks": checks})
}
