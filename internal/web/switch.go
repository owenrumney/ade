package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/owenrumney/ade/internal/chat"
)

// handleChatSwitch continues this conversation on the other provider: the
// prose crosses as context, the session id cannot.
func (s *Server) handleChatSwitch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Message  string `json:"message"`
		// NoTurn moves the conversation without spending a turn on it: the
		// command line asks for a provider, not for the agent to say something.
		NoTurn  bool  `json:"noTurn"`
		Sandbox *bool `json:"sandbox"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !validProvider(req.Provider) {
		writeError(w, http.StatusBadRequest, "choose claude or codex")
		return
	}
	to := normalizeProvider(req.Provider)
	state := s.agent.State()
	// No model named means the one it is already on, not a move to the default.
	if to == state.Provider && (req.Model == "" || req.Model == state.Model) {
		writeError(w, http.StatusBadRequest, "this conversation is already there")
		return
	}
	// Never mid-run: a turn in flight has a tool call the other has no id for.
	if state.Running {
		writeError(w, http.StatusConflict, "wait for this run to finish, or stop it, before moving providers")
		return
	}
	wantSandbox := state.Sandbox
	if req.Sandbox != nil {
		wantSandbox = *req.Sandbox
	} else if to == "codex" {
		wantSandbox = true
	}
	if wantSandbox && !state.Sandbox {
		if err := s.agent.ensureRuntime(r.Context()); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	said := s.conversationProse()
	if req.NoTurn || strings.TrimSpace(said) == "" {
		// startFresh resets the provider, session and pending conversation, so
		// it needs the run held against a turn starting between the two.
		if _, ok := s.agent.begin(s.ctx, kindChat); !ok {
			writeError(w, http.StatusConflict, "the agent is already running")
			return
		}
		if wantSandbox != state.Sandbox {
			s.agent.setSandbox(wantSandbox)
		}
		s.startFresh(to, req.Model)
		s.agent.finish(nil)
		s.hub.Broadcast("agent", nil)
		s.hub.Broadcast("state", nil)
		writeJSON(w, http.StatusAccepted, map[string]any{"provider": to})
		return
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		message = "Carry on from where the last provider left off."
	}
	from := s.agent.Current()
	runCtx, ok := s.agent.begin(s.ctx, kindChat)
	if !ok {
		writeError(w, http.StatusConflict, "the agent is already running")
		return
	}

	if wantSandbox != state.Sandbox {
		s.agent.setSandbox(wantSandbox)
	}
	s.startFresh(to, req.Model)
	carried := &chat.Context{From: switchLabel(state.Provider, from), Text: said}
	s.say(ChatMessage{Kind: chat.KindYou, Body: message, Context: carried})
	s.hub.Broadcast("state", nil)
	s.spawn(func() { s.runChat(runCtx, carried, message, nil) })
	writeJSON(w, http.StatusAccepted, map[string]any{"provider": to})
}

// switchLabel puts the break in the transcript rather than leaving it implied.
func switchLabel(provider, session string) string {
	was := provider
	if was == "" {
		was = "claude"
	}
	if session == "" {
		return "carried over from " + was
	}
	return fmt.Sprintf("carried over from %s session %s", was, session[:min(8, len(session))])
}
