package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/settings"
)

// A checkout opened on its own still comes up beside the other sessions: the
// rail is on the launcher's origin and nowhere else.
func TestTheAddressOpenedIsThisSessionOnTheLauncher(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)

	// No session id yet is a checkout with no conversation to address, and the
	// checkout's own URL is the only one there is.
	s.agent.session, s.agent.current = "", ""
	at, _ := s.throughLauncher(context.Background())
	assert.Empty(t, at)

	// Asked not to, the launcher is left alone whatever else is true.
	s.agent.current = "11111111-2222-3333-4444-555555555555"
	s.noLauncher = true
	at, watched := s.throughLauncher(context.Background())
	assert.Empty(t, at)
	assert.False(t, watched, "nothing was asked, so nothing is watching")
}

// The rail is the announced set, so a restarted ade says so itself. The claim
// is checked by the launcher, so its shape is part of the bargain.
func TestAnAdeAnnouncesWhatItIsServingToTheLauncher(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	type claim struct {
		ID   string `json:"id"`
		Dir  string `json:"dir"`
		Port int    `json:"port"`
	}
	asked := make(chan *http.Request, 1)
	said := make(chan claim, 1)
	launcher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body claim
		_ = json.NewDecoder(r.Body).Decode(&body)
		asked <- r
		said <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer launcher.Close()

	record, err := settings.RecordPath()
	require.NoError(t, err)
	require.NoError(t, session.Write(record, session.Record{
		PID: os.Getpid(), URL: launcher.URL + "/?t=launcher-token", Root: t.TempDir(),
	}))

	s := &Server{listenPort: "4321", startRoot: "/repo"}
	require.NoError(t, s.announceServing(context.Background(), "abc"))

	req := <-asked
	assert.Equal(t, http.MethodPost, req.Method)
	assert.Equal(t, "/api/serving", req.URL.Path)
	assert.Equal(t, "launcher-token", req.Header.Get("X-Ade-Token"))
	assert.Equal(t, claim{ID: "abc", Dir: "/repo", Port: 4321}, <-said)

	// Nothing is claimed when there is no session to name.
	require.Error(t, s.announceServing(context.Background(), ""))
	assert.Empty(t, asked)
}

// A refusal used to be read and dropped, which left a session off the rail
// with nothing said anywhere about why. See `new session.md`.
func TestARefusedAnnouncementIsReportedWithTheLaunchersReason(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	launcher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"no ade is recorded at /w/ade/server.json"}`))
	}))
	defer launcher.Close()

	record, err := settings.RecordPath()
	require.NoError(t, err)
	require.NoError(t, session.Write(record, session.Record{
		PID: os.Getpid(), URL: launcher.URL + "/?t=launcher-token", Root: t.TempDir(),
	}))

	s := &Server{listenPort: "4321", startRoot: "/repo"}
	err = s.announceServing(context.Background(), "abc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no ade is recorded at /w/ade/server.json")
}

// The record this claim is checked against is written by another goroutine,
// so the first try may be early. One dropped POST is not the end of it.
func TestAnAnnouncementIsSaidAgainUntilTheLauncherTakesIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var tries atomic.Int32
	launcher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if tries.Add(1) < 3 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer launcher.Close()

	record, err := settings.RecordPath()
	require.NoError(t, err)
	require.NoError(t, session.Write(record, session.Record{
		PID: os.Getpid(), URL: launcher.URL + "/?t=launcher-token", Root: t.TempDir(),
	}))

	s := &Server{listenPort: "4321", startRoot: "/repo"}
	s.keepAnnouncing(context.Background(), "abc")
	assert.Equal(t, int32(3), tries.Load())
}

// The launcher's list names each worker launcher with its token in the URL.
// The page picks a machine by name and posts nowhere else, so it is given the
// name and not the credential.
func TestTheMachineListKeepsTheLauncherTokensOffThePage(t *testing.T) {
	s := newTestServer(t)
	stubLauncher(t, func(context.Context, string, string, string, time.Duration) ([]byte, error) {
		return []byte(`{"hosts":[{"name":"linux","label":"Build worker","connected":true,` +
			`"url":"http://127.0.0.1:29233/?t=SECRET-WORKER-TOKEN","state":"connected","sessions":2}]}`), nil
	})

	rec := do(t, s, "GET", "/api/hosts", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "SECRET-WORKER-TOKEN")
	assert.NotContains(t, rec.Body.String(), "29233")

	var got struct {
		Hosts []hostChoice `json:"hosts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Hosts, 1)
	assert.Equal(t, "linux", got.Hosts[0].Name)
	assert.Equal(t, "Build worker", got.Hosts[0].Label)
	assert.True(t, got.Hosts[0].Connected)
	assert.Equal(t, 2, got.Hosts[0].Sessions)
}

