package web

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gate"
	"github.com/owenrumney/ade/internal/resolve"
	"github.com/owenrumney/ade/internal/runlog"
)

// busyAgent is a bridge.Agent that is only ever asked whether it is busy.
type busyAgent struct{ busy bool }

func (b *busyAgent) Send(context.Context, string, ...bridge.Image) (*bridge.Turn, error) {
	return nil, nil
}
func (b *busyAgent) SendCommand(context.Context, string) (*bridge.Turn, error) { return nil, nil }
func (b *busyAgent) Inject(string, ...bridge.Image) error                      { return nil }
func (b *busyAgent) Busy() bool                                                { return b.busy }
func (b *busyAgent) Dead() bool                                                { return false }
func (b *busyAgent) Close() error                                              { return nil }

func TestARunSaysItTakesAMessageOnceAndOncePerRun(t *testing.T) {
	runner := newAgentRunner(t.TempDir(), "", "", "", "", sandboxConfig{}, nil)
	agent := &busyAgent{}
	runner.agent = agent

	assert.False(t, runner.tellBusy(), "nothing is running, so there is nothing to speak to")

	_, ok := runner.begin(context.Background(), kindChat)
	require.True(t, ok)
	assert.False(t, runner.tellBusy(), "the subprocess has not reached Send yet")

	agent.busy = true
	assert.True(t, runner.tellBusy(), "the page read 'no' between begin and Send, so it is told")
	assert.False(t, runner.tellBusy(), "every later event on the same run is noise")
	runner.finish(nil)

	_, ok = runner.begin(context.Background(), kindChat)
	require.True(t, ok)
	assert.True(t, runner.tellBusy(), "a second run is a second answer")
	runner.finish(nil)
}

func TestAgentResolveRequiresOpenThreads(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/resolve", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no open comments")
}

func TestAgentResolveQueuesBehindAConcurrentRun(t *testing.T) {
	s := newTestServer(t)
	createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"why 2?"}`)

	_, ok := s.agent.begin(context.Background(), "resolve")
	require.True(t, ok, "first claim should succeed")
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/resolve", `{}`)
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `"queued":1`)
	require.Len(t, s.waiting(), 1)
	assert.Equal(t, "Resolve", s.waiting()[0].Kind)
}

func TestAgentResolveSkipsResolvedThreads(t *testing.T) {
	s := newTestServer(t)
	thread := createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"why 2?"}`)
	_, err := s.ws.store.SetResolved(thread.ID, true)
	require.NoError(t, err)

	rec := do(t, s, "POST", "/api/resolve", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "a resolved thread is not work")
}

