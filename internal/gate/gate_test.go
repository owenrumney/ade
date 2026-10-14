package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// call is one PreToolUse body, as Claude Code sends it.
const call = `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_use_id":"t1","tool_input":{"command":"rm -rf /"}}`

func TestAGateThatCannotReachAdeRefusesTheCall(t *testing.T) {
	// The whole reason this is a command and not an http hook: Claude Code
	// lets a call through when an http hook is not answered. This answers.
	tests := []struct {
		name   string
		server func(t *testing.T) string
	}{
		{"nothing listening", func(*testing.T) string { return "http://127.0.0.1:9/api/hook/pretooluse" }},
		{"ade refused the caller", serverSaying(http.StatusForbidden, "forbidden")},
		{"ade answered rubbish", serverSaying(http.StatusOK, "not json")},
		{"ade decided nothing", serverSaying(http.StatusOK, `{"hookSpecificOutput":{}}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(URLVar, tt.server(t))
			var out bytes.Buffer
			_ = Run(context.Background(), strings.NewReader(call), &out)
			assert.Equal(t, "deny", decided(t, out.Bytes()).Output.PermissionDecision)
		})
	}
}

func TestAGateWithNowhereToAskRefusesRatherThanAssumes(t *testing.T) {
	t.Setenv(URLVar, "")
	var out bytes.Buffer
	err := Run(context.Background(), strings.NewReader(call), &out)
	require.Error(t, err)
	decision := decided(t, out.Bytes())
	assert.Equal(t, "deny", decision.Output.PermissionDecision)
	assert.Contains(t, decision.Output.PermissionDecisionReason, "Nothing was run")
}

func TestAdesDecisionIsPassedThroughWhole(t *testing.T) {
	var got struct {
		Tool string `json:"tool_name"`
		Auth string
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		got.Auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse",` +
			`"permissionDecision":"allow","permissionDecisionReason":"your rule Bash(go test:*)",` +
			`"updatedInput":{"command":"go test ./...","answers":{"q":"a"}}}}`))
	}))
	defer server.Close()
	t.Setenv(URLVar, server.URL)
	t.Setenv(TokenVar, "sesame")

	var out bytes.Buffer
	require.NoError(t, Run(context.Background(), strings.NewReader(call), &out))
	decision := decided(t, out.Bytes())
	assert.Equal(t, "allow", decision.Output.PermissionDecision)
	assert.Equal(t, "your rule Bash(go test:*)", decision.Output.PermissionDecisionReason)
	assert.Equal(t, "go test ./...", decision.Output.UpdatedInput["command"],
		"an answered question's input has to survive the trip")
	assert.Equal(t, "Bash", got.Tool, "the call was not carried to ade as it arrived")
	assert.Equal(t, "Bearer sesame", got.Auth)
}

func serverSaying(status int, body string) func(*testing.T) string {
	return func(t *testing.T) string {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(server.Close)
		return server.URL
	}
}

func decided(t *testing.T, out []byte) Decision {
	t.Helper()
	var decision Decision
	require.NoError(t, json.Unmarshal(out, &decision), "the gate printed something Claude Code cannot read")
	assert.Equal(t, "PreToolUse", decision.Output.HookEventName)
	return decision
}
