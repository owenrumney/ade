package web

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/cleanse"
	"github.com/owenrumney/ade/internal/settings"
)

// longReply is prose the gate sends to the model, with a block it must keep.
var longReply = strings.Repeat("The retry loop resets its counter on success, which the old test never covered. ", 4) +
	"\n\n```sh\ngo test ./internal/retry/\n```"

const shortReply = "Counter resets on success. The old test missed it.\n\n```sh\ngo test ./internal/retry/\n```"

func TestALongReplyIsRewrittenShortBesideTheOriginal(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	calls := sharpenAnswers(t, func(opts bridge.OneShotOpts) (*bridge.Turn, error) {
		assert.Equal(t, cleanse.Model, opts.Model)
		assert.Equal(t, cleanse.Role(), opts.SystemPrompt)
		assert.Equal(t, []string{}, opts.Tools, "a cleanser with tools is an agent")
		assert.True(t, opts.NoMCP)
		assert.True(t, opts.NoSessionPersistence)
		assert.Contains(t, opts.Prompt, "resets its counter on success")
		return &bridge.Turn{Text: shortReply}, nil
	})

	s.writeEvent(kindChat, bridge.Event{Text: longReply})
	s.cleansing.Wait()

	msgs := messagesOfKind(s.conversation(), chat.KindAgent)
	require.Len(t, msgs, 1)
	assert.Equal(t, strings.TrimSpace(longReply), msgs[0].Body, "the body is the truth and is never rewritten")
	assert.Equal(t, shortReply, msgs[0].Short)
	assert.Contains(t, msgs[0].ShortHTML, "<code")
	assert.Equal(t, 1, *calls)
}

func TestANamedAgentsReplyIsCleansedAndAReviewsIsNot(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	calls := sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: shortReply}, nil
	})

	reply := s.say(ChatMessage{Kind: chat.KindAgent, Author: "architect", Run: runSubagent, Body: longReply, HTML: s.replyHTML(longReply)})
	s.cleanse(reply)
	s.cleansing.Wait()
	assert.Equal(t, 1, *calls)

	review := s.say(ChatMessage{Kind: chat.KindAgent, Author: "security", Run: runSubagent, Review: true, Body: longReply})
	s.cleanse(review)
	s.cleansing.Wait()
	assert.Equal(t, 1, *calls, "a review's prose is the argument for its findings")

	var shorts []string
	for _, msg := range messagesOfKind(s.conversation(), chat.KindAgent) {
		shorts = append(shorts, msg.Short)
	}
	assert.Equal(t, []string{shortReply, ""}, shorts)
}

func TestAShortReplyAndARunsOwnOutputNeverReachAModel(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	calls := sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: shortReply}, nil
	})

	s.writeEvent(kindChat, bridge.Event{Text: "Done. The counter resets on success now."})
	s.writeEvent(kindCommitMessage, bridge.Event{Text: longReply})
	s.writeEvent(kindQuiz, bridge.Event{Text: longReply})
	s.cleansing.Wait()
	assert.Zero(t, *calls)
	for _, msg := range messagesOfKind(s.conversation(), chat.KindAgent) {
		assert.Empty(t, msg.Short)
	}
}

func TestRewritesRunOneAtATime(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	var running atomic.Int32
	var overlapped atomic.Bool
	calls := sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		if running.Add(1) > 1 {
			overlapped.Store(true)
		}
		defer running.Add(-1)
		time.Sleep(20 * time.Millisecond)
		return &bridge.Turn{Text: shortReply}, nil
	})

	for range 3 {
		s.writeEvent(kindChat, bridge.Event{Text: longReply})
	}
	s.cleansing.Wait()
	assert.Equal(t, 3, *calls)
	assert.False(t, overlapped.Load(), "under a sandbox each rewrite is a cold slot a named agent needs")
}

func TestTheCleanseTurnedOffRewritesNothing(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Update(func(into *settings.Settings) error {
		into.CleanseOff = true
		return nil
	}))
	calls := sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: shortReply}, nil
	})

	s.writeEvent(kindChat, bridge.Event{Text: longReply})
	s.cleansing.Wait()
	assert.Zero(t, *calls)
}

func TestAFailedOrEmptyCleanseLeavesTheReplyAsItWas(t *testing.T) {
	for name, answer := range map[string]func(bridge.OneShotOpts) (*bridge.Turn, error){
		"a failure": func(bridge.OneShotOpts) (*bridge.Turn, error) { return nil, errors.New("claude is not logged in") },
		"nothing":   func(bridge.OneShotOpts) (*bridge.Turn, error) { return &bridge.Turn{Text: ""}, nil },
		"a rewrite that dropped the block": func(bridge.OneShotOpts) (*bridge.Turn, error) {
			return &bridge.Turn{Text: "Counter resets on success. Run the tests."}, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestServer(t)
			t.Setenv("HOME", t.TempDir())
			calls := sharpenAnswers(t, answer)

			s.writeEvent(kindChat, bridge.Event{Text: longReply})
			s.cleansing.Wait()

			msgs := messagesOfKind(s.conversation(), chat.KindAgent)
			require.Len(t, msgs, 1)
			assert.Equal(t, 1, *calls)
			assert.Equal(t, strings.TrimSpace(longReply), msgs[0].Body)
			assert.Empty(t, msgs[0].Short)
			assert.Empty(t, msgs[0].ShortHTML)
		})
	}
}

func TestTheShortFormKeepsWhatLandedOnTheReplyMeanwhile(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	picture := chat.Image{MediaType: "image/png", Data: "iVBORw0KGgo="}
	sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		s.attachImages(s.agent.Current(), []chat.Image{picture})
		return &bridge.Turn{Text: shortReply}, nil
	})

	s.writeEvent(kindChat, bridge.Event{Text: longReply})
	s.cleansing.Wait()

	msgs := messagesOfKind(s.conversation(), chat.KindAgent)
	require.Len(t, msgs, 1)
	assert.Equal(t, shortReply, msgs[0].Short)
	assert.Equal(t, []chat.Image{picture}, msgs[0].Images, "the cleanse wrote over a picture attached while it ran")
}
