package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/design"
)

const answerWithin = 90 * time.Second

// handleAnswer is /btw with the reply handed back instead of posted: a fork of
// the conversation answers one question, read-only, and the caller waits.
func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Question string `json:"question"`
	}
	if !decode(w, r, &req) {
		return
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		writeError(w, http.StatusBadRequest, "ask something")
		return
	}
	session, model, dir, ok := s.agent.forkable()
	if !ok {
		writeError(w, http.StatusConflict, "nothing to fork yet, or this runs on Codex; ask it as a message")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), answerWithin)
	defer cancel()
	turn, err := s.coldClaude(ctx, bridge.OneShotOpts{
		Bin:            s.agent.bin,
		Dir:            dir,
		Model:          model,
		Prompt:         answerPrompt(question),
		Fork:           session,
		Tools:          asideTools,
		AllowedTools:   asideTools,
		PermissionMode: "default",
		NoMCP:          true,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"answer": strings.TrimSpace(turn.Text)})
}

func answerPrompt(question string) string {
	return design.AsidePrompt("", "", question) +
		"\n\nAnswer in one or two short lines, plain text, no preamble. If it cannot be answered that briefly, say so in one line."
}
