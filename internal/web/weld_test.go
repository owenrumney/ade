package web

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/weld"
)

type chatRun struct {
	ctx     context.Context
	context *chat.Context
	message string
	files   []chat.File
}

// chatRuns stands in for the chat turn drain starts, and reports each one.
func chatRuns(t *testing.T, s *Server) chan chatRun {
	t.Helper()
	runs := make(chan chatRun, 8)
	s.runChatTurn = func(ctx context.Context, attached *chat.Context, message string, files []chat.File) {
		runs <- chatRun{ctx: ctx, context: attached, message: message, files: files}
	}
	return runs
}

func nextRun(t *testing.T, runs chan chatRun) chatRun {
	t.Helper()
	select {
	case run := <-runs:
		return run
	case <-time.After(5 * time.Second):
		t.Fatal("no chat turn started")
		return chatRun{}
	}
}

// queueWhileRunning starts a run and types the prompts behind it, shown as sent.
func queueWhileRunning(t *testing.T, s *Server, prompts ...queued) []ChatMessage {
	t.Helper()
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	started := s.agent.State().StartedAt
	var shown []ChatMessage
	for i, q := range prompts {
		q.at = started.Add(time.Duration(i+1) * time.Microsecond)
		if q.kind == kindChat {
			msg := s.say(ChatMessage{Kind: chat.KindYou, Body: q.message, Context: q.context, Files: q.files})
			q.chatID = msg.ID
			shown = append(shown, msg)
		}
		s.stack(q)
	}
	return shown
}

func TestTwoLinkedPromptsGoAsOneTurn(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	runs := chatRuns(t, s)
	calls := sharpenAnswers(t, func(opts bridge.OneShotOpts) (*bridge.Turn, error) {
		assert.Equal(t, weld.Model, opts.Model)
		assert.Equal(t, weld.Role(), opts.SystemPrompt)
		assert.Equal(t, []string{}, opts.Tools)
		assert.True(t, opts.NoMCP)
		assert.Contains(t, opts.Prompt, "fix the retry test")
		assert.Contains(t, opts.Prompt, "and keep the timeout at 2s")
		return &bridge.Turn{Text: "1"}, nil
	})
	shown := queueWhileRunning(t, s,
		queued{kind: kindChat, message: "fix the retry test", files: []chat.File{{Name: "a.txt", Path: "/x/a.txt"}}},
		queued{kind: kindChat, message: "and keep the timeout at 2s", files: []chat.File{{Name: "a.txt", Path: "/x/a.txt"}, {Name: "b.txt", Path: "/x/b.txt"}}},
	)

	s.agent.finish(nil)

	run := nextRun(t, runs)
	assert.Equal(t, "fix the retry test\n\nand keep the timeout at 2s", run.message)
	assert.Equal(t, []chat.File{{Name: "a.txt", Path: "/x/a.txt"}, {Name: "b.txt", Path: "/x/b.txt"}}, run.files, "the files are the union")
	assert.Equal(t, 1, *calls)
	assert.Zero(t, s.stacked(), "both came off the queue")
	assert.Empty(t, runs, "one turn, not two")

	// Both stay on screen; the second says which it went with.
	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 2)
	assert.Empty(t, yours[0].FoldedInto)
	assert.Equal(t, shown[0].ID, yours[1].FoldedInto)

	s.agent.Cancel()
	s.agent.finish(nil)
}

func TestAReviewBetweenTwoPromptsKeepsThemApart(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	runs := chatRuns(t, s)
	calls := sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: "2"}, nil
	})
	queueWhileRunning(t, s,
		queued{kind: kindChat, message: "first"},
		queued{kind: kindSwitchModel, model: "opus"},
		queued{kind: kindChat, message: "second"},
	)

	s.agent.finish(nil)

	assert.Equal(t, "first", nextRun(t, runs).message)
	assert.Equal(t, 2, s.stacked())
	assert.Zero(t, *calls, "the gate decides candidacy; the model is never asked about what fails it")

	s.agent.Cancel()
	s.agent.finish(nil)
}

func TestADeciderThatFailsRunsTheFirstAlone(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	runs := chatRuns(t, s)
	sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return nil, errors.New("claude is not logged in")
	})
	queueWhileRunning(t, s,
		queued{kind: kindChat, message: "first"},
		queued{kind: kindChat, message: "second"},
	)

	s.agent.finish(nil)

	assert.Equal(t, "first", nextRun(t, runs).message)
	assert.Equal(t, 1, s.stacked(), "the second is still waiting")
	for _, msg := range s.conversation() {
		assert.Empty(t, msg.FoldedInto)
	}

	s.agent.Cancel()
	s.agent.finish(nil)
}

