package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/session"
)

// runningAde stands in for a ade already serving this checkout: a health
// endpoint that satisfies session.Probe, and a record on disk pointing at it.
func runningAde(t *testing.T, handler http.Handler) (gitDir string, hits *[]string) {
	t.Helper()
	gitDir = t.TempDir()
	seen := []string{}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(session.Health{Ade: true, PID: os.Getpid(), Root: gitDir})
	})
	mux.Handle("/api/", recorder(&seen, handler))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	require.NoError(t, session.Write(session.Path(gitDir), session.Record{
		PID: os.Getpid(), URL: srv.URL + "/?t=tok", Root: gitDir,
	}))
	return gitDir, &seen
}

func recorder(seen *[]string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// Clicking "Open in Ade" on a checkout that already has a server used to hand
// back its URL with --pr dropped, landing you on your own branch with no
// Review tab. It has to ask that server to open the pull request.
func TestAgentFlagSwitchesTheProviderOnARunningAde(t *testing.T) {
	gitDir, hits := runningAde(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	assert.True(t, reuse(context.Background(), session.Path(gitDir), false, "", "codex", defaultModel, true, false))
	assert.Contains(t, *hits, "POST /api/chat/switch")
	assert.Contains(t, (*hits)[0], "/api/chat/switch")
}

func TestOpeningAPullRequestOnARunningAde(t *testing.T) {
	t.Run("the running server is asked to open it", func(t *testing.T) {
		gitDir, hits := runningAde(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		assert.True(t, openedPROn(context.Background(), gitDir, 7, false))
		assert.Contains(t, *hits, "POST /api/prs/7")
	})

	t.Run("a refusal falls through rather than starting a rival", func(t *testing.T) {
		gitDir, _ := runningAde(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// What the agent running mid-turn answers.
			w.WriteHeader(http.StatusConflict)
		}))

		assert.False(t, openedPROn(context.Background(), gitDir, 7, false),
			"reuse handles it from here, so the caller must not think this worked")
	})

	t.Run("no server running means start one", func(t *testing.T) {
		assert.False(t, openedPROn(context.Background(), t.TempDir(), 7, false))
	})

	t.Run("a stale record is not a running server", func(t *testing.T) {
		gitDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(gitDir, "ade"), 0o750))
		require.NoError(t, session.Write(session.Path(gitDir), session.Record{
			PID: os.Getpid(), URL: "http://127.0.0.1:1/?t=tok", Root: gitDir,
		}))
		assert.False(t, openedPROn(context.Background(), gitDir, 7, false))
	})
}