func TestThreadsToResolve(t *testing.T) {
	s := newTestServer(t)
	first := createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"one"}`)
	second := createThread(t, s, `{"file":"a.go","side":"left","line":3,"body":"two"}`)

	t.Run("no ids means every open thread", func(t *testing.T) {
		assert.Len(t, s.threadsToResolve(nil), 2)
	})

	t.Run("ids select a subset", func(t *testing.T) {
		got := s.threadsToResolve([]string{second.ID})
		require.Len(t, got, 1)
		assert.Equal(t, second.ID, got[0].ID)
	})

	t.Run("unknown ids are ignored", func(t *testing.T) {
		assert.Empty(t, s.threadsToResolve([]string{"nope"}))
	})

	t.Run("resolved threads are excluded even when named", func(t *testing.T) {
		_, err := s.ws.store.SetResolved(first.ID, true)
		require.NoError(t, err)
		assert.Empty(t, s.threadsToResolve([]string{first.ID}))
	})
}

func TestRunStateTracksProgress(t *testing.T) {
	runner := newAgentRunner(t.TempDir(), "sess-123", "", "", "", sandboxConfig{}, nil)

	assert.False(t, runner.State().Running)
	_, ok := runner.begin(context.Background(), "resolve")
	require.True(t, ok)
	_, again := runner.begin(context.Background(), "resolve")
	assert.False(t, again, "a second claim must fail")

	var heard []string
	runner.onStep = func(text string) { heard = append(heard, text) }
	runner.step("Edit auth.go")
	state := runner.State()
	assert.True(t, state.Running)
	assert.Equal(t, "resolve", state.Kind)
	assert.Equal(t, []string{"Edit auth.go"}, heard, "progress goes to whoever is listening")
	assert.NotEmpty(t, state.Activity, "progress keeps the running badge active")
	assert.Equal(t, "sess-123", state.Session)

	runner.finish(assert.AnError)
	after := runner.State()
	assert.False(t, after.Running)
	assert.Equal(t, assert.AnError.Error(), after.Error)

	_, reusable := runner.begin(context.Background(), "resolve")
	assert.True(t, reusable, "the runner is reusable after finishing")
	assert.Empty(t, runner.State().Error, "a new run starts clean")
}

func TestAStepWithNobodyListeningStillCounts(t *testing.T) {
	runner := newAgentRunner(t.TempDir(), "", "", "", "", sandboxConfig{}, nil)
	_, ok := runner.begin(context.Background(), "resolve")
	require.True(t, ok)

	// The zero runner a test reaches for has no server behind it. A step is
	// still what keeps the watchdog off the run.
	assert.NotPanics(t, func() { runner.step("Edit auth.go") })
	runner.mu.Lock()
	spoken := runner.spoken
	runner.mu.Unlock()
	assert.True(t, spoken)
}

func TestApplyOutcomes(t *testing.T) {
	s := newTestServer(t)
	fixed := createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"extract a const"}`)
	declined := createThread(t, s, `{"file":"a.go","side":"left","line":3,"body":"restore this"}`)
	silent := createThread(t, s, `{"file":"a.go","side":"right","line":1,"body":"no verdict for me"}`)

	_, ok := s.agent.begin(context.Background(), "resolve")
	require.True(t, ok)
	s.applyOutcomes(s.ws.store.Unresolved(), []resolve.Outcome{
		{ID: fixed.ID, Status: resolve.StatusFixed, Note: "extracted minTokenLen"},
		{ID: declined.ID, Status: resolve.StatusDeclined, Note: "the removal was deliberate"},
	})
	s.agent.finish(nil)

	t.Run("fixed closes the thread and records the note", func(t *testing.T) {
		got, err := s.ws.store.Get(fixed.ID)
		require.NoError(t, err)
		assert.True(t, got.Resolved)
		require.Len(t, got.Comments, 2)
		assert.Equal(t, comments.AuthorAgent, got.Comments[1].Author)
		assert.Equal(t, "extracted minTokenLen", got.Comments[1].Body)
		assert.Equal(t, resolve.StatusFixed, got.Comments[1].Outcome)
	})

	t.Run("declined replies but leaves the thread open", func(t *testing.T) {
		got, err := s.ws.store.Get(declined.ID)
		require.NoError(t, err)
		assert.False(t, got.Resolved, "a disagreement must stay open for the user to answer")
		require.Len(t, got.Comments, 2)
		assert.Equal(t, "the removal was deliberate", got.Comments[1].Body)
		// The note alone reads like a fix, so the verdict is carried beside it.
		assert.Equal(t, resolve.StatusDeclined, got.Comments[1].Outcome)
	})

	t.Run("a thread with no verdict is untouched", func(t *testing.T) {
		got, err := s.ws.store.Get(silent.ID)
		require.NoError(t, err)
		assert.False(t, got.Resolved)
		assert.Len(t, got.Comments, 1)
	})
}

