package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/settings"
)

// heard records what a stand-in launcher was told, so a test can watch for a
// ping without a real one running.
type heard struct {
	mu   sync.Mutex
	dirs []string
}

func (h *heard) seen() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.dirs...)
}

// listening puts a stand-in launcher where launcherURL will find it.
func listening(t *testing.T, s *Server) *heard {
	t.Helper()
	got := &heard{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Dir string `json:"dir"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		got.mu.Lock()
		got.dirs = append(got.dirs, req.Dir)
		got.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	writeLauncherURL(t, srv.URL+"/?t=token")
	s.noLauncher = false
	return got
}

// writeLauncherURL records a launcher at url, as a running one would.
func writeLauncherURL(t *testing.T, url string) {
	t.Helper()
	record, err := settings.RecordPath()
	require.NoError(t, err)
	require.NoError(t, session.Write(record, session.Record{
		PID: os.Getpid(), URL: url, Root: t.TempDir(),
	}))
}

// The session holds no wait of its own: it says it went quiet and the
// launcher, which outlives it, decides when to read.
func TestAnIdleSessionTellsTheLauncherItWentQuiet(t *testing.T) {
	s := newTestServer(t)
	got := listening(t, s)

	s.drain()
	s.settle(closeWait)

	assert.Equal(t, []string{s.startRoot}, got.seen())
}

// Exit is the last chance to have this session's prose distilled, and the
// session is the one process that will not be here to do it.
func TestLeavingTellsTheLauncherToo(t *testing.T) {
	s := newTestServer(t)
	got := listening(t, s)

	rec := do(t, s, http.MethodPost, "/api/exit", `{}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	s.settle(closeWait)

	assert.Equal(t, []string{s.startRoot}, got.seen())
}

func TestASessionWithNoLauncherSaysNothing(t *testing.T) {
	s := newTestServer(t)
	got := listening(t, s)
	s.noLauncher = true

	s.drain()
	s.settle(closeWait)

	assert.Empty(t, got.seen())
}

// A launcher that is not there, or refuses, must never fail a turn or hold up
// the door.
func TestALauncherThatIsNotThereIsNotAnError(t *testing.T) {
	s := newTestServer(t)
	s.noLauncher = false
	writeLauncherURL(t, "http://127.0.0.1:1/?t=token")

	s.sayQuiet(context.Background())
}
