package web

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/spawncap"
)

// groupedAgent is an agent that leads a process group and counts its closes.
type groupedAgent struct {
	bridge.Agent
	group  int
	closed atomic.Int32
}

func (g *groupedAgent) Group() int { return g.group }

func (g *groupedAgent) Close() error {
	g.closed.Add(1)
	return nil
}

// spawning is a runner whose watchdog is quick and counts what it is told.
func spawning(agent *groupedAgent, count func() int) *agentRunner {
	return &agentRunner{agent: agent, spawn: spawnWatch{
		every: 5 * time.Millisecond,
		grace: 20 * time.Millisecond,
		count: func(context.Context, int) (int, error) { return count(), nil },
	}}
}

func TestAnAgentOverTheProcessLimitHasItsTurnCancelled(t *testing.T) {
	var calm atomic.Bool
	agent := &groupedAgent{group: 4242}
	a := spawning(agent, func() int {
		if calm.Load() {
			return 3
		}
		return spawncap.Limit + 8
	})

	ctx, ok := a.begin(context.Background(), "chat")
	require.True(t, ok)
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the turn was never cancelled")
	}
	// The cancel took the processes with it, so the agent is left running.
	calm.Store(true)
	a.finish(ctx.Err())

	assert.Contains(t, a.State().Error, "40 processes running, over the limit of 32")
	time.Sleep(100 * time.Millisecond)
	assert.Zero(t, agent.closed.Load())
}

func TestProcessesThatOutliveTheCancelAreKilled(t *testing.T) {
	agent := &groupedAgent{group: 4242}
	a := spawning(agent, func() int { return spawncap.Limit + 1 })

	ctx, ok := a.begin(context.Background(), "chat")
	require.True(t, ok)
	<-ctx.Done()

	assert.Eventually(t, func() bool { return agent.closed.Load() == 1 }, 3*time.Second, 5*time.Millisecond)
}

func TestABurstThatEndsIsLeftAlone(t *testing.T) {
	var calls atomic.Int32
	a := spawning(&groupedAgent{group: 4242}, func() int {
		if calls.Add(1) == 1 {
			return spawncap.Limit + 50
		}
		return 4
	})

	ctx, ok := a.begin(context.Background(), "chat")
	require.True(t, ok)
	require.Eventually(t, func() bool { return calls.Load() > 4 }, 3*time.Second, 5*time.Millisecond)
	assert.NoError(t, ctx.Err())
	a.finish(nil)
}

func TestASandboxedAgentIsNotCounted(t *testing.T) {
	var calls atomic.Int32
	a := spawning(&groupedAgent{}, func() int { return int(calls.Add(1)) + spawncap.Limit })

	ctx, ok := a.begin(context.Background(), "chat")
	require.True(t, ok)
	time.Sleep(50 * time.Millisecond)
	assert.NoError(t, ctx.Err())
	assert.Zero(t, calls.Load())
	a.finish(nil)
}
