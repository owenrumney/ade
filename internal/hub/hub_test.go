package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// write puts a record where Detect will look for it.
func write(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(envHome, home)
	require.NoError(t, os.WriteFile(filepath.Join(home, "server.json"), []byte(body), 0o600))
}

// hubAt is a stand-in mcp-hub answering /healthz as itself.
func hubAt(t *testing.T, pid int, isHub bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"mcpHub": isHub, "pid": pid})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDetectFindsARunningHub(t *testing.T) {
	srv := hubAt(t, os.Getpid(), true)
	write(t, fmt.Sprintf(`{"pid":%d,"url":%q}`, os.Getpid(), srv.URL))

	got := Detect(context.Background())
	assert.True(t, got.Running)
	assert.Equal(t, srv.URL, got.URL)
}

func TestDetectReportsNothingWorthOffering(t *testing.T) {
	t.Run("no record at all", func(t *testing.T) {
		t.Setenv(envHome, t.TempDir())
		assert.False(t, Detect(context.Background()).Running)
	})

	t.Run("record naming a dead process", func(t *testing.T) {
		srv := hubAt(t, os.Getpid(), true)
		// PID 1 exists but is not the hub, so the health answer disagrees with
		// the record and that is what catches it.
		write(t, fmt.Sprintf(`{"pid":1,"url":%q}`, srv.URL))
		assert.False(t, Detect(context.Background()).Running)
	})

	t.Run("something else took the port", func(t *testing.T) {
		// A live PID proves nothing on its own: PIDs are reused, and whatever
		// holds the port now may be any other server.
		srv := hubAt(t, os.Getpid(), false)
		write(t, fmt.Sprintf(`{"pid":%d,"url":%q}`, os.Getpid(), srv.URL))
		assert.False(t, Detect(context.Background()).Running)
	})

	t.Run("nothing listening", func(t *testing.T) {
		write(t, fmt.Sprintf(`{"pid":%d,"url":"http://127.0.0.1:1"}`, os.Getpid()))
		assert.False(t, Detect(context.Background()).Running)
	})

	t.Run("a record that is not usable", func(t *testing.T) {
		for name, body := range map[string]string{
			"nonsense": "not json",
			"no pid":   `{"url":"http://127.0.0.1:4867"}`,
			"no url":   `{"pid":42}`,
		} {
			t.Run(name, func(t *testing.T) {
				write(t, body)
				assert.False(t, Detect(context.Background()).Running)
			})
		}
	})

	t.Run("a record pointing off this machine", func(t *testing.T) {
		// The record is a file on disk and the URL becomes a link the browser
		// is offered. It has to stay local.
		write(t, fmt.Sprintf(`{"pid":%d,"url":"http://evil.example.com"}`, os.Getpid()))
		assert.False(t, Detect(context.Background()).Running)
	})
}
