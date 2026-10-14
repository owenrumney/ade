package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/ghpr"
)

func TestConvertingSomethingThatIsNotThere(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/threads/nope/convert", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWhatCannotBeConverted(t *testing.T) {
	s := newTestServer(t)
	now := time.Now()

	tests := []struct {
		name   string
		thread comments.Thread
		want   string
	}{
		{
			// It came from the pull request. Posting it back would be ade
			// talking to a reviewer about their own comment.
			"imported from the pull request",
			comments.Thread{File: "a.go", Side: "right", Line: 3, ExternalID: "99"},
			"came from the pull request",
		},
		{
			"already sent",
			comments.Thread{File: "a.go", Side: "right", Line: 3, SentAt: &now},
			"already been sent",
		},
		{
			"about the change as a whole",
			comments.Thread{Scope: comments.ScopeRepo},
			"no line to attach to",
		},
		{
			"the line it was about has gone",
			comments.Thread{File: "a.go", Side: "right", Line: 3, Orphaned: true},
			"gone from the diff",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.thread.Comments = []comments.Comment{{Author: "agent", Body: "something"}}
			thread, err := s.space().store.Add(tc.thread)
			require.NoError(t, err)
			if tc.thread.SentAt != nil {
				require.NoError(t, s.space().store.MarkSent([]string{thread.ID}, *tc.thread.SentAt))
			}

			rec := do(t, s, "POST", "/api/threads/"+thread.ID+"/convert", "")
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.want)
		})
	}
}

