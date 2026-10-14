package launcher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rows under a machine are read from that machine, so forgetting one here
// hides nothing: the request has to land over there.
func TestDeletingASessionOnAMachineGoesToThatMachine(t *testing.T) {
	var path, body string
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(raw)
		body = string(raw)
		_ = json.NewEncoder(w).Encode(map[string]int{"removed": 1})
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}

	rec := do(t, s, http.MethodPost, "/api/sessions/delete", `{"ids":["a1"],"machine":"linux"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "/api/sessions/delete", path)
	assert.Contains(t, body, "a1")
	assert.NotContains(t, body, "machine", "the far launcher is not asked to forward it on")
	assert.Contains(t, rec.Body.String(), "removed")
}

func TestArchivingASessionOnAMachineGoesToThatMachine(t *testing.T) {
	var body string
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(raw)
		body = string(raw)
		_ = json.NewEncoder(w).Encode(map[string]int{"changed": 1})
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}

	rec := do(t, s, http.MethodPost, "/api/archive", `{"id":"a1","archived":true,"machine":"linux"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, body, `"archived":true`)
}

// A machine that is not connected cannot be told, and saying so beats a
// request that quietly edits this machine's store instead.
func TestASessionVerbForAMachineThatIsNotConnectedIsRefused(t *testing.T) {
	s := newTestLauncher(t)

	rec := do(t, s, http.MethodPost, "/api/sessions/delete", `{"ids":["a1"],"machine":"linux"}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "not connected to linux")
}
