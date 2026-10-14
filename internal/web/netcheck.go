package web

import (
	"net/http"

	"github.com/owenrumney/ade/internal/netcheck"
)

// handleNetCheck is the shared probe: the same table and the same descriptor
// count the launcher answers with, asked of this process.
func (s *Server) handleNetCheck(w http.ResponseWriter, r *http.Request) {
	netcheck.Handler()(w, r)
}