func TestCancelStopsARun(t *testing.T) {
	s := newTestServer(t)

	t.Run("nothing running", func(t *testing.T) {
		rec := do(t, s, "POST", "/api/cancel", "")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "nothing is running")
	})

	runCtx, ok := s.agent.begin(context.Background(), "resolve")
	require.True(t, ok)

	t.Run("cancels the in-flight run", func(t *testing.T) {
		rec := do(t, s, "POST", "/api/cancel", "")
		assert.Equal(t, http.StatusNoContent, rec.Code)

		select {
		case <-runCtx.Done():
		case <-time.After(time.Second):
			t.Fatal("the run context should have been cancelled")
		}
	})

	s.agent.finish(runCtx.Err())
	assert.False(t, s.agent.State().Running)
}

func TestRunStateRecordsStartTime(t *testing.T) {
	runner := newAgentRunner(t.TempDir(), "", "", "", "", sandboxConfig{}, nil)
	_, ok := runner.begin(context.Background(), "review")
	require.True(t, ok)

	assert.WithinDuration(t, time.Now(), runner.State().StartedAt, 2*time.Second)
}

func TestTranscriptSizeIsZeroWithoutASession(t *testing.T) {
	assert.Zero(t, transcriptSize(t.TempDir(), ""))
	assert.Zero(t, transcriptSize(t.TempDir(), "no-such-session"))
}

// A run log written before the rename says "pi", and the session it names is
// not resumable by the new driver — but the provider it meant is codex.
func TestRememberedPiSessionUsesACodexModel(t *testing.T) {
	mem := runlog.Open(t.TempDir() + "/runlog.json")
	require.NoError(t, mem.SetAgentSession("pi-session", "pi"))

	runner := newProviderAgentRunner(t.TempDir(), "", "", "", "", "claude-opus-5[1m]", "", sandboxConfig{}, mem)
	assert.Equal(t, "codex", runner.State().Provider)
	assert.Equal(t, "gpt-5.6-terra", runner.State().Model)
	assert.Empty(t, runner.State().Session, "a pi thread id is not a codex thread id")
}

// The Codex driver has no per-turn tool allowlist, so this mapping is the
// whole of what makes a reviewer a reviewer.
func TestReadOnlyToolsMarksAReviewer(t *testing.T) {
	assert.True(t, readOnlyTools([]string{"Read", "Grep", "Glob"}))
	assert.True(t, readOnlyTools([]string{}), "no tools at all cannot write")
	assert.False(t, readOnlyTools([]string{"Read", "Edit"}))
	assert.False(t, readOnlyTools([]string{"Read", "Write"}))
	assert.False(t, readOnlyTools([]string{"Read", "MultiEdit"}))
	assert.False(t, readOnlyTools([]string{"Read", "NotebookEdit"}))
	assert.False(t, readOnlyTools([]string{"Read", "Bash"}), "a shell writes")
	assert.False(t, readOnlyTools(nil), "nil is Claude Code's whole set")
}

func TestCodexKeepsTheSelectedSandbox(t *testing.T) {
	runner := newProviderAgentRunner(t.TempDir(), "", "", "", "codex", "gpt-5.6-terra", "", sandboxConfig{On: true}, nil)
	assert.True(t, runner.State().Sandbox)
}

func TestPermissionMode(t *testing.T) {
	// Bypass only behind the gate. Claude Code lets a call through when an
	// http hook goes unanswered, so the gate is `ade hook`, which answers.
	gated := &agentRunner{hookURL: "http://127.0.0.1:1" + HookPath, hookToken: "tok"}
	assert.Equal(t, "bypassPermissions", gated.permissionMode())
	assert.Equal(t, gated.hookURL, gated.hookFor())
	assert.NotEmpty(t, gated.hookBin())
	assert.Equal(t, []string{gate.URLVar + "=" + gated.hookURL, gate.TokenVar + "=tok"}, gated.hookEnv())

	// Nothing else bypasses, the container least of all: it cannot reach the
	// gate, and it holds the repository rw, the credentials and a network.
	assert.Equal(t, "acceptEdits", (&agentRunner{}).permissionMode())
	boxed := &agentRunner{sandbox: true, hookURL: gated.hookURL}
	assert.Equal(t, "acceptEdits", boxed.permissionMode())
	assert.Empty(t, boxed.hookFor())
	assert.Empty(t, boxed.hookBin())
	assert.Empty(t, boxed.hookEnv())
}

