package said

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/delegate"
)

// serve stands in for the checkout's server and records what was asked.
func serve(t *testing.T, body string, status int) (*httptest.Server, *http.Request) {
	t.Helper()
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestAskingOutsideASessionSaysSo(t *testing.T) {
	t.Setenv(EnvURL, "")
	t.Setenv(EnvToken, "")

	_, err := Ask(context.Background(), "security")
	require.ErrorIs(t, err, ErrNoSession)
}

func TestAskCarriesTheCapabilityAndTheName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "secret", r.Header.Get(delegate.Header))
		assert.Equal(t, "security", r.URL.Query().Get("agent"))
		assert.Equal(t, "/api/internal/delegate/said", r.URL.Path)
		_, _ = w.Write([]byte(`{"agent":"security","known":true,"said":[
			{"who":"security","kind":"agent","at":"2026-09-10T09:00:00Z","text":"Six findings, filed."},
			{"who":"security","kind":"note","at":"2026-09-10T09:00:01Z","text":"filed 6 comment(s)"}]}`))
	}))
	defer srv.Close()
	t.Setenv(EnvURL, srv.URL+"/")
	t.Setenv(EnvToken, "secret")

	// An @ in front is how it is written in the chat, so it is accepted.
	answer, err := Ask(context.Background(), " @security ")
	require.NoError(t, err)
	assert.True(t, answer.Known)
	require.Len(t, answer.Said, 2)
	assert.Equal(t, "Six findings, filed.", answer.Said[0].Text)
	assert.Equal(t, "note", answer.Said[1].Kind)
}

func TestAskReportsWhatTheServerRefused(t *testing.T) {
	srv, _ := serve(t, `{"error":"forbidden"}`, http.StatusForbidden)
	t.Setenv(EnvURL, srv.URL)
	t.Setenv(EnvToken, "stale")

	_, err := Ask(context.Background(), "security")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "forbidden")
}

func TestAskNeedsAName(t *testing.T) {
	t.Setenv(EnvURL, "http://127.0.0.1:1")
	t.Setenv(EnvToken, "secret")

	_, err := Ask(context.Background(), " @ ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "which agent")
}

func TestFormatTellsAnUnknownAgentFromASilentOne(t *testing.T) {
	unknown := Format(Answer{Agent: "invented"})
	assert.Contains(t, unknown, "no agent called @invented")

	silent := Format(Answer{Agent: "security", Known: true})
	assert.Contains(t, silent, "has not said anything")

	at := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	spoke := Format(Answer{Agent: "security", Known: true, Said: []Entry{
		{Who: "security", Kind: "agent", At: at, Text: "Six findings, filed."},
	}})
	assert.Contains(t, spoke, "@security")
	assert.Contains(t, spoke, "Six findings, filed.")
	assert.Contains(t, spoke, "2026-09-10T09:00:00Z")
}
