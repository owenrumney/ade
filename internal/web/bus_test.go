package web

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/busstate"
)

// The label is all this checkout speaks for now: the launcher turns the
// reports from every checkout into one status for the machine.
func TestTheLabelNamesThisCheckout(t *testing.T) {
	s := newTestServer(t)
	assert.Contains(t, s.busLabel(), "feature")
}

func TestAnAttachedPageIsTheEvidenceUntilTheEditorSaysOtherwise(t *testing.T) {
	s := newTestServer(t)
	assert.False(t, s.here(), "nothing attached and nothing reported")

	rec := do(t, s, "POST", "/api/bus/here", `{"focused":true}`)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"here":true}`, rec.Body.String())
	assert.True(t, s.here())
}

func TestAnUnfocusedEditorIsStillYouForAWhile(t *testing.T) {
	s := newTestServer(t)
	do(t, s, "POST", "/api/bus/here", `{"focused":false}`)
	assert.True(t, s.here(), "just switched away: reading a page is not being away")

	// Aged past the grace, with nothing attached to vouch for you either.
	s.mu.Lock()
	s.bus.focusAt = time.Now().Add(-2 * focusGrace)
	s.mu.Unlock()
	assert.False(t, s.here())
}

func TestFocusFlappingDoesNotRestartTheGrace(t *testing.T) {
	s := newTestServer(t)
	do(t, s, "POST", "/api/bus/here", `{"focused":false}`)

	s.mu.Lock()
	went := s.bus.focusAt
	s.mu.Unlock()

	do(t, s, "POST", "/api/bus/here", `{"focused":false}`)
	s.mu.RLock()
	assert.Equal(t, went, s.bus.focusAt, "the same answer twice is not a new one")
	s.mu.RUnlock()
}

func answer(id, dir, from string) busstate.Reply {
	return busstate.Reply{ID: id, Dir: dir, From: from, Kind: "answer",
		Question: "can you hear me?", Option: "yes", ForAgent: true}
}

// The launcher holds answers for every checkout on the machine, so a session
// must pick out its own and say each exactly once.
func TestAnAnswerIsSaidInTheCheckoutThatAskedAndNowhereElse(t *testing.T) {
	said := map[string]bool{}
	replies := []busstate.Reply{
		answer("msg_1", "/code/ade", "liam@infracost.io"),
		answer("msg_2", "/code/other", "vadim@infracost.io"),
	}

	got := unsaid(replies, "/code/ade", said)
	require.Len(t, got, 1)
	assert.Equal(t, "msg_1", got[0].ID)

	said["msg_1"] = true
	assert.Empty(t, unsaid(replies, "/code/ade", said), "an answer is news exactly once")
}

// You asked it from the Team view, so you get a notification. Putting it in
// the conversation would hand the agent an answer it never asked for.
func TestAnAnswerToYourOwnQuestionIsNotSaidInTheConversation(t *testing.T) {
	mine := answer("msg_1", "/code/ade", "liam@infracost.io")
	mine.ForAgent = false
	assert.Empty(t, unsaid([]busstate.Reply{mine}, "/code/ade", map[string]bool{}))
}

// A reply with no checkout belongs to nobody: the launcher could not say who
// asked, and guessing would put it in whichever session polled first.
func TestAnAnswerWithNoCheckoutIsSaidNowhere(t *testing.T) {
	assert.Empty(t, unsaid([]busstate.Reply{answer("msg_1", "", "liam@infracost.io")},
		"/code/ade", map[string]bool{}))
}

func TestWhatAnAnswerReadsLikeInTheConversation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply busstate.Reply
		want  []string
	}{
		{"a person answering", answer("m", "/d", "liam@infracost.io"),
			[]string{"liam@infracost.io", "can you hear me?", "yes"}},
		{"their agent standing in", busstate.Reply{Kind: "answer", From: "liam@infracost.io",
			Text: "probably", ByAgent: true},
			[]string{"their agent", "probably"}},
		{"a review verdict", busstate.Reply{Kind: "review", From: "vadim@infracost.io",
			PR: "https://github.com/infracost/infra/pull/9", Verdict: "approved", Text: "LGTM"},
			[]string{"vadim@infracost.io", "approved", "pull/9", "LGTM"}},
		{"nobody named", busstate.Reply{Kind: "answer", Option: "yes"},
			[]string{"somebody", "yes"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := replyLine(tc.reply)
			for _, want := range tc.want {
				assert.Contains(t, line, want)
			}
		})
	}
}
