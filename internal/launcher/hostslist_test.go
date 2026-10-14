package launcher

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

func TestThisMachineLeadsTheHostList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Save(settings.Settings{Hosts: []settings.Host{{Name: "linux"}}}))
	s, err := New(0, "test")
	require.NoError(t, err)

	rec := do(t, s, http.MethodGet, "/api/hosts", "")
	var got struct {
		Hosts []hostView `json:"hosts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Hosts, 2)
	assert.True(t, got.Hosts[0].Local)
	assert.True(t, got.Hosts[0].Connected, "this machine is always up")
	assert.Equal(t, "linux", got.Hosts[1].Name)
	assert.False(t, got.Hosts[1].Local)
}

func TestTheHostListSaysWhatEachMachineIsDoing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Save(settings.Settings{Hosts: []settings.Host{{Name: "linux"}, {Name: "gpu"}}}))
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/serving" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]string{{"id": "a", "dir": "/srv/ade/api"}, {"id": "b", "dir": "/srv/ade/web"}})
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}
	s.noteHost("gpu", errors.New("ssh: connect to host gpu port 22: No route to host"))

	rec := do(t, s, http.MethodGet, "/api/hosts", "")
	var got struct {
		Hosts []hostView `json:"hosts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Hosts, 3)
	assert.Equal(t, hostConnected, got.Hosts[0].State)
	assert.Equal(t, hostConnected, got.Hosts[1].State)
	assert.Equal(t, 2, got.Hosts[1].Sessions, "the far machine's sessions are counted against it")
	assert.Equal(t, hostOff, got.Hosts[2].State)
	assert.Contains(t, got.Hosts[2].Error, "No route to host")

	s.noteHost("gpu", nil)
	rec = do(t, s, http.MethodGet, "/api/hosts", "")
	assert.NotContains(t, rec.Body.String(), "No route", "a connect that worked clears the fault")
}

func TestStoppingAWorkerNeedsItConnected(t *testing.T) {
	s := newTestLauncher(t)
	rec := do(t, s, http.MethodPost, "/api/worker/stop", `{"host":"linux"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "not connected")
}

func TestSyncingCredentialsNeedsTheWorkerConnected(t *testing.T) {
	s := newTestLauncher(t)
	rec := do(t, s, http.MethodPost, "/api/worker/credentials", `{"host":"linux"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "not connected")
}

func TestAFreshSessionIsProxiedUnderTheIDItAnnounces(t *testing.T) {
	s := newTestLauncher(t)
	s.served["pending-abc"] = Serving{ID: "pending-abc", Dir: "/srv/ade/x/api", Port: 4111}
	assert.Equal(t, "pending-abc", s.announcedFor("/srv/ade/x/api"),
		"a session with no id of its own is still addressable")
	was := announcedWait
	announcedWait = 10 * time.Millisecond
	defer func() { announcedWait = was }()
	assert.Empty(t, s.announcedFor("/srv/ade/x/other"))
}

func TestUpdatingAWorkerNeedsItConnected(t *testing.T) {
	s := newTestLauncher(t)
	rec := do(t, s, http.MethodPost, "/api/worker/update", `{"host":"linux"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "not connected")
}