func TestMergingTurnedOffAsksNothing(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Update(func(into *settings.Settings) error {
		into.MergeQueuedOff = true
		return nil
	}))
	runs := chatRuns(t, s)
	calls := sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: "1"}, nil
	})
	queueWhileRunning(t, s,
		queued{kind: kindChat, message: "first"},
		queued{kind: kindChat, message: "second"},
	)

	s.agent.finish(nil)

	assert.Equal(t, "first", nextRun(t, runs).message)
	assert.Equal(t, 1, s.stacked())
	assert.Zero(t, *calls)

	s.agent.Cancel()
	s.agent.finish(nil)
}

func TestARestoredQueueIsNeverWelded(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	runs := chatRuns(t, s)
	calls := sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: "1"}, nil
	})
	s.stack(queued{kind: kindChat, message: "first"})
	s.stack(queued{kind: kindChat, message: "second"})

	// No run has just ended, so nothing was typed while watching one.
	rec := do(t, s, "POST", "/api/queue/run", "")
	require.Equal(t, http.StatusNoContent, rec.Code)

	assert.Equal(t, "first", nextRun(t, runs).message)
	assert.Equal(t, 1, s.stacked())
	assert.Zero(t, *calls)

	s.agent.Cancel()
	s.agent.finish(nil)
}

func TestWeldedPromptsCanBeRunSeparately(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	runs := chatRuns(t, s)
	sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: "1"}, nil
	})
	shown := queueWhileRunning(t, s,
		queued{kind: kindChat, message: "first"},
		queued{kind: kindChat, message: "second"},
	)
	s.agent.finish(nil)
	welded := nextRun(t, runs)
	require.Equal(t, "first\n\nsecond", welded.message)

	rec := do(t, s, "POST", "/api/chat/"+shown[0].ID+"/separate", "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	// The run stops, both are back in order, and the mark comes off.
	<-welded.ctx.Done()
	waiting := s.waiting()
	require.Len(t, waiting, 2)
	assert.Equal(t, []string{"first", "second"}, []string{waiting[0].What, waiting[1].What})
	for _, msg := range s.conversation() {
		assert.Empty(t, msg.FoldedInto)
	}

	// The first runs alone: it was typed before the turn that just ended.
	s.endRun(welded.ctx.Err())
	assert.Equal(t, "first", nextRun(t, runs).message)
	assert.Equal(t, 1, s.stacked())
	notes := messagesOfKind(s.conversation(), chat.KindNote)
	require.NotEmpty(t, notes)
	assert.Equal(t, "the prompts are being run separately", notes[len(notes)-1].Body)

	// Pressed again, there is nothing to take apart.
	rec = do(t, s, "POST", "/api/chat/"+shown[0].ID+"/separate", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	s.agent.Cancel()
	s.agent.finish(nil)
}

func TestAFinishedWeldedTurnIsNotRunAgainSeparately(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	runs := chatRuns(t, s)
	sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: "1"}, nil
	})
	shown := queueWhileRunning(t, s,
		queued{kind: kindChat, message: "first"},
		queued{kind: kindChat, message: "second"},
	)
	s.agent.finish(nil)
	require.Equal(t, "first\n\nsecond", nextRun(t, runs).message)

	// The turn is over but its drain has not come round yet: the window
	// finish leaves while it syncs the login.
	s.agent.mu.Lock()
	s.agent.running = false
	s.agent.mu.Unlock()

	rec := do(t, s, "POST", "/api/chat/"+shown[0].ID+"/separate", "")
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Zero(t, s.stacked(), "work already done is not queued to be done again")
	assert.Equal(t, shown[0].ID, messagesOfKind(s.conversation(), chat.KindYou)[1].FoldedInto)

	s.agent.finish(nil)
}

func TestCancelWhileTheDeciderReadsDropsTheTurn(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	runs := chatRuns(t, s)
	sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		assert.Equal(t, 2, s.dropStack(), "the turn being decided is still queued, so Cancel reaches it")
		return &bridge.Turn{Text: "1"}, nil
	})
	queueWhileRunning(t, s,
		queued{kind: kindChat, message: "first"},
		queued{kind: kindChat, message: "second"},
	)

	s.agent.finish(nil)

	assert.False(t, s.agent.State().Running)
	assert.Empty(t, runs, "nothing the user cancelled ran")
	assert.Zero(t, s.stacked())
	assert.Empty(t, messagesOfKind(s.conversation(), chat.KindYou))
}

