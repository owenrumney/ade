package web

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/permissions"
)

// HookPath is where the agent's PreToolUse hook reaches this server.
const HookPath = "/api/hook/pretooluse"

// handleHook decides one tool call. The request is held open while the card is
// on screen, so the agent's own timeout is the one that bounds it.
func (s *Server) handleHook(w http.ResponseWriter, r *http.Request) {
	if !s.hookAuthorized(r) {
		slog.Warn("hook request not authorized", "from", r.RemoteAddr)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var request bridge.HookRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "that is not a hook request")
		return
	}
	gate, ok := s.agent.gate()
	if !ok {
		// Nothing is gating, so nothing can answer. Allowing here would make a
		// dead gate indistinguishable from an open one.
		writeJSON(w, http.StatusOK, denyHook("ade is not gating this session"))
		return
	}
	response := gate.Hook(r.Context(), request)
	slog.Debug("gated a tool call", "tool", request.ToolName, "target", request.Target(),
		"decision", response.Output.PermissionDecision, "why", response.Output.PermissionDecisionReason)
	writeJSON(w, http.StatusOK, response)
	if len(response.Rules) > 0 {
		s.ruleAnswered(request.ToolUseID, strings.Join(response.Rules, ", "))
	}
	s.agent.touch()
}

// ruleAnswered names the rule on the call's row, or holds it until the row is
// written: the hook and the stream race, and either may arrive first.
func (s *Server) ruleAnswered(toolID, rule string) {
	s.mu.Lock()
	id, written := s.tools[toolID]
	if !written {
		if s.ruled == nil {
			s.ruled = map[string]string{}
		}
		s.ruled[toolID] = rule
	}
	s.mu.Unlock()
	if !written {
		return
	}
	for _, msg := range s.chatStore().List(s.agent.Current()) {
		if msg.ID == id {
			msg.Rule = rule
			s.amend(msg)
			return
		}
	}
}

// remember writes down a rule the card decided on.
func (s *Server) remember(rule permissions.Rule, deny, session bool) error {
	rules := s.agent.rulesStore()
	if rules == nil {
		return errors.New("ade has no rule file to remember that in")
	}
	return rules.Remember(rule, deny, session)
}

// handleFree turns the asking off for the rest of the session, or back on.
func (s *Server) handleFree(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On bool `json:"on"`
	}
	if !decode(w, r, &req) {
		return
	}
	rules := s.agent.rulesStore()
	if rules == nil {
		writeError(w, http.StatusConflict, "ade has no rules to stop checking")
		return
	}
	rules.SetFree(req.On)
	slog.Info("permission asking", "free", req.On)
	s.agent.touch()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) hookAuthorized(r *http.Request) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return s.hookToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.hookToken)) == 1
}

func denyHook(reason string) bridge.HookResponse {
	return bridge.HookResponse{Output: bridge.HookOutput{
		HookEventName:            "PreToolUse",
		PermissionDecision:       "deny",
		PermissionDecisionReason: reason,
	}}
}