func TestSendingNeedsSomethingYouHaveRead(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	_, err := s.space().store.Add(comments.Thread{
		File: "a.go", Side: "right", Line: 3,
		Comments: []comments.Comment{{Author: "agent", Body: "terse finding"}},
	})
	require.NoError(t, err)

	// Converted is what "you have read it" means. A finding with no rewritten
	// text has never been on screen as something that would be posted.
	rec := do(t, s, "POST", "/api/github/send", `{"confirm":7}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "nothing has been converted")
}

func TestSendingHasToNameThePullRequest(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})

	for _, body := range []string{`{}`, `{"confirm":0}`, `{"confirm":8}`} {
		rec := do(t, s, "POST", "/api/github/send", body)
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "accepted %s", body)
		assert.Contains(t, rec.Body.String(), "has to name it")
	}
}

func TestSomethingAlreadySentIsNotOfferedAgain(t *testing.T) {
	s := newTestServer(t)
	store := s.space().store
	thread, err := store.Add(comments.Thread{
		File: "a.go", Side: "right", Line: 3,
		Comments: []comments.Comment{{Author: "agent", Body: "finding"}},
	})
	require.NoError(t, err)
	_, err = store.SetOutgoing(thread.ID, "Could we reset the counter here?")
	require.NoError(t, err)
	require.Len(t, store.Sendable(), 1)

	require.NoError(t, store.MarkSent([]string{thread.ID}, time.Now()))

	// Posting it twice is not something you can take back either.
	assert.Empty(t, store.Sendable())
}

func TestTheRewrittenTextCanBeChangedBeforeItGoes(t *testing.T) {
	s := newTestServer(t)
	thread, err := s.space().store.Add(comments.Thread{
		File: "a.go", Side: "right", Line: 3,
		Comments: []comments.Comment{{Author: "agent", Body: "finding"}},
	})
	require.NoError(t, err)
	_, err = s.space().store.SetOutgoing(thread.ID, "the agent's wording")
	require.NoError(t, err)

	// The agent drafted it; the name on it is yours.
	rec := do(t, s, "PUT", "/api/threads/"+thread.ID+"/outgoing", `{"text":"my wording"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	got, err := s.space().store.Get(thread.ID)
	require.NoError(t, err)
	assert.Equal(t, "my wording", got.Outgoing)
}

func TestSendingOneNamesTheComment(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})

	converted, err := s.space().store.Add(comments.Thread{
		File: "a.go", Side: "right", Line: 3,
		Comments: []comments.Comment{{Author: "agent", Body: "finding"}},
	})
	require.NoError(t, err)
	_, err = s.space().store.SetOutgoing(converted.ID, "Could we reset the counter?")
	require.NoError(t, err)

	// A thread that was never converted has never been on screen as something
	// that would be posted, so naming it is not enough.
	raw, err := s.space().store.Add(comments.Thread{
		File: "b.go", Side: "right", Line: 9,
		Comments: []comments.Comment{{Author: "agent", Body: "other finding"}},
	})
	require.NoError(t, err)

	rec := do(t, s, "POST", "/api/github/send", `{"confirm":7,"thread":"`+raw.ID+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "has not been converted")

	// And one that does not exist at all.
	rec = do(t, s, "POST", "/api/github/send", `{"confirm":7,"thread":"nope"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// An orphaned thread used to convert, show "will be sent to GitHub", offer a
// Send button, and then be refused with a message blaming the conversion that
// had in fact happened. Sendable drops it; notSendable now agrees, and says why.
func TestAConvertedCommentThatDriftedOffTheDiffSaysSo(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})

	thread, err := s.space().store.Add(comments.Thread{
		File: "a.go", Side: "right", Line: 3, Orphaned: true,
		Comments: []comments.Comment{{Author: "agent", Body: "finding"}},
	})
	require.NoError(t, err)
	_, err = s.space().store.SetOutgoing(thread.ID, "Could we reset the counter?")
	require.NoError(t, err)

	rec := do(t, s, "POST", "/api/github/send", `{"confirm":7,"thread":"`+thread.ID+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "gone from the diff")
	assert.NotContains(t, rec.Body.String(), "not been converted")
}

func TestOnlyThreadPicksTheOne(t *testing.T) {
	pending := []comments.Thread{{ID: "a", Outgoing: "one"}, {ID: "b", Outgoing: "two"}}

	got := onlyThread(pending, "b")
	require.Len(t, got, 1)
	assert.Equal(t, "b", got[0].ID)

	assert.Empty(t, onlyThread(pending, "c"))
	assert.Empty(t, onlyThread(nil, "a"))
}

func TestReadyToSendIsOnlyWhatWasConverted(t *testing.T) {
	ready := readyToSend([]comments.Thread{
		{ID: "a", Outgoing: "rewritten"},
		{ID: "b"},
		{ID: "c", Outgoing: "also rewritten"},
	})

	require.Len(t, ready, 2)
	assert.Equal(t, "a", ready[0].ID)
	assert.Equal(t, "c", ready[1].ID)
}

func TestConvertRunsBesideARunningAgentOnTheSmallModel(t *testing.T) {
	s := newTestServer(t)
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")
	s.agent.switchModel("opus")
	thread, err := s.space().store.Add(comments.Thread{
		File: "a.go", Side: "right", Line: 3,
		Comments: []comments.Comment{{Author: "agent", Body: "finding"}},
	})
	require.NoError(t, err)

	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	// Pressing convert while the agent is busy does not wait for it: a
	// rewrite is a second on the small model, in a slot of its own.
	rec := do(t, s, "POST", "/api/threads/"+thread.ID+"/convert", "")
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "queued")
	assert.Zero(t, s.stacked())

	// No claude to run it, so it fails fast — under its own kind, so the
	// page folds it, and never as the main agent's failure.
	assert.Eventually(t, func() bool {
		for _, msg := range messagesOfKind(s.conversation(), chat.KindNote) {
			if msg.Run == kindConvert && msg.Failed {
				return true
			}
		}
		return false
	}, 5e9, 1e7)
	assert.True(t, s.agent.State().Running, "the review was never interrupted")
	assert.Empty(t, s.agent.State().Error)
	assert.Eventually(t, func() bool { return len(s.sideRuns()) == 0 }, 5e9, 1e7)
}

func TestAConvertSavedByAnOlderAdeDoesNotWedgeTheAgent(t *testing.T) {
	s := newTestServer(t)
	s.stack(queued{kind: kindConvert, thread: "gone"})

	s.drain()

	// It runs beside the agent now, or not at all for a comment that is not
	// there; either way the runner is not held.
	assert.Zero(t, s.stacked())
	assert.False(t, s.agent.State().Running)
	assert.Empty(t, s.sideRuns())
}

func TestDroppingAQueuedPromptTakesItsMessageToo(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n")
	s.setDoc("plan.md")

	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	require.Equal(t, http.StatusAccepted,
		do(t, s, "POST", "/api/chat", `{"message":"the one I change my mind about"}`).Code)
	require.Len(t, messagesOfKind(s.conversation(), "you"), 1)

	// A queued prompt is on screen the moment you send it. Taking it out of the
	// queue has to take the message with it, or the conversation keeps a prompt
	// that will never run.
	id := s.waiting()[0].ID
	rec := do(t, s, "DELETE", "/api/queue/"+id, "")
	require.Equal(t, http.StatusNoContent, rec.Code)

	assert.Empty(t, messagesOfKind(s.conversation(), "you"), "the dropped prompt is still in the conversation")
	assert.Zero(t, s.stacked())
}

func TestCancellingUnsaysEverythingItDropped(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n")
	s.setDoc("plan.md")

	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)

	for _, message := range []string{"one", "two"} {
		require.Equal(t, http.StatusAccepted,
			do(t, s, "POST", "/api/chat", `{"message":"`+message+`"}`).Code)
	}
	require.Len(t, messagesOfKind(s.conversation(), "you"), 2)

	require.Equal(t, http.StatusNoContent, do(t, s, "POST", "/api/cancel", "").Code)

	// The prompts go; the note saying they went stays.
	assert.Empty(t, messagesOfKind(s.conversation(), "you"), "cancel left the dropped prompts on screen")
	notes := messagesOfKind(s.conversation(), "note")
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Body, "dropped 2")
}

