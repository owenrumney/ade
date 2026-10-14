package web

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/spawncap"
)

// spawnWatch is how an agent's processes are counted: how often, how long a
// cancelled turn gets to take them with it, and with what. Zero is the real one.
type spawnWatch struct {
	every time.Duration
	grace time.Duration
	count func(context.Context, int) (int, error)
}

func (w spawnWatch) orDefault() spawnWatch {
	if w.count == nil {
		return spawnWatch{every: 5 * time.Second, grace: 10 * time.Second, count: spawncap.Count}
	}
	return w
}

// watchSpawn cancels a turn whose agent holds too many processes. Two counts
// running, so a burst that ends by itself is left alone.
func (a *agentRunner) watchSpawn(ctx context.Context, cancel context.CancelFunc) {
	w := a.spawn.orDefault()
	ticker := time.NewTicker(w.every)
	defer ticker.Stop()
	over := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			group := a.group()
			if group == 0 {
				continue
			}
			n, err := w.count(ctx, group)
			if err != nil || n <= spawncap.Limit {
				over = 0
				continue
			}
			if over++; over < 2 {
				continue
			}
			reason := fmt.Sprintf("the agent had %d processes running, over the limit of %d, so the turn was cancelled", n, spawncap.Limit)
			a.mu.Lock()
			a.stopped = reason
			a.mu.Unlock()
			slog.Warn("stopping the agent", "why", reason)
			cancel()
			go a.reap(w, group)
			return
		}
	}
}

// reap kills an agent whose processes outlived the cancel. The conversation
// stays: the next message starts a new agent in it.
func (a *agentRunner) reap(w spawnWatch, group int) {
	time.Sleep(w.grace)
	n, err := w.count(context.Background(), group)
	if err != nil || n <= spawncap.Limit {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.groupLocked() != group {
		return
	}
	slog.Warn("killing the agent's processes", "count", n, "group", group)
	a.dropKeepingSession()
}

// group is the process group of the agent on the host, or zero: a sandboxed
// agent leads none here.
func (a *agentRunner) group() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.groupLocked()
}

func (a *agentRunner) groupLocked() int {
	agent, ok := a.agent.(bridge.Grouped)
	if !ok {
		return 0
	}
	return agent.Group()
}
