package web

import (
	"bytes"
	"cmp"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/gate"
	"github.com/owenrumney/ade/internal/permissions"
)

// BadgePath is where an agent CLI's own hooks report, through `ade badge`.
const BadgePath = "/api/agent/badge"

// What the agent's terminal is doing. Blocked is told as needs, and unlike a
// finished turn it does not clear by being looked at.
const (
	badgeWorking = "working"
	badgeNeeds   = "needs"
	badgeBlocked = "blocked"
	badgeIdle    = "idle"
	badgeEnded   = "ended"
)

// quietAfter is how long a working agent may print nothing. Both CLIs animate
// while they work, so silence is a turn that stopped without saying so.
const quietAfter = 10 * time.Second

// badgeEvents is every hook claude is asked to report.
var badgeEvents = []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "PermissionRequest", "Notification", "Stop"}

// cliSession is an id either CLI gives a conversation, and never a flag.
var cliSession = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{7,63}$`)

// hookSaid is one report, in either CLI's words.
type hookSaid struct {
	Event   string `json:"hook_event_name"`
	Session string `json:"session_id"`
	Notice  string `json:"notification_type"`
	Kind    string `json:"type"`
	Thread  string `json:"thread-id"`
}

// badge is what a report makes of the agent, or empty when it changes nothing.
func (h hookSaid) badge() string {
	switch h.Event {
	case "UserPromptSubmit", "PostToolUse":
		return badgeWorking
	case "Stop":
		return badgeNeeds
	case "PermissionRequest":
		return badgeBlocked
	case "Notification":
		if h.Notice == "permission_prompt" || h.Notice == "elicitation_dialog" {
			return badgeBlocked
		}
	}
	if h.Kind == "agent-turn-complete" {
		return badgeNeeds
	}
	return ""
}

func (h hookSaid) session() string {
	id := cmp.Or(h.Session, h.Thread)
	if !cliSession.MatchString(id) {
		return ""
	}
	return id
}

func (s *Server) handleAgentBadge(w http.ResponseWriter, r *http.Request) {
	if !s.hookAuthorized(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var said hookSaid
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&said); err != nil {
		writeError(w, http.StatusBadRequest, "that is not a hook report")
		return
	}
	// The CLI names its own conversation, and that name is what resumes it.
	if id := said.session(); id != "" && id != s.agent.Current() {
		s.agent.note(id)
	}
	badge := said.badge()
	if s.agent.setBadge(badge) {
		s.badgeChanged()
	}
	// A finished turn is when the findings file says what became of each.
	if badge == badgeNeeds {
		s.readFindingsBack()
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAgentSeen is the window saying its user is looking at the agent.
func (s *Server) handleAgentSeen(w http.ResponseWriter, _ *http.Request) {
	if s.agent.seen() {
		s.badgeChanged()
	}
	w.WriteHeader(http.StatusNoContent)
}

// badgeChanged tells this server's pages, and the launcher for everyone else's.
func (s *Server) badgeChanged() {
	s.hub.Broadcast("state", nil)
	ctx := s.ctx
	if ctx == nil {
		return
	}
	s.spawn(func() {
		if err := s.announceServing(ctx, s.railID()); err != nil {
			slog.Debug("tell the launcher the agent's badge moved", "err", err)
		}
	})
}

// agentBadge is what the agent's terminal is doing, or empty when it has none.
func (s *Server) agentBadge() string {
	held := s.agentTerm()
	switch {
	case held == nil || !held.Started():
		return ""
	case !held.Alive():
		return badgeEnded
	}
	return s.agent.badgeAfter(held.Quiet())
}

// agentAlive is an agent CLI running in its pane, watched or not.
func (s *Server) agentAlive() bool {
	held := s.agentTerm()
	return held != nil && held.Alive()
}

// keyed is the agent's terminal being typed into. Codex has no hook for a
// message sent, so Enter stands in for one.
func (s *Server) keyed(data []byte) {
	if bytes.IndexByte(data, '\r') < 0 || !s.agent.isCodex() {
		return
	}
	if s.agent.setBadge(badgeWorking) {
		s.badgeChanged()
	}
}

func (a *agentRunner) isCodex() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.provider == "codex"
}

// setBadge records what the agent's terminal is doing, and whether that moved.
func (a *agentRunner) setBadge(badge string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if badge == "" || a.badge == badge {
		return false
	}
	a.badge = badge
	return true
}

// freshBadge is a terminal just started: at work if it was given a message.
func (a *agentRunner) freshBadge(prompted bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.badge = badgeIdle
	if prompted {
		a.badge = badgeWorking
	}
}

// seen clears a finished turn once its user has looked at it.
func (a *agentRunner) seen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.badge != badgeNeeds {
		return false
	}
	a.badge = badgeIdle
	return true
}

// badgeAfter is the badge as told, given how long the terminal has been quiet.
func (a *agentRunner) badgeAfter(quiet time.Duration) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.badge == badgeWorking && quiet > quietAfter {
		a.badge = badgeIdle
	}
	switch a.badge {
	case "":
		return badgeIdle
	case badgeBlocked:
		return badgeNeeds
	}
	return a.badge
}

// badgeEnv is how `ade badge` in the agent's terminal reaches this server.
func (a *agentRunner) badgeEnv() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.hookBase == "" {
		return nil
	}
	return []string{
		gate.BadgeURLVar + "=" + strings.TrimSuffix(a.hookBase, "/") + BadgePath,
		gate.TokenVar + "=" + a.hookToken,
	}
}

// badgeHooks is claude's hooks section: every event runs this binary's badge.
func badgeHooks() map[string]any {
	self, err := os.Executable()
	if err != nil {
		return nil
	}
	run := []any{map[string]any{"hooks": []any{map[string]any{
		"type": "command", "command": shellQuote(self) + " badge", "timeout": 5,
	}}}}
	hooks := map[string]any{}
	for _, event := range badgeEvents {
		hooks[event] = run
	}
	return hooks
}

// codexNotify is the override that points codex's turn-complete at this binary.
func codexNotify() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	program, err := json.Marshal([]string{self, "badge"})
	if err != nil {
		return ""
	}
	return "notify=" + string(program)
}

// codexDenies writes ade's deny list where codex reads rules. A failure is
// said and not fatal: the user's own rules still hold.
func (a *agentRunner) codexDenies() {
	a.mu.Lock()
	rules := a.rules
	a.mu.Unlock()
	if rules == nil {
		return
	}
	file, _ := rules.Rules()
	if err := permissions.WriteCodexRules(permissions.Wrapped(file.Deny)); err != nil {
		slog.Warn("write ade's deny list for codex", "err", err)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
