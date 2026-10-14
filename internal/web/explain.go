package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/review"
)

const explainAgent = "eli5"

// handleExplain has a named agent read the diff back in plain words. A side
// run like a review, except nothing it says is filed.
func (s *Server) handleExplain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Agent string `json:"agent"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Agent)
	if name == "" {
		name = explainAgent
	}
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	if diff == nil || len(diff.Files) == 0 {
		writeError(w, http.StatusBadRequest, "nothing to explain: the diff is empty")
		return
	}
	// User scope only, as a review: the branch must not supply its own reader.
	agent, ok := agents.Find(s.userAgentsList(), name)
	if !ok {
		writeError(w, http.StatusBadRequest, "there is no agent called @"+name+" — run `ade agents install`")
		return
	}
	// The prompt says read it, do not change it; the tool set has to say so too.
	if writesCode(agentTools(agent)) {
		writeError(w, http.StatusBadRequest, "@"+name+" can edit the checkout, so it does not explain one")
		return
	}
	call := sideCall{agent: agent, prompt: design.ExplainPrompt(name, s.reviewDocPath(), review.RenderDiff(diff))}
	s.say(ChatMessage{Kind: chat.KindYou, Body: fmt.Sprintf("@%s explain the diff (%d files)", name, len(diff.Files))})
	started := s.startSides([]sideCall{call})
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	if !started {
		writeError(w, http.StatusConflict, fmt.Sprintf("no room for another agent; %d at most, and one is kept for a rewrite. Wait for one, or stop them", maxSides))
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "agent": name})
}
