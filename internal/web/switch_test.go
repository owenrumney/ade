package web

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/sandbox"
)

// withRuntime stands a runtime in: moving to codex turns the sandbox on, and a
// machine without docker would fail the move rather than make it.
func withRuntime(t *testing.T) {
	was := detectRuntime
	t.Cleanup(func() { detectRuntime = was })
	detectRuntime = func(context.Context) (sandbox.Runtime, error) { return sandbox.Docker{}, nil }
}

func TestMovingProviderCarriesWhatWasSaid(t *testing.T) {
	s := newTestServer(t)
	withRuntime(t)
	s.say(ChatMessage{Kind: chat.KindYou, Body: "cache the cart"})
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "done, in cart.go"})

	rec := do(t, s, "POST", "/api/chat/switch", `{"provider":"codex"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	// The prose goes into the new session as the first message's context, so
	// the agent reads it the way it reads a highlight.
	var carried *chat.Context
	for _, msg := range s.chatStore().List(s.agent.Current()) {
		if msg.Context != nil {
			carried = msg.Context
		}
	}
	require.NotNil(t, carried, "nothing was carried over")
	assert.Contains(t, carried.Text, "cache the cart")
	assert.Contains(t, carried.Text, "done, in cart.go")
	assert.Contains(t, carried.From, "carried over from", "the break is named in the transcript")
	assert.Equal(t, "codex", s.agent.State().Provider)
}

func TestAProviderMoveWaitsForTheTurnToEnd(t *testing.T) {
	// A turn in flight has a tool call the other provider has no id for.
	s := newTestServer(t)
	s.say(ChatMessage{Kind: chat.KindYou, Body: "something"})
	_, ok := s.agent.begin(s.ctx, kindChat)
	require.True(t, ok)
	defer s.endRun(nil)

	rec := do(t, s, "POST", "/api/chat/switch", `{"provider":"codex"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "before moving providers")
}

func TestAnEmptyConversationCanSwitchProvider(t *testing.T) {
	s := newTestServer(t)
	withRuntime(t)
	rec := do(t, s, "POST", "/api/chat/switch", `{"provider":"codex"}`)
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, "codex", s.agent.State().Provider)
}

func TestSwitchingWithNoTurnCarriesNothingAndRunsNothing(t *testing.T) {
	// What `ade --agent codex` asks for on a checkout that already has a server.
	s := newTestServer(t)
	withRuntime(t)
	s.say(ChatMessage{Kind: chat.KindYou, Body: "cache the cart"})

	rec := do(t, s, "POST", "/api/chat/switch", `{"provider":"codex","noTurn":true}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Equal(t, "codex", s.agent.State().Provider)
	assert.False(t, s.agent.State().Running)
}

// Only an explicit --sandbox=false drops the container; the launcher never
// asks, so choosing a provider cannot move a conversation out of its box.
func TestOnlyAnExplicitRequestSwitchesWithoutTheADESandbox(t *testing.T) {
	s := newTestServer(t)
	s.agent.setSandbox(true)
	rec := do(t, s, "POST", "/api/chat/switch", `{"provider":"codex","noTurn":true,"sandbox":false}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Equal(t, "codex", s.agent.State().Provider)
	assert.False(t, s.agent.State().Sandbox)
}

func TestMovingNowhereIsRefused(t *testing.T) {
	s := newTestServer(t)
	s.say(ChatMessage{Kind: chat.KindYou, Body: "something"})
	rec := do(t, s, "POST", "/api/chat/switch", `{"provider":"claude"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "already there")
}

func TestOnlyAProviderTheRunnerCanDriveIsOffered(t *testing.T) {
	// Anything the runner has no driver for would fail after the conversation
	// was already reset, so it is refused before anything moves.
	s := newTestServer(t)
	s.say(ChatMessage{Kind: chat.KindYou, Body: "something"})
	rec := do(t, s, "POST", "/api/chat/switch", `{"provider":"gemini"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "claude or codex")
}

func TestThePiSpellingStillNamesCodex(t *testing.T) {
	s := newTestServer(t)
	withRuntime(t)
	s.say(ChatMessage{Kind: chat.KindYou, Body: "something"})
	rec := do(t, s, "POST", "/api/chat/switch", `{"provider":"pi","noTurn":true}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Equal(t, "codex", s.agent.State().Provider)
}
