package web

import (
	"context"
	"log/slog"
	"os"
	"sync"

	"github.com/owenrumney/ade/internal/agentbus"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/statedir"
)

// busSession is the connection standing for this checkout's running agent. A
// session on the bus is a connection, so nothing shows unless one is held.
type busSession struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	id     string
}

// busInstanceID is the machine's instance, which the launcher connects under.
func busInstanceID() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return agentbus.InstanceID(statedir.Home(home))
}

// launcherPID is the process the bus joins a session to. Zero when no launcher
// is running, which is also when there is no instance to join.
func launcherPID() int {
	path, err := settings.RecordPath()
	if err != nil {
		return 0
	}
	rec, ok := session.Read(path)
	if !ok || !session.Running(rec.PID) {
		return 0
	}
	return rec.PID
}

// open holds a session connection until close. Calling it for a session
// already held does nothing: one connection per run, not one per event.
func (b *busSession) open(ctx context.Context, id, agent, model string) {
	pid := launcherPID()
	if pid == 0 || id == "" {
		return
	}
	b.mu.Lock()
	if b.id == id && b.cancel != nil {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()
	b.close()

	cfg, err := settings.Load()
	if err != nil || cfg.Agentbus.Off {
		return
	}
	url := cfg.Agentbus.URL
	if url == "" {
		if url, err = agentbus.ResolveURL(ctx); err != nil {
			slog.Debug("no agentbus for this run", "err", err)
			return
		}
	}

	held, cancel := context.WithCancel(context.WithoutCancel(ctx))
	b.mu.Lock()
	b.cancel, b.id = cancel, id
	b.mu.Unlock()

	go agentbus.Run(held, agentbus.Options{
		URL:       url,
		Instance:  agentbus.Instance{ID: busInstanceID(), Harness: "ade"},
		Session:   &agentbus.Session{ID: id, Agent: agent, Model: model},
		Ancestors: []int{pid},
	}, func(*agentbus.Client) {})
}

// close drops the connection, which is what takes the session off `who`.
func (b *busSession) close() {
	b.mu.Lock()
	cancel, id := b.cancel, b.id
	b.cancel, b.id = nil, ""
	b.mu.Unlock()
	if cancel != nil {
		slog.Debug("agentbus session ended", "session", id)
		cancel()
	}
}

// sessionOnBus follows the run: a session appears while an agent is working
// and goes when it stops, so `who` says what is running rather than what exists.
func (s *Server) sessionOnBus(ctx context.Context) {
	run := s.agent.State()
	if !run.Running {
		s.busRun.close()
		return
	}
	id := run.Current
	if id == "" {
		id = run.Session
	}
	s.busRun.open(ctx, id, run.Provider, run.Model)
}
