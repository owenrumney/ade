package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnEditorWebviewMayReadWhatItAskedFor(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/state?t="+s.token, nil)
	req.Header.Set("Origin", "ade-webview://0123abcd")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ade-webview://0123abcd", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, rec.Header().Values("Vary"), "Origin")
}

func TestAPageOnTheWebIsNotAnEditor(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/state?t="+s.token, nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	// The token already refused it; this says the browser is not told otherwise.
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestTheAllowanceIsNotAWayIn(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/state", nil)
	req.Header.Set("Origin", "ade-webview://0123abcd")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	// A webview with no token is still a stranger.
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAPreflightIsAnsweredWithoutAToken(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("OPTIONS", "/api/chat", nil)
	req.Header.Set("Origin", "ade-webview://0123abcd")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	// A preflight never carries one, so refusing it refuses every POST.
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), "X-Ade-Token")
}

func TestAPreflightFromAnywhereElseIsRefused(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("OPTIONS", "/api/chat", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAPreflightAllowsEverySettingsMethod(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("OPTIONS", "/api/settings", nil)
	req.Header.Set("Origin", "ade-webview://0123abcd")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	// The settings panel saves with PATCH and puts a secret with PUT; a method
	// missing here is a "Failed to fetch" with nothing in the server's log.
	allowed := rec.Header().Get("Access-Control-Allow-Methods")
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		assert.Contains(t, allowed, method)
	}
}

// ade's own app registers this scheme and serves its panels from it, so
// their fetches reach the session server.
func TestOnlyTheAppsOwnWebviewSchemeIsAllowed(t *testing.T) {
	assert.True(t, editorOrigin("ade-webview://0123abcd"))
	assert.False(t, editorOrigin("vscode-webview://0123abcd"))
	assert.False(t, editorOrigin("file://"))
	assert.False(t, editorOrigin("http://evil.example"))
	assert.False(t, editorOrigin(""))
}
