package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/verify"
)

// callChecks makes one JSON-RPC call to the checks server, as a run would.
func callChecks(t *testing.T, s *Server, token, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest("POST", ChecksMCPPath, strings.NewReader(body))
	if token != "" {
		req.Header.Set("X-Ade-Delegate-Token", token)
	}
	rec := httptest.NewRecorder()
	s.handleChecksMCP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

func TestTheChecksServerRefusesAnyoneWithoutTheRunsOwnToken(t *testing.T) {
	s := &Server{delegateToken: "secret"}
	for _, header := range []string{"", "wrong"} {
		req := httptest.NewRequest("POST", ChecksMCPPath, strings.NewReader(`{"id":1,"method":"ping"}`))
		if header != "" {
			req.Header.Set("X-Ade-Delegate-Token", header)
		}
		rec := httptest.NewRecorder()
		s.handleChecksMCP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code, header)
	}
}

func TestCodexReachesItWithABearerToken(t *testing.T) {
	s := &Server{delegateToken: "secret"}
	req := httptest.NewRequest("POST", ChecksMCPPath, strings.NewReader(`{"id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	s.handleChecksMCP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestOnlyTheChecksMarkedAsToolsAreOffered(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, verify.Save(verify.Path(root), verify.Config{Checks: []verify.Check{
		{Name: "test", Run: "go test ./...", Tool: true, Filter: "-run=%s"},
		{Name: "lint", Run: "golangci-lint run"},
	}}))
	s := &Server{delegateToken: "t", commonGitDir: root}

	got := callChecks(t, s, "t", `{"id":1,"method":"tools/list"}`)

	tools := got["result"].(map[string]any)["tools"].([]any)
	require.Len(t, tools, 1, "a check not marked as a tool is the post-turn list's")
	one := tools[0].(map[string]any)
	assert.Equal(t, "run_test", one["name"])
	props := one["inputSchema"].(map[string]any)["properties"].(map[string]any)
	assert.Contains(t, props, "paths")
	assert.Contains(t, props, "filter")
}

func TestACheckWithNoFilterFormOffersNoPattern(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, verify.Save(verify.Path(root), verify.Config{Checks: []verify.Check{
		{Name: "build", Run: "go build ./...", Tool: true},
	}}))
	s := &Server{delegateToken: "t", commonGitDir: root}

	got := callChecks(t, s, "t", `{"id":1,"method":"tools/list"}`)

	tools := got["result"].(map[string]any)["tools"].([]any)
	props := tools[0].(map[string]any)["inputSchema"].(map[string]any)["properties"].(map[string]any)
	assert.NotContains(t, props, "filter")
}

// A box with no toolchain and no tools is a dead end, so the refusal has to
// say what to do about it.
func TestAnUnknownCheckSaysWhatThereIs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, verify.Save(verify.Path(root), verify.Config{Checks: []verify.Check{
		{Name: "test", Run: "true", Tool: true},
	}}))
	s := &Server{delegateToken: "t", commonGitDir: root}

	got := callChecks(t, s, "t", `{"id":1,"method":"tools/call","params":{"name":"run_nope"}}`)

	said := textOf(t, got)
	assert.Contains(t, said, "run_test")
	assert.True(t, got["result"].(map[string]any)["isError"].(bool))
}

func TestACheckoutWithNoChecksSaysSoRatherThanNothing(t *testing.T) {
	s := &Server{delegateToken: "t", commonGitDir: t.TempDir()}

	got := callChecks(t, s, "t", `{"id":1,"method":"tools/call","params":{"name":"run_test"}}`)

	assert.Contains(t, textOf(t, got), "checks.json")
}

func textOf(t *testing.T, got map[string]any) string {
	t.Helper()
	result, ok := got["result"].(map[string]any)
	require.True(t, ok, got)
	content := result["content"].([]any)
	return content[0].(map[string]any)["text"].(string)
}