// No launcher is not an error: a session with none can still build here.
func TestTheMachineListIsEmptyWithoutALauncher(t *testing.T) {
	s := newTestServer(t)
	stubLauncher(t, func(context.Context, string, string, string, time.Duration) ([]byte, error) {
		return nil, errors.New("no launcher is running on this machine")
	})

	rec := do(t, s, "GET", "/api/hosts", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"hosts":[]}`, rec.Body.String())
}

// A checkout opened from a terminal has no agent session until the first turn,
// and a rail keyed on session ids would have no tile for it at all.
func TestTheRailIDIsProvisionalUntilTheAgentNamesASession(t *testing.T) {
	s := newTestServer(t)
	s.agent.session, s.agent.current = "", ""

	assert.Equal(t, s.pendingID, s.railID())
	assert.NotEmpty(t, s.pendingID)
	assert.Contains(t, s.pendingID, "pending-")

	s.agent.current = "11111111-2222-3333-4444-555555555555"
	assert.Equal(t, s.agent.current, s.railID(), "the agent's own id wins once there is one")
}

// `ade --session <id>` is how the launcher opens a tile, and the tile of a
// session not yet named carries the provisional id back here.
func TestResumingTheIDThisServerIsAlreadyOnIsANoOp(t *testing.T) {
	s := newTestServer(t)
	s.agent.session, s.agent.current = "", ""

	body, err := json.Marshal(map[string]string{"id": s.pendingID, "provider": "claude"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/chat/resume", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleChatResume(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "asked for the session it is already serving")
	assert.JSONEq(t, `{"session":"`+s.pendingID+`"}`, rec.Body.String())
}

// A session on a worker records the container's own port, which nothing on the
// host it runs on can reach. The proxied address is what the editor there has.
func TestTheRecordCarriesTheProxiedAddressWhenThereIsOne(t *testing.T) {
	git := t.TempDir()
	s := &Server{startGitDir: git, startRoot: t.TempDir(),
		direct: "http://127.0.0.1:24590/?t=direct", startedAt: time.Now()}

	s.record("")
	rec, ok := session.Read(session.Path(git))
	require.True(t, ok)
	assert.Equal(t, "http://127.0.0.1:24590/?t=direct", rec.URL)
	assert.Empty(t, rec.Proxy, "a session with no launcher has only its own address")

	s.record("http://127.0.0.1:36448/s/abc/?t=worker")
	rec, ok = session.Read(session.Path(git))
	require.True(t, ok)
	assert.Equal(t, "http://127.0.0.1:24590/?t=direct", rec.URL, "the direct address is kept")
	assert.Equal(t, "http://127.0.0.1:36448/s/abc/?t=worker", rec.Proxy)
}

// A new conversation changes the session id, which is in the path the launcher
// proxies this server at. A reader on another machine has nothing else to
// reach it by, so the record has to be written again.
func TestTheProxiedAddressFollowsANewSession(t *testing.T) {
	git := t.TempDir()
	s := &Server{startGitDir: git, startRoot: t.TempDir(),
		direct: "http://127.0.0.1:24590/?t=direct", startedAt: time.Now()}

	s.record("http://127.0.0.1:36448/s/old/?t=worker")
	rec, ok := session.Read(session.Path(git))
	require.True(t, ok)
	require.Contains(t, rec.Proxy, "/s/old/")

	s.record("http://127.0.0.1:36448/s/new/?t=worker")
	rec, ok = session.Read(session.Path(git))
	require.True(t, ok)
	assert.Contains(t, rec.Proxy, "/s/new/")
	assert.Equal(t, "http://127.0.0.1:24590/?t=direct", rec.URL, "its own address is unchanged")
}
