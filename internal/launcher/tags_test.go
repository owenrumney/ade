package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/runlog"
	"github.com/owenrumney/ade/internal/sessions"
)

// recorded gives a fixture checkout a session of its own.
func recorded(t *testing.T, dir, id string) string {
	t.Helper()
	path := runlog.Path(sessions.GitDir(dir))
	require.NoError(t, runlog.Update(path, func(r *runlog.Record) { r.Session, r.Provider = id, "claude" }))
	return path
}

func listedSession(t *testing.T, s *Server, dir, id string) (sessions.Session, bool) {
	t.Helper()
	repos, err := s.repos()
	require.NoError(t, err)
	checkout, ok := sessions.FindCheckout(repos, dir)
	require.True(t, ok)
	for _, session := range checkout.Sessions {
		if session.ID == id {
			return session, true
		}
	}
	return sessions.Session{}, false
}

func TestTaggingASessionReplacesItsTagsAndTellsTheWindow(t *testing.T) {
	m := newFixture(t)
	dir := m.checkout(t, "repo", 0, false)
	path := recorded(t, dir, "sess-1")
	s := newLauncherOn(t, m)
	sent := listening(t, s)

	rec := do(t, s, http.MethodPost, "/api/session/tags", `{"id":"sess-1","tags":[" auth ","","auth","billing"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"tags":["auth","billing"]}`, rec.Body.String())
	assert.Contains(t, sent(), "event: repos")

	rec = do(t, s, http.MethodPost, "/api/session/tags", `{"id":"sess-1","tags":["later"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"later"}, runlog.Read(path).Tags, "the list is replaced, not added to")
	assert.Equal(t, "sess-1", runlog.Read(path).Session, "and the session it tags is untouched")

	listed, ok := listedSession(t, s, dir, "sess-1")
	require.True(t, ok)
	assert.Equal(t, []string{"later"}, listed.Tags)

	rec = do(t, s, http.MethodPost, "/api/session/tags", `{"id":"sess-1","tags":[]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Tags []string `json:"tags"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.NotNil(t, got.Tags, "no tags is an empty list")
	assert.Empty(t, got.Tags)
}

func TestOnlyAListedSessionIsTagged(t *testing.T) {
	m := newFixture(t)
	m.checkout(t, "repo", 0, false)
	s := newLauncherOn(t, m)

	for _, body := range []string{`{"id":"nobody","tags":["x"]}`, `{"id":"","tags":["x"]}`} {
		rec := do(t, s, http.MethodPost, "/api/session/tags", body)
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "accepted %s", body)
	}
}

func TestDeletingASessionRemovesItsRecord(t *testing.T) {
	m := newFixture(t)
	dir := m.checkout(t, "repo", 0, false)
	other := m.checkout(t, "other", 0, false)
	path := recorded(t, dir, "sess-1")
	kept := recorded(t, other, "sess-2")
	s := newLauncherOn(t, m)

	rec := do(t, s, http.MethodPost, "/api/sessions/delete", `{"ids":["sess-1","never-listed"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"removed":1}`, rec.Body.String())
	assert.NoFileExists(t, path)
	_, listed := listedSession(t, s, dir, "sess-1")
	assert.False(t, listed)

	_, err := os.Stat(kept)
	require.NoError(t, err, "a session nobody named keeps its record")

	rec = do(t, s, http.MethodPost, "/api/sessions/delete", `{"ids":[]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// listening opens an event stream and answers a func that hangs up and says what was sent.
func listening(t *testing.T, s *Server) func() string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/events?t="+s.token, nil)
	ctx, hangUp := context.WithCancel(req.Context())
	out := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.routes().ServeHTTP(out, req.WithContext(ctx))
	}()
	require.Eventually(t, func() bool { return s.hub.Clients() > 0 }, time.Second, 5*time.Millisecond)
	return func() string {
		hangUp()
		<-done
		return out.Body.String()
	}
}