func TestMatchSentFindsTheCommentItWasPostedAs(t *testing.T) {
	thread := comments.Thread{
		ID: "t1", File: "a.go", Line: 3, Side: ghpr.SideRight, Outgoing: "this reads oddly",
	}
	found := []ghpr.ReviewComment{
		{ID: 500, Path: "a.go", Line: 3, Side: ghpr.SideRight, Body: "this reads oddly\n"},
	}

	t.Run("path, line, side and body all match", func(t *testing.T) {
		id, ok := matchSent(thread, found)
		require.True(t, ok)
		assert.Equal(t, int64(500), id, "trailing whitespace does not stop the match")
	})

	t.Run("a reply is never the comment you sent", func(t *testing.T) {
		replies := []ghpr.ReviewComment{
			{ID: 501, ReplyTo: 500, Path: "a.go", Line: 3, Side: ghpr.SideRight, Body: "this reads oddly"},
		}
		_, ok := matchSent(thread, replies)
		assert.False(t, ok)
	})

	t.Run("the same words on another line are a different comment", func(t *testing.T) {
		elsewhere := []ghpr.ReviewComment{
			{ID: 502, Path: "a.go", Line: 9, Side: ghpr.SideRight, Body: "this reads oddly"},
		}
		_, ok := matchSent(thread, elsewhere)
		assert.False(t, ok)
	})

	t.Run("the other side of the diff is a different comment", func(t *testing.T) {
		left := []ghpr.ReviewComment{
			{ID: 503, Path: "a.go", Line: 3, Side: ghpr.SideLeft, Body: "this reads oddly"},
		}
		_, ok := matchSent(thread, left)
		assert.False(t, ok)
	})

	t.Run("nothing upstream is no match, not an error", func(t *testing.T) {
		_, ok := matchSent(thread, nil)
		assert.False(t, ok)
	})
}

