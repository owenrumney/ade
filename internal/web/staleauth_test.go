package web

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestALoginThatRotatedRestartsTheAgent(t *testing.T) {
	// Another ade, or Claude Code itself, refreshing the same account rotates
	// the token this subprocess started with. Nothing it does can recover, so
	// asking again only fails again — until ade is restarted by hand, which is
	// what this saves.
	s := newTestServer(t)
	// A conversation this process started: named by current, with no session
	// to resume from yet. This is the one a plain drop would throw away.
	s.agent.current = "abc123"
	_, ok := s.agent.begin(s.ctx, kindChat)
	require.True(t, ok)

	s.endRun(errors.New("Failed to authenticate: OAuth session expired and could not be refreshed"))

	state := s.agent.State()
	assert.Contains(t, state.Error, "the agent was restarted", "the reply has to say what to do next")
	assert.Nil(t, s.agent.agent, "the subprocess is dropped, so the next turn reads the login again")
	assert.Equal(t, "abc123", s.agent.session,
		"the conversation carries over: a fresh one is named only by current, and would otherwise be lost")
}

func TestAnOrdinaryFailureKeepsTheAgent(t *testing.T) {
	// Dropping on every error would throw away the conversation's process for
	// a failed tool call.
	s := newTestServer(t)
	_, ok := s.agent.begin(s.ctx, kindChat)
	require.True(t, ok)

	s.endRun(errors.New("exit status 1"))
	assert.NotContains(t, s.agent.State().Error, "restarted")
}

func TestWhatCountsAsAStaleLogin(t *testing.T) {
	for _, reason := range []string{
		"Failed to authenticate: OAuth session expired and could not be refreshed",
		"oauth session expired",
		"failed to authenticate: token expired",
	} {
		assert.Truef(t, staleLogin(reason), "%q should restart the agent", reason)
	}
	for _, reason := range []string{
		"exit status 1",
		"the agent went quiet for 5m0s",
		"usage answered HTTP 429",
	} {
		assert.Falsef(t, staleLogin(reason), "%q should not", reason)
	}
}
