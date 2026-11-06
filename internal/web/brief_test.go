package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeBrief puts two files under the checkout's docs and returns the newer.
func writeBrief(t *testing.T, s *Server) string {
	t.Helper()
	docs := filepath.Join(s.ws.gitDir, "ade", "docs")
	require.NoError(t, os.MkdirAll(docs, 0o750))
	older, newer := filepath.Join(docs, "first.md"), filepath.Join(docs, "lean-host.md")
	require.NoError(t, os.WriteFile(older, []byte("# First\n"), 0o600))
	require.NoError(t, os.WriteFile(newer, []byte("# Lean host\n\nStep 5 is next.\n"), 0o600))
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(older, past, past))
	return newer
}

func TestTheBriefIsTheNewestFileUnderTheCheckoutsDocs(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "GET", "/api/brief", "")
	assert.Equal(t, http.StatusNotFound, rec.Code, "no docs, no brief")

	want := writeBrief(t, s)
	rec = do(t, s, "GET", "/api/brief", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct{ Path, Body string }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, want, got.Path)
	assert.Equal(t, "# Lean host\n\nStep 5 is next.\n", got.Body)
}

func TestSendingTheBriefTypesOneLineAtTheAgent(t *testing.T) {
	fakeAgent(t)
	s := newTestServer(t)
	t.Cleanup(s.closeTerminal)

	rec := do(t, s, "POST", "/api/agent/brief", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Nil(t, s.agentTerm(), "no agent is started for a brief that is not there")

	want := writeBrief(t, s)
	rec = do(t, s, "POST", "/api/agent/brief", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"path":"`+want+`"}`, rec.Body.String())
	assert.Equal(t, "Read "+want+": it is the brief for this work. Say where it leaves off.", briefAsk(want))
	assert.Contains(t, seen(t, s.agentTerm(), "Say where it leaves off."), "it is the brief for this work")
}

func TestTheBriefIsNotSentOverATurnInFlight(t *testing.T) {
	s := newTestServer(t)
	writeBrief(t, s)
	s.agent.running = true

	rec := do(t, s, "POST", "/api/agent/brief", "")
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Nil(t, s.agentTerm())
}