func TestStampSentExternalIDs(t *testing.T) {
	stub := func(t *testing.T, found []ghpr.ReviewComment, err error) {
		t.Helper()
		was := readComments
		readComments = func(context.Context, string, int) ([]ghpr.ReviewComment, error) {
			return found, err
		}
		t.Cleanup(func() { readComments = was })
	}

	sent := func(t *testing.T, s *Server) comments.Thread {
		t.Helper()
		thread := createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"this reads oddly"}`)
		updated, err := s.ws.store.SetOutgoing(thread.ID, "this reads oddly")
		require.NoError(t, err)
		return updated
	}

	t.Run("the ID GitHub gave it is recorded", func(t *testing.T) {
		s := newTestServer(t)
		thread := sent(t, s)
		stub(t, []ghpr.ReviewComment{
			{ID: 500, Path: "a.go", Line: 3, Side: ghpr.SideRight, Body: "this reads oddly"},
		}, nil)

		s.stampSentExternalIDs(context.Background(), s.ws, 6, []comments.Thread{thread})

		got, err := s.ws.store.Get(thread.ID)
		require.NoError(t, err)
		assert.Equal(t, "500", got.ExternalID)
	})

	t.Run("no match leaves the thread alone", func(t *testing.T) {
		s := newTestServer(t)
		thread := sent(t, s)
		stub(t, []ghpr.ReviewComment{
			{ID: 500, Path: "b.go", Line: 3, Side: ghpr.SideRight, Body: "something else"},
		}, nil)

		s.stampSentExternalIDs(context.Background(), s.ws, 6, []comments.Thread{thread})

		got, err := s.ws.store.Get(thread.ID)
		require.NoError(t, err)
		assert.Empty(t, got.ExternalID, "a miss costs the merge, not the send")
	})

	t.Run("a failed read does not fail the send", func(t *testing.T) {
		s := newTestServer(t)
		thread := sent(t, s)
		stub(t, nil, assert.AnError)

		s.stampSentExternalIDs(context.Background(), s.ws, 6, []comments.Thread{thread})

		got, err := s.ws.store.Get(thread.ID)
		require.NoError(t, err)
		assert.Empty(t, got.ExternalID)
	})
}

func TestNotReplyable(t *testing.T) {
	when := time.Now()

	t.Run("a thread of your own has nobody to reply to", func(t *testing.T) {
		why := notReplyable(comments.Thread{Comments: []comments.Comment{{Author: comments.AuthorYou, Body: "mine"}}})
		assert.Contains(t, why, "not on the pull request")
	})

	t.Run("an imported thread with nothing written back", func(t *testing.T) {
		why := notReplyable(comments.Thread{
			ExternalID: "100",
			Comments:   []comments.Comment{{Author: "Copilot", Body: "why 2?", ExternalID: "100"}},
		})
		assert.Contains(t, why, "write a reply first")
	})

	t.Run("an imported thread whose reply has already gone", func(t *testing.T) {
		why := notReplyable(comments.Thread{
			ExternalID: "100",
			Comments: []comments.Comment{
				{Author: "Copilot", Body: "why 2?", ExternalID: "100"},
				{Author: comments.AuthorYou, Body: "because", SentAt: &when},
			},
		})
		assert.Contains(t, why, "write a reply first")
	})

	t.Run("an imported thread with something to say", func(t *testing.T) {
		assert.Empty(t, notReplyable(comments.Thread{
			ExternalID: "100",
			Comments: []comments.Comment{
				{Author: "Copilot", Body: "why 2?", ExternalID: "100"},
				{Author: comments.AuthorYou, Body: "because"},
			},
		}))
	})
}

func TestReplyBodyJoinsWhatYouSaid(t *testing.T) {
	body, ids := replyBody([]comments.Comment{
		{ID: "c1", Body: "  because it is\n"},
		{ID: "c2", Body: "and here is why"},
	})
	assert.Equal(t, "because it is\n\nand here is why", body, "one comment, not two notifications")
	assert.Equal(t, []string{"c1", "c2"}, ids)
}

func TestSendReplyRefusesWhatItCannotSend(t *testing.T) {
	s := newTestServer(t)
	thread := createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"mine alone"}`)

	rec := do(t, s, "POST", "/api/threads/"+thread.ID+"/send-reply", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "not on the pull request")

	rec = do(t, s, "POST", "/api/threads/nope/send-reply", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// An imported thread is no longer refused outright by the send path: it has a
// reply route of its own, and the message has to point at it.
func TestAnImportedThreadIsPointedAtTheReplyRoute(t *testing.T) {
	why := notSendable(comments.Thread{ExternalID: "100"})
	assert.Contains(t, why, "reply")
}

func TestReplyableIsWhatHasSomethingWaitingForAReviewer(t *testing.T) {
	sent := time.Now()
	list := []comments.Thread{
		{ID: "drafted", ExternalID: "100", Comments: []comments.Comment{
			{Author: "Copilot", Body: "why 2?", ExternalID: "100"},
			{Author: comments.AuthorDraft, Body: "because it is"},
		}},
		{ID: "typed", ExternalID: "200", Comments: []comments.Comment{
			{Author: "Copilot", Body: "and here?", ExternalID: "200"},
			{Author: comments.AuthorYou, Body: "fixed"},
		}},
		{ID: "answered", ExternalID: "300", Comments: []comments.Comment{
			{Author: "Copilot", Body: "third", ExternalID: "300"},
			{Author: comments.AuthorYou, Body: "said already", SentAt: &sent},
		}},
		{ID: "unanswered", ExternalID: "400", Comments: []comments.Comment{
			{Author: "Copilot", Body: "fourth", ExternalID: "400"},
		}},
		{ID: "mine", Comments: []comments.Comment{{Author: "agent", Body: "my own finding"}}},
	}

	ids := []string{}
	for _, thread := range replyable(list) {
		ids = append(ids, thread.ID)
	}
	assert.Equal(t, []string{"drafted", "typed"}, ids)
}

func TestSendingEveryReplyHasToNameThePullRequest(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})

	for _, body := range []string{`{}`, `{"confirm":0}`, `{"confirm":8}`} {
		rec := do(t, s, "POST", "/api/threads/send-replies", body)
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "accepted %s", body)
		assert.Contains(t, rec.Body.String(), "has to name it")
	}
}

