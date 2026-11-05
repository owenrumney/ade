package launcher

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
)

// A checkout that has been worked in before reopens its last conversation
// when the id is empty. "New session" has to say it means a new one.
func TestANewSessionAsksForAFreshConversation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "code", "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{filepath.Join(home, "code")}}))

	s := newTestLauncher(t)
	var gotID string
	s.open = func(_ context.Context, _, _, _, id string) (string, error) {
		gotID = id
		return "http://127.0.0.1:1234/", nil
	}

	rec := do(t, s, http.MethodPost, "/api/open", `{"dir":"`+dir+`","id":"","fresh":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "none", gotID, "a new session starts a conversation")
}

func TestOpeningWithoutFreshStillReopensTheLastConversation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "code", "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{filepath.Join(home, "code")}}))

	s := newTestLauncher(t)
	var gotID string
	s.open = func(_ context.Context, _, _, _, id string) (string, error) {
		gotID = id
		return "http://127.0.0.1:1234/", nil
	}

	rec := do(t, s, http.MethodPost, "/api/open", `{"dir":"`+dir+`","id":""}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, gotID, "clicking a checkout is not asking for a new conversation")
}

// A terminal session's first message starts the agent's own interface on it.

func TestATerminalSessionsFirstMessageGoesToTheAgentsTerminal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "code", "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{filepath.Join(home, "code")}}))

	s := newTestLauncher(t)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		return "http://127.0.0.1:1234/?t=abc", nil
	}
	wasPrompt := sendPrompt
	var prompted string
	sendPrompt = func(_ context.Context, url, body string) error {
		prompted = body
		assert.Contains(t, url, "t=abc", "the session's own token")
		return nil
	}
	t.Cleanup(func() { sendPrompt = wasPrompt })

	rec := do(t, s, http.MethodPost, "/api/open",
		`{"dir":"`+dir+`","fresh":true,"terminal":true,"prompt":"  fix the failing checks "}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"prompt":"fix the failing checks"}`, prompted)
}

func TestATicketIsATerminalSessionsFirstMessageWhenNoneWasWritten(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "code", "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{filepath.Join(home, "code")}}))

	s := newTestLauncher(t)
	s.create = func(context.Context, string, sessions.Work) (string, string, error) {
		return "http://127.0.0.1:1234/?t=abc", dir + "-fix-623", nil
	}
	wasPrompt := sendPrompt
	var prompted []string
	sendPrompt = func(_ context.Context, _, body string) error {
		prompted = append(prompted, body)
		return nil
	}
	t.Cleanup(func() { sendPrompt = wasPrompt })

	rec := do(t, s, http.MethodPost, "/api/open", `{"dir":"`+dir+`","ticket":"FIX-623","terminal":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = do(t, s, http.MethodPost, "/api/open",
		`{"dir":"`+dir+`","ticket":"FIX-623","terminal":true,"prompt":"only the tests"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Len(t, prompted, 2)
	assert.Contains(t, prompted[0], "Start on ticket FIX-623.")
	assert.JSONEq(t, `{"prompt":"only the tests"}`, prompted[1], "what was written wins")
}