func TestTheEndOfASandboxedTurnWritesTheLoginBack(t *testing.T) {
	// A container that refreshed mid-turn holds the only chain that works.
	// Waiting for the next run means waiting for the login prompt first.
	var synced int
	old := sandboxSync
	sandboxSync = func(context.Context, string) { synced++ }
	t.Cleanup(func() { sandboxSync = old })

	host := newAgentRunner(t.TempDir(), "", "", "", "", sandboxConfig{}, nil)
	host.finish(nil)
	assert.Zero(t, synced, "nothing runs in a container, nothing to write back")

	boxed := newAgentRunner(t.TempDir(), "", "", "", "", sandboxConfig{On: true}, nil)
	boxed.finish(nil)
	assert.Equal(t, 1, synced)
}

// Resolve all means what is still waiting. A declined thread stays open for
// you to answer, but re-sending it asks the resolver the same question again.
func TestResolveAllSkipsWhatTheResolverAlreadyAnswered(t *testing.T) {
	s := newTestServer(t)
	waiting := createThread(t, s, `{"file":"a.go","side":"right","line":1,"body":"unanswered"}`)
	declined := createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"restore this"}`)

	_, err := s.ws.store.ReplyOutcome(declined.ID, comments.AuthorAgent, "deliberate", resolve.StatusDeclined)
	require.NoError(t, err)

	t.Run("resolve all leaves the answered one out", func(t *testing.T) {
		got := s.threadsToResolve(nil)
		require.Len(t, got, 1)
		assert.Equal(t, waiting.ID, got[0].ID)
	})

	t.Run("naming it still sends it", func(t *testing.T) {
		got := s.threadsToResolve([]string{declined.ID})
		require.Len(t, got, 1)
		assert.Equal(t, declined.ID, got[0].ID)
	})

	t.Run("your reply puts it back in the queue", func(t *testing.T) {
		_, err := s.ws.store.Reply(declined.ID, comments.AuthorYou, "no, do it anyway")
		require.NoError(t, err)
		assert.Len(t, s.threadsToResolve(nil), 2)
	})
}

// askingAgent is a busyAgent paused on a question.
type askingAgent struct {
	busyAgent
	answers    map[string]map[string]string
	decided    []string
	allowed    bool
	remembered string
}

func (a *askingAgent) Decide(id string, allow bool, remember, reason string) error {
	a.decided = []string{id, reason}
	a.allowed, a.remembered = allow, remember
	a.answers = map[string]map[string]string{}
	return nil
}

func (a *askingAgent) Pending(id string) (string, bool) {
	return "Bash", len(a.answers) == 0
}

func (a *askingAgent) Asking() []bridge.Ask {
	if len(a.answers) > 0 {
		return nil
	}
	return []bridge.Ask{{ToolID: "toolu_1", Tool: "Bash", Rule: "Bash(ls)"}}
}

func (a *askingAgent) Answer(id string, answers map[string]string) error {
	a.answers = map[string]map[string]string{id: answers}
	return nil
}

func TestAQuestionIsAnsweredOnlyWhileTheRunIsInFlight(t *testing.T) {
	s := newTestServer(t)
	agent := &askingAgent{busyAgent: busyAgent{busy: true}}
	s.agent.agent = agent

	body := `{"toolId":"toolu_1","answers":{"Tea or coffee?":"Coffee"}}`
	rec := do(t, s, "POST", "/api/chat/answer", body)
	assert.Equal(t, http.StatusConflict, rec.Code, "nothing is running to answer")

	_, ok := s.agent.begin(context.Background(), kindChat)
	require.True(t, ok)
	// The page needs more than a count: the id marks the card still open, and
	// the rule says what remembering would write.
	assert.Equal(t, []bridge.Ask{{ToolID: "toolu_1", Tool: "Bash", Rule: "Bash(ls)"}}, s.agent.State().Asking)

	rec = do(t, s, "POST", "/api/chat/answer", `{"toolId":"toolu_1","answers":{}}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = do(t, s, "POST", "/api/chat/answer", body)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, map[string]string{"Tea or coffee?": "Coffee"}, agent.answers["toolu_1"])
	assert.Empty(t, s.agent.State().Asking, "answered, so the run is working again")
}

