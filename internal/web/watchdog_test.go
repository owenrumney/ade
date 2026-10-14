package web

import (
	"context"
	"testing"
	"time"

	"errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWorkingRunIsNotStoppedForTakingLong(t *testing.T) {
	a := &agentRunner{}
	ctx, ok := a.begin(context.Background(), "review")
	require.True(t, ok)

	// The old guard was a total budget, so a build that ran past it died even
	// while it was reporting tool calls. Only silence should end a run.
	a.mu.Lock()
	a.state.StartedAt = time.Now().Add(-3 * time.Hour)
	a.lastEvent = time.Now().Add(-3 * time.Hour)
	a.mu.Unlock()

	a.step("Bash go test ./...")

	a.mu.Lock()
	idle := time.Since(a.lastEvent)
	a.mu.Unlock()
	assert.Less(t, idle, time.Second, "a step should reset the idle clock")
	assert.NoError(t, ctx.Err())
}

func TestSilenceEndsARunWithAReason(t *testing.T) {
	a := &agentRunner{}
	ctx, ok := a.begin(context.Background(), "review")
	require.True(t, ok)

	a.mu.Lock()
	a.lastEvent = time.Now().Add(-idleTimeout - time.Minute)
	a.mu.Unlock()

	// The watchdog ticks every 30s, so drive one pass directly rather than
	// waiting for it.
	stopped := make(chan struct{})
	go func() { <-ctx.Done(); close(stopped) }()
	a.mu.Lock()
	a.stopped = "the agent said nothing for 16m0s, so the run was stopped"
	cancel := a.cancel
	a.mu.Unlock()
	cancel()
	<-stopped

	a.finish(ctx.Err())

	// "context deadline exceeded" told you nothing about what happened.
	state := a.State()
	assert.Contains(t, state.Error, "said nothing")
	assert.NotContains(t, state.Error, "context")
}

func TestCancellingSaysSoRatherThanReportingAContextError(t *testing.T) {
	a := &agentRunner{}
	ctx, ok := a.begin(context.Background(), "review")
	require.True(t, ok)

	require.True(t, a.Cancel())
	<-ctx.Done()
	a.finish(ctx.Err())

	assert.Equal(t, "you cancelled the run", a.State().Error)
}

func TestTheReasonDoesNotLeakIntoTheNextRun(t *testing.T) {
	a := &agentRunner{}
	ctx, _ := a.begin(context.Background(), "review")
	require.True(t, a.Cancel())
	<-ctx.Done()
	a.finish(ctx.Err())
	require.Equal(t, "you cancelled the run", a.State().Error)

	_, ok := a.begin(context.Background(), "resolve")
	require.True(t, ok)
	a.finish(assertErr{})
	assert.Equal(t, "something else went wrong", a.State().Error)
}

type assertErr struct{}

func (assertErr) Error() string { return "something else went wrong" }

func TestSilenceBeforeTheFirstEventIsNotWedged(t *testing.T) {
	a := &agentRunner{}
	_, ok := a.begin(context.Background(), kindChat)
	require.True(t, ok)

	// A resumed session replays its whole transcript before it says anything,
	// and stream-json emits nothing during a long reply that calls no tools.
	// Both look exactly like a wedged agent, and killing them destroys real
	// work — which is what happened to an Explore chat.
	a.mu.Lock()
	a.lastEvent = time.Now().Add(-time.Hour)
	quiet := !a.spoken
	a.mu.Unlock()

	require.True(t, quiet, "a run has said nothing before its first event")

	// Once it has spoken, silence means something again.
	a.step("Bash go test ./...")
	a.mu.Lock()
	spoken := a.spoken
	a.mu.Unlock()
	assert.True(t, spoken)
}

func TestWhyExplainsACancelledRun(t *testing.T) {
	a := &agentRunner{}
	ctx, ok := a.begin(context.Background(), kindChat)
	require.True(t, ok)
	require.True(t, a.Cancel())
	<-ctx.Done()

	// "context canceled" in the chat told you nothing about what happened.
	assert.Equal(t, "you cancelled the run", a.why(ctx.Err()))

	// Asked after finish, the reason has gone — which is why callers ask first.
	a.finish(ctx.Err())
	assert.Equal(t, context.Canceled.Error(), a.why(ctx.Err()))

	// An ordinary failure is its own message.
	assert.Equal(t, "gh exploded", a.why(errors.New("gh exploded")))
	assert.Empty(t, a.why(nil))
}
