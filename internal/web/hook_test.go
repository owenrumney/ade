package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
)

// fakeGate is a running agent that decides every call the same way.
type fakeGate struct {
	decision string
	rules    []string
	got      []bridge.HookRequest
}

func (g *fakeGate) Send(context.Context, string, ...bridge.Image) (*bridge.Turn, error) {
	return &bridge.Turn{}, nil
}
func (g *fakeGate) SendCommand(context.Context, string) (*bridge.Turn, error) {
	return &bridge.Turn{}, nil
}
func (g *fakeGate) Inject(string, ...bridge.Image) error { return nil }
func (g *fakeGate) Busy() bool                           { return false }
func (g *fakeGate) Dead() bool                           { return false }
func (g *fakeGate) Close() error                         { return nil }
func (g *fakeGate) Hook(_ context.Context, request bridge.HookRequest) bridge.HookResponse {
	g.got = append(g.got, request)
	return bridge.HookResponse{Output: bridge.HookOutput{HookEventName: "PreToolUse", PermissionDecision: g.decision}, Rules: g.rules}
}

func runningGate(s *Server, gate *fakeGate) {
	s.hookToken = "hook-token"
	s.agent.mu.Lock()
	s.agent.agent, s.agent.running = gate, true
	s.agent.mu.Unlock()
}

func toolRow(t *testing.T, s *Server) ChatMessage {
	t.Helper()
	rows := messagesOfKind(s.conversation(), chat.KindTool)
	require.Len(t, rows, 1)
	return rows[0]
}

var goTestCall = bridge.Event{ToolUses: []bridge.ToolUse{{
	ID: "toolu_1", Name: "Bash", Input: json.RawMessage(`{"command":"go test ./..."}`),
}}}

func TestTheRuleThatAnsweredIsNamedOnTheCallWhicheverArrivesFirst(t *testing.T) {
	t.Run("the hook before the row", func(t *testing.T) {
		s := newTestServer(t)
		runningGate(s, &fakeGate{decision: "allow", rules: []string{"Bash(go test:*)"}})
		hookCall(t, s, "hook-token")
		s.writeEvent(kindChat, goTestCall)
		assert.Equal(t, "Bash(go test:*)", toolRow(t, s).Rule)
	})
	t.Run("the row before the hook", func(t *testing.T) {
		s := newTestServer(t)
		runningGate(s, &fakeGate{decision: "allow", rules: []string{"Bash(go test:*)"}})
		s.writeEvent(kindChat, goTestCall)
		hookCall(t, s, "hook-token")
		assert.Equal(t, "Bash(go test:*)", toolRow(t, s).Rule)
	})
	t.Run("a card, not a rule", func(t *testing.T) {
		s := newTestServer(t)
		runningGate(s, &fakeGate{decision: "allow"})
		s.writeEvent(kindChat, goTestCall)
		hookCall(t, s, "hook-token")
		assert.Empty(t, toolRow(t, s).Rule, "a call you allowed yourself was credited to a rule")
	})
}

func hookCall(t *testing.T, s *Server, bearer string) (*httptest.ResponseRecorder, bridge.HookResponse) {
	t.Helper()
	body := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"go test ./..."},"tool_use_id":"toolu_1"}`
	req := httptest.NewRequest("POST", HookPath, strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	// No session token: `ade hook` holds only its own.
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	var response bridge.HookResponse
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	}
	return rec, response
}

func TestTheGateRouteTakesOnlyItsOwnToken(t *testing.T) {
	s := newTestServer(t)
	s.hookToken = "hook-token"

	rec, _ := hookCall(t, s, "")
	assert.Equal(t, http.StatusForbidden, rec.Code, "no bearer at all")
	rec, _ = hookCall(t, s, s.token)
	assert.Equal(t, http.StatusForbidden, rec.Code, "the session token authorises every route; it must not decide a tool call")
	rec, _ = hookCall(t, s, "hook-token")
	assert.Equal(t, http.StatusOK, rec.Code)

	s.hookToken = ""
	rec, _ = hookCall(t, s, "")
	assert.Equal(t, http.StatusForbidden, rec.Code, "an unset token matched an empty bearer")
}

func TestAGateWithNothingRunningDeniesRatherThanAllows(t *testing.T) {
	s := newTestServer(t)
	s.hookToken = "hook-token"

	rec, response := hookCall(t, s, "hook-token")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "deny", response.Output.PermissionDecision, "a dead gate read as an open one")
	assert.NotEmpty(t, response.Output.PermissionDecisionReason)
}

func TestTheGateRouteHandsTheCallToTheRunningAgent(t *testing.T) {
	s := newTestServer(t)
	gate := &fakeGate{decision: "allow"}
	runningGate(s, gate)

	rec, response := hookCall(t, s, "hook-token")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "allow", response.Output.PermissionDecision)
	require.Len(t, gate.got, 1)
	assert.Equal(t, "go test ./...", gate.got[0].Target())
	assert.Equal(t, "toolu_1", gate.got[0].ToolUseID)
}
