package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
)

func TestAFinishedRunTakesTheNextTurn(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n")
	s.setDoc("plan.md")

	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	require.Equal(t, 1, s.stack(queued{kind: kindChat, message: "next"}))

	// The runner tells the server when it goes idle, so a queue empties without
	// anything polling for a gap.
	s.agent.finish(nil)
	assert.Zero(t, s.stacked(), "the queued turn was never picked up")

	s.agent.Cancel()
	s.agent.finish(nil)
}

func TestAnUnknownQueuedKindDoesNotHoldTheAgent(t *testing.T) {
	s := newTestServer(t)
	s.stack(queued{kind: "nonsense", message: "hello"})

	s.drain()

	// Holding the lock for something nothing knows how to run would wedge every
	// later run behind it.
	assert.Zero(t, s.stacked())
	assert.False(t, s.agent.State().Running)
}

func TestDroppingTheStackReportsWhatWent(t *testing.T) {
	s := newTestServer(t)
	s.stack(queued{kind: kindChat, message: "one"})
	s.stack(queued{kind: kindChat, message: "two"})

	assert.Equal(t, 2, s.dropStack())
	assert.Zero(t, s.dropStack(), "dropping an empty queue is not an error")
}

func TestRestackPutsATurnBackAtTheFront(t *testing.T) {
	s := newTestServer(t)
	s.stack(queued{kind: kindChat, message: "second"})
	s.restack(queued{kind: kindChat, message: "first"})

	// A turn that lost the race for the agent has to keep its place, or the
	// conversation reorders itself under you.
	next, ok := s.unstack()
	require.True(t, ok)
	assert.Equal(t, "first", next.message)
}

func TestTheQueueDepthIsOnTheWire(t *testing.T) {
	s := newTestServer(t)
	s.stack(queued{kind: kindChat, message: "waiting"})

	assert.Equal(t, 1, s.agentState().Queued)
}

func TestTheQueueSaysWhatIsInIt(t *testing.T) {
	s := newTestServer(t)
	s.stack(queued{kind: kindChat, message: "make the retry loop reset its counter"})
	s.stack(queued{kind: kindConvert, thread: "t1"})

	waiting := s.waiting()
	require.Len(t, waiting, 2)

	// A column of kinds says how many are waiting and nothing else.
	assert.Equal(t, "Chat", waiting[0].Kind)
	assert.Equal(t, "make the retry loop reset its counter", waiting[0].What)
	assert.Equal(t, "Convert for GitHub", waiting[1].Kind)
	assert.NotEmpty(t, waiting[1].What)

	// In the order they will run.
	assert.NotEqual(t, waiting[0].ID, waiting[1].ID)
}

