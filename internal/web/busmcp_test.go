package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rpc(t *testing.T, s *Server, body string) map[string]any {
	t.Helper()
	rec := delegateDo(t, s, body)
	require.Equal(t, http.StatusOK, rec.Code)
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

// delegateDo calls as a run does, with the token ade hands its agent.
func delegateDo(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", BusMCPPath, strings.NewReader(body))
	if s.delegateToken == "" {
		s.delegateToken = "test-delegate-token"
	}
	req.Header.Set("X-Ade-Delegate-Token", s.delegateToken)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	return rec
}

// The tools reach other people. An unguarded endpoint would let anything that
// can reach the port ask your colleagues in your name.
func TestTheBusToolsNeedTheToken(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("POST", BusMCPPath,
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("X-Ade-Token", s.token)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, "the browser's token is not the agent's")
}

func TestTheBusServerNamesItselfAgentbus(t *testing.T) {
	s := newTestServer(t)
	out := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	info := out["result"].(map[string]any)["serverInfo"].(map[string]any)
	// The agentbus skills name this server, so renaming it breaks them.
	assert.Equal(t, "agentbus", info["name"])
}

func TestTheToolsOfferedAreTheOnesAnAgentCanUse(t *testing.T) {
	s := newTestServer(t)
	out := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	var names []string
	for _, raw := range out["result"].(map[string]any)["tools"].([]any) {
		names = append(names, raw.(map[string]any)["name"].(string))
	}
	assert.ElementsMatch(t, []string{"ask_human", "request_review", "who_is_online"}, names)

	// Answering and claiming belong to the person. A run must not be able to
	// answer a question somebody put to its human.
	assert.NotContains(t, names, "answer")
	assert.NotContains(t, names, "claim_review")
}

func TestAnUnknownToolIsRefusedRatherThanIgnored(t *testing.T) {
	s := newTestServer(t)
	out := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_everything"}}`)
	require.Contains(t, out, "error")
	assert.Contains(t, out["error"].(map[string]any)["message"], "unknown tool")
}

// Without a launcher there is no connection, and the agent is told so rather
// than left waiting on a tool that cannot work.
func TestAToolCallWithNoLauncherSaysSo(t *testing.T) {
	s := newTestServer(t)
	out := rpc(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask_human","arguments":{"question":"safe?"}}}`)
	result := out["result"].(map[string]any)
	assert.True(t, result["isError"].(bool))
	assert.Contains(t, result["content"].([]any)[0].(map[string]any)["text"], "launcher is not running")
}

// A notification has no id and takes no reply.
func TestANotificationIsAcceptedWithoutAnAnswer(t *testing.T) {
	s := newTestServer(t)
	rec := delegateDo(t, s, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Empty(t, rec.Body.String())
}
