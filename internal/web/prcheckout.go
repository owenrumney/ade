package web

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/gitdiff"
)

// handlePRCheckout is the button beside the composer: checkout, nothing typed.
func (s *Server) handlePRCheckout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message string          `json:"message"`
		Files   []chat.Incoming `json:"files"`
	}
	if !decodeUpTo(w, r, &req, maxChatBody) {
		return
	}
	if !s.space().snapshot {
		writeError(w, http.StatusBadRequest, "this pull request is already checked out here")
		return
	}
	s.checkOutAndHandOff(w, r, strings.TrimSpace(req.Message), nil, req.Files)
}

// checkOutAndHandOff puts the pull request in the review board's worktree and
// hands it an ade. The checkout being served is never touched.
func (s *Server) checkOutAndHandOff(w http.ResponseWriter, r *http.Request, message string, selected *chat.Context, files []chat.Incoming) {
	ws := s.space()
	if ws.pr == nil {
		writeError(w, http.StatusBadRequest, "no pull request is open, so there is nothing to check out")
		return
	}
	path, err := s.checkOutPR(r.Context(), ws.homeRoot, ws.pr.Number)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// gh leaves the checkout on the pull request's own head, which is the name
	// to push to; the worktree directory is named after the number.
	branch := gitdiff.Branch(r.Context(), path)
	if branch == "detached" {
		branch = filepath.Base(path)
	}
	// A prompt carries this session's sandbox choice, not the machine default.
	boxed := s.agent.sandboxOn()
	s.handOff(w, path, branch, message, selected, files,
		s.agent.providerName(), s.agent.modelName(), &boxed)
}