func TestSendingEveryReplyNeedsAReplyToSend(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	// An imported comment nobody has answered is not a reply waiting.
	thread, err := s.space().store.Add(comments.Thread{
		File: "a.go", Side: "right", Line: 3, ExternalID: "100",
		Comments: []comments.Comment{{Author: "Copilot", Body: "why 2?", ExternalID: "100"}},
	})
	require.NoError(t, err)

	rec := do(t, s, "POST", "/api/threads/send-replies", `{"confirm":7,"threads":["`+thread.ID+`"]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "none of those threads has a reply waiting")
}

// A batch that names no thread is a client that lost track of what it showed,
// not permission to send whatever is pending.
func TestSendingEveryReplyNamesTheThreadsItShowed(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})

	rec := do(t, s, "POST", "/api/threads/send-replies", `{"confirm":7}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "name the threads")
}

// fakeGitHub stands in for gh, and records what each reply posted.
type fakeGitHub struct {
	posts []string
	fail  map[int64]error
}

func (f *fakeGitHub) install(t *testing.T) {
	t.Helper()
	wasRepo, wasPost := repoOf, postReply
	repoOf = func(context.Context, string) (string, string, error) { return "o", "r", nil }
	postReply = func(_ context.Context, _, _, _ string, _ int, comment int64, body string) error {
		if err := f.fail[comment]; err != nil {
			return err
		}
		f.posts = append(f.posts, fmt.Sprintf("%d:%s", comment, body))
		return nil
	}
	t.Cleanup(func() { repoOf, postReply = wasRepo, wasPost })
}