// Denying is the useful half: the reason reaches the model, so it can do
// something else rather than only learn that it failed.
func TestAHeldToolCallIsAllowedOrDeniedWithAReason(t *testing.T) {
	s := newTestServer(t)
	agent := &askingAgent{busyAgent: busyAgent{busy: true}}
	s.agent.agent = agent

	rec := do(t, s, "POST", "/api/chat/decide", `{"toolId":"toolu_1","allow":true}`)
	assert.Equal(t, http.StatusConflict, rec.Code, "nothing is running to decide")

	_, ok := s.agent.begin(context.Background(), kindChat)
	require.True(t, ok)

	rec = do(t, s, "POST", "/api/chat/decide", `{"allow":true}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "a decision needs a tool call")

	rec = do(t, s, "POST", "/api/chat/decide", `{"toolId":"toolu_1","allow":false,"reason":"not on main"}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"toolu_1", "not on main"}, agent.decided)
	assert.False(t, agent.allowed)
	assert.Empty(t, agent.remembered, "a denial is never remembered")
}

// How long an allow lasts is the user's choice, and an unknown scope is
// refused rather than quietly treated as one of the two that exist.
func TestAnAllowSaysHowLongItLasts(t *testing.T) {
	s := newTestServer(t)
	agent := &askingAgent{busyAgent: busyAgent{busy: true}}
	s.agent.agent = agent
	_, ok := s.agent.begin(context.Background(), kindChat)
	require.True(t, ok)

	for _, scope := range []string{"session", "always"} {
		rec := do(t, s, "POST", "/api/chat/decide",
			`{"toolId":"toolu_1","allow":true,"remember":"`+scope+`"}`)
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		assert.True(t, agent.allowed)
		assert.Equal(t, scope, agent.remembered)
	}

	rec := do(t, s, "POST", "/api/chat/decide", `{"toolId":"toolu_1","allow":true,"remember":"forever"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "an unknown scope is not a scope")
}

func TestRunStateSaysWhatTheAgentIsDoing(t *testing.T) {
	runner := newAgentRunner(t.TempDir(), "sess-1", "", "", "", sandboxConfig{}, nil)
	_, ok := runner.begin(context.Background(), "chat")
	require.True(t, ok)

	runner.noteTool("Bash: git diff")
	runner.noteTouched("internal/web/agent.go")
	runner.noteTouched("internal/web/agent.go")
	runner.noteTouched("vscode/src/runstate.ts")
	state := runner.State()
	assert.Equal(t, "Bash: git diff", state.Doing)
	assert.Equal(t, []string{"internal/web/agent.go", "vscode/src/runstate.ts"}, state.Touched, "each file once")

	runner.finish(nil)
	_, again := runner.begin(context.Background(), "chat")
	require.True(t, again)
	fresh := runner.State()
	assert.Empty(t, fresh.Doing, "a new run starts with nothing doing")
	assert.Empty(t, fresh.Touched)
}

// A turn the agent starts itself — a background task came back — reaches the
// runner as events with no Send around them. The run opens on the first and
// closes on the result, so the page says busy rather than quiet.
func TestATurnTheAgentStartsItselfIsARun(t *testing.T) {
	runner := newAgentRunner(t.TempDir(), "", "", "", "", sandboxConfig{}, nil)
	runner.agent = &busyAgent{}
	var notes []string
	runner.onStep = func(text string) { notes = append(notes, text) }
	ended := 0
	runner.onWake = func(error) { ended++ }
	ctx := context.Background()

	assert.False(t, runner.wake(ctx, bridge.Event{Type: "assistant", Text: "hi"}))
	state := runner.State()
	assert.True(t, state.Running)
	assert.Equal(t, kindWake, state.Kind)
	assert.Equal(t, []string{"carrying on by itself"}, notes)

	assert.False(t, runner.wake(ctx, bridge.Event{Type: "assistant", Text: "more"}))
	assert.Len(t, notes, 1, "one run, said once")

	assert.True(t, runner.wake(ctx, bridge.Event{Type: "result", Result: "done"}))
	assert.False(t, runner.State().Running)
	assert.Empty(t, runner.State().Error)
	assert.Equal(t, 1, ended)

	assert.False(t, runner.wake(ctx, bridge.Event{Type: "assistant", Text: "again"}))
	assert.True(t, runner.wake(ctx, bridge.Event{Type: "result", IsError: true, Result: "boom"}))
	assert.Equal(t, "boom", runner.State().Error)
}

func TestAResultWithNoWakeOpenEndsNothing(t *testing.T) {
	runner := newAgentRunner(t.TempDir(), "", "", "", "", sandboxConfig{}, nil)
	_, ok := runner.begin(context.Background(), kindChat)
	require.True(t, ok)
	assert.True(t, runner.wake(context.Background(), bridge.Event{Type: "result", Result: "x"}))
	state := runner.State()
	assert.True(t, state.Running, "a sent turn's result reaches Send, never here")
	assert.Equal(t, kindChat, state.Kind)
	runner.finish(nil)
}

func TestCancellingAWakeEndsIt(t *testing.T) {
	runner := newAgentRunner(t.TempDir(), "", "", "", "", sandboxConfig{}, nil)
	runner.agent = &busyAgent{}
	runner.wake(context.Background(), bridge.Event{Type: "assistant", Text: "hi"})
	require.True(t, runner.Cancel())
	assert.Eventually(t, func() bool { return !runner.State().Running }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, "you cancelled the run", runner.State().Error)
}

// deadAgent is a bridge.Agent whose process has gone.
type deadAgent struct{ busyAgent }

func (*deadAgent) Dead() bool { return true }

func TestAWakeEndsWhenTheAgentDies(t *testing.T) {
	runner := newAgentRunner(t.TempDir(), "", "", "", "", sandboxConfig{}, nil)
	runner.agent = &deadAgent{}
	runner.wakePoll = 10 * time.Millisecond
	runner.wake(context.Background(), bridge.Event{Type: "assistant", Text: "hi"})
	assert.Eventually(t, func() bool { return !runner.State().Running }, 2*time.Second, 10*time.Millisecond)
	assert.Contains(t, runner.State().Error, "exited")
}

// A wake spends the subscription and edits the tree as any turn does, so it
// is bracketed the same way: the tree is noted as it opens, and what it cost
// settled when it ends.
func TestAWakeIsBracketedLikeATurn(t *testing.T) {
	runner := newAgentRunner(t.TempDir(), "", "", "", "", sandboxConfig{}, nil)
	runner.agent = &busyAgent{}
	var order []string
	runner.onWakeStart = func() { order = append(order, "start") }
	runner.onWake = func(err error) {
		order = append(order, fmt.Sprintf("end %v", err))
	}
	ctx := context.Background()

	runner.wake(ctx, bridge.Event{Type: "assistant", Text: "hi"})
	assert.Equal(t, []string{"start"}, order, "the tree is noted before it works")

	runner.wake(ctx, bridge.Event{Type: "result", IsError: true, Result: "boom"})
	assert.Equal(t, []string{"start", "end boom"}, order, "the failure reaches the bookkeeping")
}

func TestAFinishedTurnSaysWhatItDid(t *testing.T) {
	runner := newAgentRunner(t.TempDir(), "", "", "", "", sandboxConfig{}, nil)
	_, ok := runner.begin(context.Background(), kindChat)
	require.True(t, ok)
	assert.Nil(t, runner.State().Finished, "nothing has finished yet")

	runner.finish(nil)
	runner.finished(&Finished{Files: 3, At: time.Now()})
	assert.Equal(t, 3, runner.State().Finished.Files)

	_, ok = runner.begin(context.Background(), kindChat)
	require.True(t, ok)
	assert.Nil(t, runner.State().Finished, "the next turn is about to make it wrong")
}

func TestTheLastWordDecidesWhetherATurnIsAsking(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), kindChat)
	require.True(t, ok)
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "Two ways to do it.\n\nWhich?\n\n- a\n- b\n- c\n- d\n- e\n- f"})
	s.agent.finish(nil)

	s.noteFinished(kindChat)
	require.NotNil(t, s.agent.State().Finished)
	assert.True(t, s.agent.State().Finished.Asking, "a question with six options is still a question")

	s.say(ChatMessage{Kind: chat.KindAgent, Body: "Done. Tests pass."})
	s.noteFinished(kindChat)
	assert.False(t, s.agent.State().Finished.Asking)

	// A named agent's reply is its own; the main agent's last word is the turn's.
	s.say(ChatMessage{Kind: chat.KindAgent, Author: "security", Body: "Shall I look again?"})
	s.noteFinished(kindChat)
	assert.False(t, s.agent.State().Finished.Asking)

	s.agent.finished(nil)
	s.noteFinished(kindReview)
	assert.Nil(t, s.agent.State().Finished, "a review reports in its own place")
}

func TestWhatACancelSaysItTook(t *testing.T) {
	assert.Equal(t, "cancelled", cancelLine(0, 0))
	assert.Equal(t, "cancelled, and dropped 2 queued message(s)", cancelLine(2, 0))
	assert.Equal(t, "cancelled, and stopped 1 sub-agent(s)", cancelLine(0, 1))
	assert.Equal(t, "cancelled, and dropped 2 queued message(s), stopped 1 sub-agent(s)", cancelLine(2, 1))
}

// Busy and stuck look the same until the same call keeps being the one in
// hand. Anything else between is progress, however small.
func TestTheSameCallOverAndOverIsCounted(t *testing.T) {
	runner := newAgentRunner(t.TempDir(), "", "", "", "", sandboxConfig{}, nil)
	_, ok := runner.begin(context.Background(), kindChat)
	require.True(t, ok)

	runner.noteTool("Bash: go test ./...")
	runner.noteTool("Bash: go test ./...")
	runner.noteTool("Bash: go test ./...")
	state := runner.State()
	assert.Equal(t, 3, state.Repeats)
	assert.False(t, state.RepeatedAt.IsZero())
	first := state.RepeatedAt

	runner.noteTool("Edit: agent.go")
	assert.Equal(t, 1, runner.State().Repeats, "something else is progress")
	assert.True(t, runner.State().RepeatedAt.After(first) || runner.State().RepeatedAt.Equal(first))

	runner.noteTool("Bash: go test ./...")
	assert.Equal(t, 1, runner.State().Repeats, "the count is consecutive, not a tally")

	runner.finish(nil)
	assert.Zero(t, runner.State().Repeats, "a run that is over is not going round")
}

// ade said "is running in a container" before the container existed, and the
// pull then failed for want of a ghcr login. The step says which it is doing.
func TestTheContainerStepDoesNotClaimAContainerItHasNotGot(t *testing.T) {
	assert.Equal(t, "claude is running in a container, via container",
		containerStep("claude", "container", true))

	fetching := containerStep("claude", "docker", false)
	assert.NotContains(t, fetching, "is running in a container")
	assert.Contains(t, fetching, "fetching the sandbox image")
	assert.Contains(t, fetching, "make sandbox-image")
}
