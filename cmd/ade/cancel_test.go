package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/session"
)

// fakeSession is a session server: it answers the probe as the record's own,
// and the cancel with whatever the test hands it.
func fakeSession(t *testing.T, agent *session.AgentHealth, cancel http.HandlerFunc) (string, *int) {
	t.Helper()
	root := t.TempDir()
	asked := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_ = json.NewEncoder(w).Encode(session.Health{Ade: true, Root: root, PID: os.Getpid(), Agent: agent})
			return
		}
		require.Equal(t, "POST /api/cancel", r.Method+" "+r.URL.Path)
		require.Equal(t, "secret", r.Header.Get("X-Ade-Token"))
		asked++
		cancel(w, r)
	}))
	t.Cleanup(server.Close)

	path := filepath.Join(root, "server.json")
	require.NoError(t, session.Write(path, session.Record{PID: os.Getpid(), URL: server.URL + "/?t=secret", Root: root}))
	return path, &asked
}

func TestCancelWithNoRecordSaysSoAndAsksNothing(t *testing.T) {
	var out bytes.Buffer
	err := cancelHere(context.Background(), &out, filepath.Join(t.TempDir(), "server.json"))
	require.EqualError(t, err, "no ade is running for this repository")
	assert.Empty(t, out.String())
}

func TestCancelReportsWhatTheServerWasDoing(t *testing.T) {
	agent := &session.AgentHealth{Running: true, Queued: 2, SideRuns: []string{"security"}}
	path, asked := fakeSession(t, agent, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	var out bytes.Buffer
	require.NoError(t, cancelHere(context.Background(), &out, path))
	assert.Equal(t, 1, *asked)
	assert.Equal(t, filepath.Dir(path)+": cancelled the running turn, 2 queued message(s) dropped, 1 side run(s) stopped\n", out.String())
}

func TestCancellingAnIdleServerIsNotAFailure(t *testing.T) {
	path, _ := fakeSession(t, nil, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"nothing is running"}`))
	})

	var out bytes.Buffer
	require.NoError(t, cancelHere(context.Background(), &out, path))
	assert.Equal(t, filepath.Dir(path)+": nothing is running\n", out.String())
}

func TestCancelClearsTheRecordOfAServerThatHasGone(t *testing.T) {
	gone := exec.Command("true")
	require.NoError(t, gone.Run())
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()

	root := t.TempDir()
	path := filepath.Join(root, "server.json")
	require.NoError(t, session.Write(path, session.Record{PID: gone.Process.Pid, URL: server.URL + "/?t=secret", Root: root}))

	err := cancelHere(context.Background(), &bytes.Buffer{}, path)
	require.ErrorContains(t, err, root)
	_, still := session.Read(path)
	assert.False(t, still, "a record nothing answers for is cleared, as stop clears it")
}

// fakeLauncher lists two live sessions and refuses to cancel the one named.
func fakeLauncher(t *testing.T, refuse string) (string, *[]string) {
	t.Helper()
	var asked []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "secret", r.Header.Get("X-Ade-Token"))
		if r.Method == http.MethodGet {
			asked = append(asked, "GET "+r.URL.RequestURI())
			_, _ = w.Write([]byte(`{"repos":[{"name":"ade","checkouts":[
				{"path":"/code/ade","server":{"pid":1,"agent":{"running":true}}},
				{"path":"/code/ade-idle"},
				{"path":"/code/ade-fix","server":{"pid":2,"agent":{"running":true,"queued":1}}}]}]}`))
			return
		}
		var req struct{ Machine, Dir string }
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		asked = append(asked, r.Method+" "+r.URL.Path+" "+req.Machine+" "+req.Dir)
		if req.Dir == refuse {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"connection refused"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	record := filepath.Join(t.TempDir(), "launcher.json")
	require.NoError(t, session.Write(record, session.Record{PID: os.Getpid(), URL: server.URL + "/?t=secret"}))
	return record, &asked
}

func TestCancelAllCarriesOnPastASessionThatRefuses(t *testing.T) {
	record, asked := fakeLauncher(t, "/code/ade")

	var out bytes.Buffer
	err := cancelEvery(context.Background(), &out, record, "")
	require.EqualError(t, err, "1 of 2 sessions were not cancelled")
	assert.Equal(t, []string{
		"GET /api/graph",
		"POST /api/graph/cancel  /code/ade",
		"POST /api/graph/cancel  /code/ade-fix",
	}, *asked)
	assert.Equal(t, "/code/ade: not cancelled: connection refused\n"+
		"/code/ade-fix: cancelled the running turn, 1 queued message(s) dropped\n", out.String())
}

func TestCancelOnAHostNamesTheMachineToTheLauncher(t *testing.T) {
	record, asked := fakeLauncher(t, "")

	var out bytes.Buffer
	require.NoError(t, cancelEvery(context.Background(), &out, record, "pi5.local"))
	assert.Equal(t, []string{
		"GET /api/graph?machine=pi5.local",
		"POST /api/graph/cancel pi5.local /code/ade",
		"POST /api/graph/cancel pi5.local /code/ade-fix",
	}, *asked)
}

func TestCancelAllWithoutALauncherSaysWhatToDoInstead(t *testing.T) {
	err := cancelEvery(context.Background(), &bytes.Buffer{}, filepath.Join(t.TempDir(), "launcher.json"), "")
	require.ErrorContains(t, err, "launcher is not running")
}
