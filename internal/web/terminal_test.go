package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dialTerminal opens the terminal socket against a live test server.
func dialTerminal(t *testing.T, srv *httptest.Server, token string) *websocket.Conn {
	t.Helper()

	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(context.Background(), ws+"/api/terminal?t="+url.QueryEscape(token), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

// live serves the whole mux, since a websocket needs a real connection to
// hijack and a recorder has none.
func live(t *testing.T, s *Server) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	t.Cleanup(s.closeTerminal)
	return srv
}

// awaitOutput reads frames until want appears.
func awaitOutput(t *testing.T, conn *websocket.Conn, want string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var seen strings.Builder
	for {
		_, data, err := conn.Read(ctx)
		require.NoError(t, err, "waiting for %q, saw %q", want, seen.String())
		seen.Write(data)
		if strings.Contains(seen.String(), want) {
			return seen.String()
		}
	}
}

func TestTerminalRunsInTheCheckout(t *testing.T) {
	s := newTestServer(t)
	srv := live(t, s)

	conn := dialTerminal(t, srv, s.token)
	ctx := context.Background()
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, []byte("echo ade-terminal-here\n")))
	assert.Contains(t, awaitOutput(t, conn, "ade-terminal-here"), "ade-terminal-here")
}

func TestTerminalNeedsTheSessionToken(t *testing.T) {
	s := newTestServer(t)
	srv := live(t, s)

	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	// A page on another origin can open a websocket without CORS ever being
	// consulted, so the token is the whole of the defence and has to be asked
	// for before the shell exists.
	_, resp, err := websocket.Dial(context.Background(), ws+"/api/terminal?t=wrong", nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestTerminalRefusesAnotherOrigin(t *testing.T) {
	s := newTestServer(t)
	srv := live(t, s)

	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	_, resp, err := websocket.Dial(context.Background(), ws+"/api/terminal?t="+url.QueryEscape(s.token), &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://evil.example"}},
	})
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestTerminalAllowsTheDevServersOrigin(t *testing.T) {
	s := newTestServer(t)
	srv := live(t, s)

	// In --dev the page is served by Vite on another port and proxied here, so
	// the origin it sends is loopback but never the same port.
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(context.Background(), ws+"/api/terminal?t="+url.QueryEscape(s.token), &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"http://127.0.0.1:5173"}},
	})
	require.NoError(t, err)
	_ = conn.CloseNow()
}

func TestTerminalReplaysToASecondConnection(t *testing.T) {
	s := newTestServer(t)
	srv := live(t, s)
	ctx := context.Background()

	first := dialTerminal(t, srv, s.token)
	require.NoError(t, first.Write(ctx, websocket.MessageBinary, []byte("echo left-behind\n")))
	awaitOutput(t, first, "left-behind")

	// Reloading the page must come back to the shell that is running.
	second := dialTerminal(t, srv, s.token)
	assert.Contains(t, awaitOutput(t, second, "left-behind"), "left-behind")
}

func TestTerminalResizes(t *testing.T) {
	s := newTestServer(t)
	srv := live(t, s)
	ctx := context.Background()

	conn := dialTerminal(t, srv, s.token)
	msg, err := json.Marshal(termControl{Type: "resize", Cols: 120, Rows: 45})
	require.NoError(t, err)
	require.NoError(t, conn.Write(ctx, websocket.MessageText, msg))

	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, []byte("stty size\n")))
	assert.Contains(t, awaitOutput(t, conn, "45 120"), "45 120")
}

func TestTerminalIgnoresRubbishControlFrames(t *testing.T) {
	s := newTestServer(t)
	srv := live(t, s)
	ctx := context.Background()

	conn := dialTerminal(t, srv, s.token)
	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte("{not json")))
	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(`{"type":"nonsense"}`)))

	// Still usable: a frame it does not understand is dropped, not fatal.
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, []byte("echo still-here\n")))
	assert.Contains(t, awaitOutput(t, conn, "still-here"), "still-here")
}

func TestEditorHasItsOwnVimSession(t *testing.T) {
	s := newTestServer(t)
	t.Cleanup(s.closeTerminal)

	// sh, not whatever this machine has: a container image may carry no editor.
	t.Setenv("EDITOR", "sh")
	editor, err := s.editor()
	require.NoError(t, err)
	again, err := s.editor()
	require.NoError(t, err)
	assert.Same(t, editor, again)
	assert.NotSame(t, s.terminal(), editor)
}

func TestNoEditorOnTheMachineIsRefusedRatherThanRetried(t *testing.T) {
	s := newTestServer(t)
	t.Cleanup(s.closeTerminal)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	editor, err := s.editor()
	require.Error(t, err)
	assert.Nil(t, editor)
	assert.Contains(t, err.Error(), "no editor")
}

func TestTheTerminalFollowsTheCheckout(t *testing.T) {
	s := newTestServer(t)
	t.Cleanup(s.closeTerminal)

	first := s.terminal()
	assert.Same(t, first, s.terminal(), "one shell per checkout, not one per call")

	// Relocating into a worktree has to leave the old shell behind: a prompt in
	// a directory the page is no longer showing is worse than a new one.
	moved := *s.space()
	moved.homeRoot = t.TempDir()
	s.mu.Lock()
	s.ws = &moved
	s.mu.Unlock()

	assert.NotSame(t, first, s.terminal())
	assert.Equal(t, moved.homeRoot, s.termRoot)
}

func TestEachPaneGetsItsOwnShell(t *testing.T) {
	s := newTestServer(t)
	srv := live(t, s)
	ctx := context.Background()

	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	dial := func(pane string) *websocket.Conn {
		conn, _, err := websocket.Dial(ctx, ws+"/api/terminal?t="+url.QueryEscape(s.token)+"&pane="+pane, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.CloseNow() })
		return conn
	}

	left, right := dial("pane-a"), dial("pane-b")
	require.NoError(t, left.Write(ctx, websocket.MessageBinary, []byte("echo only-on-the-left\n")))
	awaitOutput(t, left, "only-on-the-left")

	// The other pane is another shell, so it never saw that.
	require.NoError(t, right.Write(ctx, websocket.MessageBinary, []byte("echo on-the-right\n")))
	assert.NotContains(t, awaitOutput(t, right, "on-the-right"), "only-on-the-left")
}

func TestClosingAPaneKillsItsShell(t *testing.T) {
	s := newTestServer(t)
	live(t, s)

	key, err := paneKey("pane-a", "")
	require.NoError(t, err)
	_, err = s.pane(key)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/terminal/close?pane=pane-a", nil)
	req.Header.Set("X-Ade-Token", s.token)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	s.termMu.Lock()
	defer s.termMu.Unlock()
	assert.NotContains(t, s.terms, key)
}

func TestAPaneIDHasToBeOneOfOurs(t *testing.T) {
	// The id is a map key and comes from the browser, so a saved layout someone
	// has edited must not be able to name anything else.
	_, err := paneKey("../editor", "")
	assert.Error(t, err)

	key, err := paneKey("abc", "")
	require.NoError(t, err)
	assert.NotEqual(t, "editor", key)
}
