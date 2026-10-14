package web

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/preview"
)

func TestPreviewSettings(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "PUT", "/api/preview", `{"url":"http://localhost:5173"}`)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	var got previewView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "http://localhost:5173", got.URL)
	assert.Contains(t, got.Path, ".git/ade/preview.json")

	rec = do(t, s, "PUT", "/api/preview", `{"url":"https://example.com"}`)
	assert.Equal(t, 400, rec.Code)
}

func TestUIWritesQueueAPreview(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		want bool
	}{
		{"typescript", "web/src/App.tsx", true},
		{"stylesheet", "web/src/styles/chrome.css", true},
		{"go", "internal/web/preview.go", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			s.beginRunTurn("change")
			s.noteEdits([]bridge.ToolUse{{Name: "Write", Input: json.RawMessage(`{"file_path":"` + tc.path + `"}`)}})
			s.settleTurn()
			s.mu.RLock()
			got := s.previewPending
			s.mu.RUnlock()
			assert.Equal(t, tc.want, got)
		})
	}
}

// A sandboxed run cannot reach the dev server, so the shot waits for a turn
// that can take one rather than being dropped on the floor.
func TestASandboxedRunDefersThePreviewRatherThanLosingIt(t *testing.T) {
	s := newTestServer(t)
	s.agent.setSandbox(true)
	s.mu.Lock()
	s.previewPending = true
	s.mu.Unlock()

	s.capturePreview()

	s.mu.RLock()
	defer s.mu.RUnlock()
	assert.True(t, s.previewPending, "still owed once the sandbox is off")
}

// The capture takes up to twenty seconds, which is long enough to start a
// different session behind it.
func TestAPreviewIsNotAttachedToTheSessionThatReplacedIt(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "PUT", "/api/preview", `{"url":"http://localhost:5173"}`)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	s.agent.resume("sess-one")
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "done"})

	wasCapture, wasReachable := capturePreview, previewReachable
	previewReachable = func(string) bool { return true }
	capturePreview = func(context.Context, string) ([]byte, error) {
		s.agent.resume("sess-two")
		return []byte("png"), nil
	}
	t.Cleanup(func() { capturePreview, previewReachable = wasCapture, wasReachable })
	s.mu.Lock()
	s.previewPending = true
	s.mu.Unlock()

	s.capturePreview()

	for _, msg := range s.chatStore().List("sess-one") {
		assert.Empty(t, msg.Images, "the shot belongs to the session that asked for it")
	}
	assert.Empty(t, s.chatStore().List("sess-two"))
}

func TestCapturePreviewAddsItToTheLastAgentReply(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "PUT", "/api/preview", `{"url":"http://localhost:5173"}`)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	s.say(ChatMessage{Kind: chat.KindYou, Body: "make it blue"})
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "done"})

	previous := capturePreview
	capturePreview = func(context.Context, string) ([]byte, error) { return []byte("png"), nil }
	wasReachable := previewReachable
	previewReachable = func(string) bool { return true }
	t.Cleanup(func() {
		capturePreview = previous
		previewReachable = wasReachable
	})
	s.mu.Lock()
	s.previewPending = true
	s.mu.Unlock()
	s.capturePreview()

	messages := s.conversation()
	require.Len(t, messages, 2)
	require.Len(t, messages[1].Images, 1)
	assert.Equal(t, "image/png", messages[1].Images[0].MediaType)
	assert.Equal(t, "cG5n", messages[1].Images[0].Data)
}

func TestARepositoryThatSaysNothingIsStillCaptured(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.say(ChatMessage{Kind: chat.KindYou, Body: "make it blue"})
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "done"})

	var asked string
	previous := capturePreview
	capturePreview = func(_ context.Context, url string) ([]byte, error) {
		asked = url
		return []byte("png"), nil
	}
	wasReachable := previewReachable
	previewReachable = func(string) bool { return true }
	t.Cleanup(func() {
		capturePreview = previous
		previewReachable = wasReachable
	})
	s.mu.Lock()
	s.previewPending = true
	s.mu.Unlock()
	s.capturePreview()

	assert.Equal(t, preview.DefaultURL, asked, "no preview.json means the default, not nothing")
	messages := s.conversation()
	require.Len(t, messages, 2)
	require.Len(t, messages[1].Images, 1)
}

func TestNothingIsCapturedWhenScreenshotsAreOffEverywhere(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	rec := do(t, s, "PUT", "/api/settings", `{"scanDirs":["~/code"],"previewOff":true}`)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "done"})

	previous := capturePreview
	capturePreview = func(context.Context, string) ([]byte, error) {
		t.Fatal("capture was asked for with screenshots off")
		return nil, nil
	}
	t.Cleanup(func() { capturePreview = previous })
	s.mu.Lock()
	s.previewPending = true
	s.mu.Unlock()
	s.capturePreview()

	require.Len(t, s.conversation(), 1)
	assert.Empty(t, s.conversation()[0].Images)
}
