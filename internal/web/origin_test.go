package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dialFrom(t *testing.T, srv *httptest.Server, token, origin string) error {
	t.Helper()

	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(context.Background(), ws+"/api/terminal?t="+url.QueryEscape(token),
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}})
	if conn != nil {
		t.Cleanup(func() { _ = conn.CloseNow() })
	}
	return err
}

func TestTheTerminalTakesAnyLoopbackOrigin(t *testing.T) {
	s := newTestServer(t)
	srv := live(t, s)

	for _, origin := range []string{
		"http://127.0.0.1:41999",
		"http://localhost:41999",
		"http://[::1]:41999",
	} {
		t.Run(origin, func(t *testing.T) {
			require.NoError(t, dialFrom(t, srv, s.token, origin),
				"a pane opened from this origin would sit on reconnecting for ever")
		})
	}
}

func TestTheTerminalRefusesAnOriginOffThisMachine(t *testing.T) {
	s := newTestServer(t)
	srv := live(t, s)

	err := dialFrom(t, srv, s.token, "http://bigboy.local:41999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

// getFrom is the pane's own probe: the same URL over http, carrying no upgrade.
func getFrom(t *testing.T, srv *httptest.Server, token, origin string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/terminal?t="+url.QueryEscape(token), nil)
	require.NoError(t, err)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

// What the pane shows as the reason is this status and the first line of this body.
func TestARefusedOriginSaysWhyOverHTTP(t *testing.T) {
	s := newTestServer(t)
	res := getFrom(t, live(t, s), s.token, "http://bigboy.local:41999")

	assert.Equal(t, http.StatusForbidden, res.StatusCode)
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "may not open a terminal")
}

// 426 is the answer the probe must read as saying nothing about the refusal.
func TestAProbeCarryingNoUpgradeIsNotARefusal(t *testing.T) {
	s := newTestServer(t)
	res := getFrom(t, live(t, s), s.token, "")

	assert.Equal(t, http.StatusUpgradeRequired, res.StatusCode)
}