func TestDroppingAPromptDropsWhatWasFoldedIntoIt(t *testing.T) {
	s := newTestServer(t)
	first := s.say(ChatMessage{Kind: chat.KindYou, Body: "first"})
	second := s.say(ChatMessage{Kind: chat.KindYou, Body: "second"})
	second.FoldedInto = first.ID
	s.amend(second)

	s.unsay(first.ID)

	assert.Empty(t, messagesOfKind(s.conversation(), chat.KindYou))
}

func TestJoinedCarriesTheSelectionsAndCapsTheFiles(t *testing.T) {
	var many []chat.File
	for i := 0; i < chat.MaxFiles+2; i++ {
		many = append(many, chat.File{Name: "f", Path: string(rune('a' + i))})
	}
	first := queued{message: "one", context: &chat.Context{From: "a.go", Text: "x := 1"}, files: many[:2]}
	second := queued{message: "two", context: &chat.Context{From: "a.go", Text: "y := 2"}, files: many}
	third := queued{message: "three"}

	got := joined(first, []queued{second, third})
	assert.Equal(t, "one\n\ntwo\n\nthree", got.message)
	assert.Equal(t, &chat.Context{From: "a.go", Text: "x := 1\n\ny := 2"}, got.context)
	assert.Len(t, got.files, chat.MaxFiles)
	assert.Equal(t, "x := 1", first.context.Text, "the first's own selection is not written to")

	alone := joined(queued{message: "one"}, []queued{{message: "two", context: &chat.Context{From: "b.go", Text: "z"}}})
	assert.Equal(t, &chat.Context{From: "b.go", Text: "z"}, alone.context)

	// A selection from another file is not written up as this one's.
	apart := joined(first, []queued{{message: "two", context: &chat.Context{From: "b.go", Text: "z"}}})
	assert.Equal(t, &chat.Context{From: "a.go", Text: "x := 1"}, apart.context)
}

func TestAPromptBackOnTheSubjectJoinsItPastAnAside(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	runs := chatRuns(t, s)
	sharpenAnswers(t, func(opts bridge.OneShotOpts) (*bridge.Turn, error) {
		assert.Contains(t, opts.Prompt, "<next n=\"3\">\nand log each retry\n</next>")
		return &bridge.Turn{Text: "1, 3"}, nil
	})
	shown := queueWhileRunning(t, s,
		queued{kind: kindChat, message: "fix the retry loop"},
		queued{kind: kindChat, message: "and reset its counter on success"},
		queued{kind: kindChat, message: "how does the launcher pick a port?"},
		queued{kind: kindChat, message: "and log each retry"},
	)

	s.agent.finish(nil)

	// type, Enter, type more, Enter, ooh shiny, Enter, back to the job: two
	// turns, the job first and the aside after it.
	run := nextRun(t, runs)
	assert.Equal(t, "fix the retry loop\n\nand reset its counter on success\n\nand log each retry", run.message)
	waiting := s.waiting()
	require.Len(t, waiting, 1)
	assert.Equal(t, "how does the launcher pick a port?", waiting[0].What)

	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 4)
	assert.Equal(t, shown[0].ID, yours[1].FoldedInto)
	assert.Empty(t, yours[2].FoldedInto, "the aside is its own turn")
	assert.Equal(t, shown[0].ID, yours[3].FoldedInto)

	s.agent.Cancel()
	s.agent.finish(nil)
}

func TestACancelledWeldGivesThePromptsBack(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	runs := chatRuns(t, s)
	sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: "1"}, nil
	})
	queueWhileRunning(t, s,
		queued{kind: kindChat, message: "fix the retry loop"},
		queued{kind: kindChat, message: "and the fixture"},
	)
	s.agent.finish(nil)
	welded := nextRun(t, runs)
	require.Equal(t, "fix the retry loop\n\nand the fixture", welded.message)

	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 2)
	require.NotEmpty(t, yours[1].FoldedInto, "folded while the turn is going")

	require.True(t, s.agent.Cancel())
	<-welded.ctx.Done()
	s.endRun(welded.ctx.Err())

	after := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, after, 2)
	for _, msg := range after {
		assert.Empty(t, msg.FoldedInto)
	}
	assert.Equal(t, "and the fixture", after[1].Body)

	s.agent.Cancel()
	s.agent.finish(nil)
}

func TestAWeldThatRanKeepsItsFold(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	runs := chatRuns(t, s)
	sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: "1"}, nil
	})
	shown := queueWhileRunning(t, s,
		queued{kind: kindChat, message: "first"},
		queued{kind: kindChat, message: "second"},
	)
	s.agent.finish(nil)
	nextRun(t, runs)

	s.endRun(nil)

	after := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, after, 2)
	assert.Equal(t, shown[0].ID, after[1].FoldedInto)
}
