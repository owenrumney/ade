package web

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/settings"
)

func TestBtwIsReadOffTheMessageAndNothingElse(t *testing.T) {
	tests := []struct {
		message  string
		question string
		ok       bool
	}{
		{"/btw why is this slow?", "why is this slow?", true},
		{"/btw\nwhat about the cache", "what about the cache", true},
		{"/btw", "", true},
		{"/btwx hello", "", false},
		{"/compact", "", false},
		{"by the way /btw", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			question, ok := btwQuestion(tt.message)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.question, question)
		})
	}
}

func TestABtwWithNothingToForkIsRefusedNotQueued(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/chat", `{"message":"/btw why?"}`)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Equal(t, 0, s.stacked(), "a refused aside must not wait behind the turn")
	assert.Empty(t, s.sideRuns())

	s.agent.current = "sess-1"
	rec = do(t, s, "POST", "/api/chat", `{"message":"/btw"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "a /btw with no question asks for nothing")
}

func TestABtwForksTheConversationAndKeepsTheAnswerFromTheMainAgent(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	// Off, or the rewrite of the aside's answer runs the same fake claude and
	// writes over the arguments this test is here to read.
	require.NoError(t, settings.Update(func(into *settings.Settings) error {
		into.CleanseOff = true
		return nil
	}))
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	bin := filepath.Join(dir, "claude")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" > %q
echo '{"type":"system","subtype":"init","session_id":"fork-1"}'
echo '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"the cache is cold"}]},"session_id":"fork-1"}'
echo '{"type":"result","result":"the cache is cold","session_id":"fork-1"}'
`, argsFile)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o700))
	s.agent.bin, s.agent.provider, s.agent.current = bin, "claude", "sess-1"
	_, ok := s.agent.begin(context.Background(), kindChat)
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/chat", `{"message":"/btw why is it slow?"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"aside":true`)
	assert.Equal(t, 0, s.stacked(), "an aside runs now, not after the turn")

	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 1)
	assert.Equal(t, "/btw why is it slow?", yours[0].Body)

	assert.Eventually(t, func() bool {
		for _, msg := range messagesOfKind(s.conversation(), chat.KindAgent) {
			if msg.Author == asideName && msg.Body == "the cache is cold" {
				return true
			}
		}
		return false
	}, 5e9, 1e7)
	assert.Eventually(t, func() bool { return len(s.sideRuns()) == 0 }, 5e9, 1e7)

	args, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Contains(t, string(args), "--resume sess-1 --fork-session")
	assert.Contains(t, string(args), "--tools Read Grep Glob")
	assert.NotContains(t, string(args), "Edit")
	assert.Empty(t, s.heardSoFar(), "the answer is yours; the main agent never hears it")
	for _, msg := range s.conversation() {
		assert.False(t, strings.Contains(msg.Body, "Since you last spoke"), "nothing relayed into the turn")
	}
}
