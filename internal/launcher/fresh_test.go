package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestTheFirstMessageIsHandedToTheNewSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "code", "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{filepath.Join(home, "code")}}))

	s := newTestLauncher(t)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		return "http://127.0.0.1:1234/?t=abc", nil
	}
	was := sendFirst
	var sent string
	sendFirst = func(_ context.Context, url, body string) error {
		sent = body
		assert.Contains(t, url, "t=abc", "the session's own token")
		return nil
	}
	t.Cleanup(func() { sendFirst = was })

	rec := do(t, s, http.MethodPost, "/api/open", `{"dir":"`+dir+`","fresh":true,"prompt":"fix the failing checks"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, sent, "fix the failing checks")
}

// An image pasted into the first message goes with it, and goes alone: the
// session takes a picture with no words, as its own chat route does.
func TestWhatWasPastedIntoTheFirstMessageGoesWithIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "code", "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{filepath.Join(home, "code")}}))

	s := newTestLauncher(t)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		return "http://127.0.0.1:1234/?t=abc", nil
	}
	was := sendFirst
	var sent string
	sendFirst = func(_ context.Context, _, body string) error {
		sent = body
		return nil
	}
	t.Cleanup(func() { sendFirst = was })

	rec := do(t, s, http.MethodPost, "/api/open",
		`{"dir":"`+dir+`","fresh":true,"files":[{"name":"shot.png","mediaType":"image/png","data":"AAAA"}]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, sent, "shot.png")
	assert.Contains(t, sent, "AAAA")
}

// An empty box is the normal case, and must not post an empty message the
// session would refuse.
func TestNoFirstMessageIsSentWhenTheBoxIsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "code", "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{filepath.Join(home, "code")}}))

	s := newTestLauncher(t)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		return "http://127.0.0.1:1234/?t=abc", nil
	}
	was := sendFirst
	called := false
	sendFirst = func(context.Context, string, string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { sendFirst = was })

	rec := do(t, s, http.MethodPost, "/api/open", `{"dir":"`+dir+`","fresh":true,"prompt":"   "}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.False(t, called, "nothing to say means nothing is sent")
}

// TestABriefLandsBeforeTheBuildIsAskedFor is the ordering the whole hand-off
// rests on: an agent given no brief invents one.
func TestABriefLandsBeforeTheBuildIsAskedFor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "code", "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{filepath.Join(home, "code")}}))

	s := newTestLauncher(t)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		return "http://127.0.0.1:1234/?t=abc", nil
	}
	was := askSession
	var calls []string
	var pushed string
	askSession = func(_ context.Context, _, path, body string) error {
		calls = append(calls, path)
		if path == "/api/design/push" {
			pushed = body
		}
		return nil
	}
	t.Cleanup(func() { askSession = was })

	rec := do(t, s, http.MethodPost, "/api/open",
		`{"dir":"`+dir+`","fresh":true,"brief":{"name":"cart.md","body":"# Cart\n","build":true}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, []string{"/api/design/push", "/api/design/go"}, calls)
	assert.Contains(t, pushed, "cart.md")
	assert.Contains(t, pushed, "# Cart")
}

// A document that never landed must not be built from.
func TestAFailedPushDoesNotStartTheBuild(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "code", "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{filepath.Join(home, "code")}}))

	s := newTestLauncher(t)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		return "http://127.0.0.1:1234/?t=abc", nil
	}
	was := askSession
	var calls []string
	askSession = func(_ context.Context, _, path, _ string) error {
		calls = append(calls, path)
		return errors.New("the session refused it")
	}
	t.Cleanup(func() { askSession = was })

	rec := do(t, s, http.MethodPost, "/api/open",
		`{"dir":"`+dir+`","fresh":true,"brief":{"name":"cart.md","body":"# Cart\n","build":true}}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Equal(t, []string{"/api/design/push"}, calls, "the build must not follow a brief that failed")
}

// A build that fails after the push leaves a session open, and the failure has
// to say where: it is the only address the caller will ever get for it.
func TestAFailedBuildStillSaysWhereTheSessionIs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "code", "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{filepath.Join(home, "code")}}))

	s := newTestLauncher(t)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		return "http://127.0.0.1:1234/?t=abc", nil
	}
	was := askSession
	askSession = func(_ context.Context, _, path, _ string) error {
		if path == "/api/design/go" {
			return errors.New("the agent is already running")
		}
		return nil
	}
	t.Cleanup(func() { askSession = was })

	rec := do(t, s, http.MethodPost, "/api/open",
		`{"dir":"`+dir+`","fresh":true,"brief":{"name":"cart.md","body":"# Cart\n","build":true}}`)
	require.Equal(t, http.StatusBadGateway, rec.Code)

	var got struct {
		URL   string `json:"url"`
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.NotEmpty(t, got.URL, "a session was opened, so the reply names it")
	assert.Contains(t, got.Error, "start the build")
}