func TestOneQueuedTurnCanBeDropped(t *testing.T) {
	s := newTestServer(t)
	s.stack(queued{kind: kindChat, message: "first"})
	s.stack(queued{kind: kindChat, message: "second"})
	s.stack(queued{kind: kindChat, message: "third"})

	middle := s.waiting()[1].ID
	rec := do(t, s, "DELETE", "/api/queue/"+middle, "")
	require.Equal(t, http.StatusNoContent, rec.Code)

	// Cancel drops everything; changing your mind about one prompt should not
	// cost you the other two, and the rest keep their order.
	left := s.waiting()
	require.Len(t, left, 2)
	assert.Equal(t, "first", left[0].What)
	assert.Equal(t, "third", left[1].What)

	// An id that has already run is gone, not an error worth panicking about.
	rec = do(t, s, "DELETE", "/api/queue/"+middle, "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAnIdIsNotAPosition(t *testing.T) {
	s := newTestServer(t)
	s.stack(queued{kind: kindChat, message: "first"})
	s.stack(queued{kind: kindChat, message: "second"})

	// Positions shift as the queue drains under you, so removing by index would
	// remove whatever had moved into that slot.
	first := s.waiting()[0].ID
	_, ok := s.unstack()
	require.True(t, ok)

	assert.False(t, s.drop(first), "the id of a turn that already ran matches nothing")
	assert.Len(t, s.waiting(), 1)
}

func TestACancelledRunSaysSoInTheConversation(t *testing.T) {
	s := newTestServer(t)

	ctx, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	require.True(t, s.agent.Cancel())
	<-ctx.Done()
	s.endRun(ctx.Err())

	// "context canceled" in the conversation would be as unhelpful there as it
	// was on the page.
	messages := s.conversation()
	require.Len(t, messages, 1)
	assert.Equal(t, "you cancelled the run", messages[0].Body)
	assert.True(t, messages[0].Failed)
	assert.Equal(t, "review", messages[0].Run, "the note says which run it was")
}

func TestARestoredQueueWaitsToBeRun(t *testing.T) {
	s := newTestServer(t)
	shown := s.say(ChatMessage{Kind: chat.KindYou, Body: "next"})
	s.stack(queued{kind: kindChat, message: "next", chatID: shown.ID})
	s.Close()

	store, err := comments.Open(s.startGitDir, "feature")
	require.NoError(t, err)
	again, err := New(Config{
		Root: s.startRoot, Branch: "feature", GitDir: s.startGitDir, CommonGitDir: s.commonGitDir,
		Diff: gitdiff.Options{Repo: s.startRoot},
	}, store)
	require.NoError(t, err)
	defer again.Close()

	// Back on the queue, not running: the store it came from is a file in the
	// git dir, so nothing in it gets to start a run by itself.
	assert.Equal(t, 1, again.stacked())
	assert.False(t, again.agent.State().Running)
	var notes []string
	for _, msg := range messagesOfKind(again.conversation(), chat.KindNote) {
		notes = append(notes, msg.Body)
	}
	assert.Contains(t, strings.Join(notes, "\n"), "1 queued turn(s) restored")

	// Asked for, it goes.
	rec := do(t, again, "POST", "/api/queue/run", "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Zero(t, again.stacked())
	again.agent.Cancel()
	again.agent.finish(nil)

	rec = do(t, again, "POST", "/api/queue/run", "")
	assert.Equal(t, http.StatusNotFound, rec.Code, "an empty queue has nothing to run")
}

func TestASavedQueueIsNotTakenBackOnTrust(t *testing.T) {
	s := newTestServer(t)
	shown := s.say(ChatMessage{Kind: chat.KindYou, Body: "real"})

	records := []queuedRecord{
		{ID: "q1", Kind: kindChat, Message: "real", ChatID: shown.ID},
		{ID: "q2", Kind: kindChat, Message: "planted", ChatID: "not-on-screen"},
		{ID: "q3", Kind: kindChat, Message: strings.Repeat("x", maxRestoredMessage+1), ChatID: shown.ID},
		{ID: "q4", Kind: kindSwitchModel, Model: "opus"},
		{ID: "q5", Kind: kindChat, Message: "no message at all"},
	}
	for i := 6; i <= maxRestored+5; i++ {
		records = append(records, queuedRecord{ID: fmt.Sprintf("q%d", i), Kind: kindChat, Message: "more", ChatID: shown.ID})
	}
	data, err := json.Marshal(records)
	require.NoError(t, err)
	require.NoError(t, s.chat.PutState(queueState, data))

	require.Equal(t, maxRestored-3, s.loadQueue())

	var ids []string
	for _, w := range s.waiting() {
		ids = append(ids, w.ID)
	}
	// Only what names a message on screen, at a size someone typed, and no
	// more than fit; a model switch has no message to name.
	assert.Contains(t, ids, "q1")
	assert.Contains(t, ids, "q4")
	assert.NotContains(t, ids, "q2", "no such message in the conversation")
	assert.NotContains(t, ids, "q3", "too big to have been typed")
	assert.NotContains(t, ids, "q5", "a chat turn with no message")
	assert.Contains(t, ids, fmt.Sprintf("q%d", maxRestored))
	assert.NotContains(t, ids, fmt.Sprintf("q%d", maxRestored+1), "capped")
	assert.False(t, s.agent.State().Running)
}

func TestMarkingAQueuedTurnUrgentMovesItToTheFront(t *testing.T) {
	s := newTestServer(t)
	s.stack(queued{kind: kindChat, message: "first"})
	s.stack(queued{kind: kindChat, message: "second"})
	third := s.stack(queued{kind: kindChat, message: "third"})
	require.Equal(t, 3, third)

	waiting := s.waiting()
	require.Len(t, waiting, 3)

	rec := do(t, s, "POST", "/api/queue/"+waiting[2].ID+"/urgent", "")
	require.Equal(t, http.StatusNoContent, rec.Code)

	after := s.waiting()
	assert.Equal(t, []string{"third", "first", "second"},
		[]string{after[0].What, after[1].What, after[2].What})
}

func TestMarkingTheFrontUrgentChangesNothing(t *testing.T) {
	s := newTestServer(t)
	s.stack(queued{kind: kindChat, message: "first"})
	s.stack(queued{kind: kindChat, message: "second"})

	front := s.waiting()[0].ID
	rec := do(t, s, "POST", "/api/queue/"+front+"/urgent", "")
	require.Equal(t, http.StatusNoContent, rec.Code)

	after := s.waiting()
	assert.Equal(t, []string{"first", "second"}, []string{after[0].What, after[1].What})
}

func TestMarkingATurnThatHasRunUrgentIsNotFound(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/queue/q99/urgent", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// A queued button reads its inputs when its turn comes, not when it was
// pressed: the run it waited behind may have done the work for it.
func TestAQueuedButtonReadsItsInputsWhenItRuns(t *testing.T) {
	s := newTestServer(t)
	thread := createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"why 2?"}`)

	_, ok := s.agent.begin(context.Background(), "chat")
	require.True(t, ok)
	rec := do(t, s, "POST", "/api/resolve", `{}`)
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Len(t, s.waiting(), 1)

	// Resolved while it waited, so there is nothing left for it to apply.
	_, err := s.ws.store.SetResolved(thread.ID, true)
	require.NoError(t, err)

	s.agent.finish(nil)
	s.drain()

	assert.Empty(t, s.waiting(), "it came off the queue")
	assert.False(t, s.agent.State().Running, "and started no run")
}

// Every kind the queue can hold has to name itself, or the view is a column of
// rows that each say the same nothing.
func TestEveryQueuedKindSaysWhatItWillDo(t *testing.T) {
	for _, kind := range []string{
		kindChat, kindConvert, kindSwitchModel, kindCommitMessage, kindFixChecks,
		kindReview, kindResolve, kindDraftReply, kindPRCreate, kindSeed, kindFold,
		kindQuiz,
	} {
		assert.NotEqual(t, kind, label(kind), "%s has no label", kind)
		if kind == kindChat {
			continue
		}
		assert.NotEmpty(t, queued{kind: kind}.describe(), "%s describes nothing", kind)
	}
}

func TestAQueuedPromptCanBeReworded(t *testing.T) {
	// Second thoughts about the wording should not cost the place in the queue,
	// which dropping and resending would.
	s := newTestServer(t)
	s.stack(queued{kind: kindChat, message: "first"})
	s.stack(queued{kind: kindChat, message: "make the retry loop reset"})
	s.stack(queued{kind: kindChat, message: "third"})
	middle := s.waiting()[1].ID

	rec := do(t, s, "PUT", "/api/queue/"+middle, `{"message":"reset the counter too"}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	left := s.waiting()
	require.Len(t, left, 3)
	assert.Equal(t, "reset the counter too", left[1].What)
	assert.Equal(t, middle, left[1].ID, "the same entry, in the same place")
	assert.Equal(t, "first", left[0].What)
	assert.Equal(t, "third", left[2].What)
}

// What is truncated for the row, so an editor prefilled from it would cut the
// turn short without saying so.
func TestTheWholePromptIsOfferedForEditing(t *testing.T) {
	s := newTestServer(t)
	long := strings.Repeat("make the retry loop reset its counter. ", 12)
	s.stack(queued{kind: kindChat, message: long})

	waiting := s.waiting()
	require.Len(t, waiting, 1)
	assert.Equal(t, long, waiting[0].Message)
	assert.Less(t, len(waiting[0].What), len(long), "the row is still truncated")
}

func TestRewordingNeedsSomethingToSay(t *testing.T) {
	s := newTestServer(t)
	s.stack(queued{kind: kindChat, message: "first"})
	id := s.waiting()[0].ID

	rec := do(t, s, "PUT", "/api/queue/"+id, `{"message":"   "}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "first", s.waiting()[0].What, "left as it was")
}

func TestAQueuedButtonHasNoWordingToChange(t *testing.T) {
	// A queued review or commit message is a button someone pressed. Offering
	// an editor for it would ask what to type and then ignore it.
	s := newTestServer(t)
	s.stack(queued{kind: kindCommitMessage})
	id := s.waiting()[0].ID
	assert.Empty(t, s.waiting()[0].Message, "so the button is not offered")

	rec := do(t, s, "PUT", "/api/queue/"+id, `{"message":"something else"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "button")
}

func TestRewordingAQueuedTurnThatHasRunSaysSo(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "PUT", "/api/queue/q99", `{"message":"too late"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// The queued prompt is on screen from the moment it is sent. An edit that only
// reached the queue would leave the conversation showing the old wording.
func TestRewordingRewritesWhatIsOnScreen(t *testing.T) {
	s := newTestServer(t)
	s.agent.resume("sess-queue")
	shown := s.say(ChatMessage{Kind: chat.KindYou, Body: "make the retry loop reset"})
	require.NotEmpty(t, shown.ID)
	s.stack(queued{kind: kindChat, message: "make the retry loop reset", chatID: shown.ID})
	id := s.waiting()[0].ID

	rec := do(t, s, "PUT", "/api/queue/"+id, `{"message":"reset the counter too"}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	said := s.conversation()
	require.Len(t, said, 1)
	assert.Equal(t, "reset the counter too", said[0].Body)
}