// imported adds a thread from the pull request with a drafted reply on it.
func imported(t *testing.T, s *Server, external string, line int, reply string) comments.Thread {
	t.Helper()
	thread, err := s.space().store.Add(comments.Thread{
		File: "a.go", Side: "right", Line: line, ExternalID: external,
		Comments: []comments.Comment{
			{Author: "Copilot", Body: "why?", ExternalID: external},
			{Author: comments.AuthorDraft, Body: reply},
		},
	})
	require.NoError(t, err)
	return thread
}

func TestEveryNamedThreadGetsOneReplyAndIsMarkedSent(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	gh := &fakeGitHub{}
	gh.install(t)

	first := imported(t, s, "100", 3, "because it is")
	second := imported(t, s, "200", 9, "fixed in the next commit")

	rec := do(t, s, "POST", "/api/threads/send-replies",
		fmt.Sprintf(`{"confirm":7,"threads":[%q,%q]}`, first.ID, second.ID))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var out struct {
		Sent    int              `json:"sent"`
		Threads int              `json:"threads"`
		Failed  []map[string]any `json:"failed"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, 2, out.Threads)
	assert.Equal(t, 2, out.Sent)
	assert.Empty(t, out.Failed)

	// One post per thread, each with its own body, and neither offered again.
	assert.Equal(t, []string{"100:because it is", "200:fixed in the next commit"}, gh.posts)
	assert.Empty(t, replyable(s.space().store.List()))
}

func TestOneReplyThatFailsDoesNotStopTheRest(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	gh := &fakeGitHub{fail: map[int64]error{100: errors.New("gh said no")}}
	gh.install(t)

	first := imported(t, s, "100", 3, "this one fails")
	second := imported(t, s, "200", 9, "this one goes")

	rec := do(t, s, "POST", "/api/threads/send-replies",
		fmt.Sprintf(`{"confirm":7,"threads":[%q,%q]}`, first.ID, second.ID))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, []string{"200:this one goes"}, gh.posts)

	// The one that failed is still waiting; the one that went is not.
	left := replyable(s.space().store.List())
	require.Len(t, left, 1)
	assert.Equal(t, first.ID, left[0].ID)
}

// A thread you resolved is one you decided against answering, so its draft
// must not ride along in a batch.
func TestAResolvedThreadIsNotRepliedTo(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	gh := &fakeGitHub{}
	gh.install(t)

	thread := imported(t, s, "100", 3, "a draft you thought better of")
	_, err := s.space().store.SetResolved(thread.ID, true)
	require.NoError(t, err)

	assert.Empty(t, replyable(s.space().store.List()))
	rec := do(t, s, "POST", "/api/threads/send-replies",
		fmt.Sprintf(`{"confirm":7,"threads":[%q]}`, thread.ID))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, gh.posts)

	// And the button on the thread itself refuses for the same reason.
	rec = do(t, s, "POST", "/api/threads/"+thread.ID+"/send-reply", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "resolved")
}

// A draft that arrived while the modal was open was never on screen.
func TestOnlyTheThreadsTheModalShowedArePosted(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	gh := &fakeGitHub{}
	gh.install(t)

	shown := imported(t, s, "100", 3, "the one you read")
	imported(t, s, "200", 9, "a draft that landed after you opened the modal")

	rec := do(t, s, "POST", "/api/threads/send-replies",
		fmt.Sprintf(`{"confirm":7,"threads":[%q]}`, shown.ID))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, []string{"100:the one you read"}, gh.posts)
	assert.Len(t, replyable(s.space().store.List()), 1, "the unread draft is still waiting")
}

func TestSendingEveryReplyNeedsAPullRequest(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/threads/send-replies", `{"confirm":7}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no pull request")
}
