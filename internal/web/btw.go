package web

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"unicode"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/design"
)

// asideName is who a /btw answer is written under, beside the named agents.
const asideName = "btw"

var asideTools = []string{"Read", "Grep", "Glob"}

// btwQuestion is the question in "/btw <question>". Claude Code's own /btw
// does not exist under -p, so ade answers it with a fork instead.
func btwQuestion(message string) (string, bool) {
	rest, ok := strings.CutPrefix(message, "/btw")
	if !ok || (rest != "" && !unicode.IsSpace(rune(rest[0]))) {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// handleAside answers beside the turn in flight; the main agent never hears it.
func (s *Server) handleAside(w http.ResponseWriter, question string, attached *chat.Context) {
	if question == "" {
		writeError(w, http.StatusBadRequest, "ask something after /btw")
		return
	}
	session, model, dir, ok := s.agent.forkable()
	if !ok {
		writeError(w, http.StatusConflict, "/btw forks the conversation, and this one has nothing to fork yet, or runs on Codex; ask it as a message")
		return
	}
	message := "/btw " + question
	s.say(ChatMessage{Kind: chat.KindYou, Body: message, Context: attached})

	from, text := "", ""
	if attached != nil {
		from, text = attached.From, attached.Text
	}
	prompt := design.AsidePrompt(from, text, question)
	started := s.startJobs([]sideJob{{name: asideName, cold: true, run: func(ctx context.Context) {
		s.runAside(ctx, session, model, dir, prompt)
	}}}, 0)
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"running": started, "aside": true})
}

// runAside streams like a named agent, but relays nothing to the main one.
func (s *Server) runAside(ctx context.Context, session, model, dir, prompt string) {
	_, err := s.coldClaude(ctx, bridge.OneShotOpts{
		Bin:            s.agent.bin,
		Dir:            dir,
		Model:          model,
		Prompt:         prompt,
		Fork:           session,
		Tools:          asideTools,
		AllowedTools:   asideTools,
		PermissionMode: "default",
		NoMCP:          true,
		OnEvent: func(ev bridge.Event) {
			s.onSubagentEventModel(asideName, model, "", "", ev)
		},
	})
	if err == nil {
		return
	}
	if ctx.Err() != nil {
		s.say(ChatMessage{Kind: chat.KindNote, Author: asideName, Run: runSubagent, Body: "/btw was stopped"})
		return
	}
	slog.Warn("btw fork failed", "session", session, "err", err)
	s.say(ChatMessage{Kind: chat.KindNote, Author: asideName, Run: runSubagent, Failed: true, Body: "/btw: " + err.Error()})
}
