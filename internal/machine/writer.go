package machine

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/owenrumney/ade/internal/sessions"
)

// A hang only matters while something is supposed to be moving.
const (
	busyEvery = 30 * time.Second
	idleEvery = 5 * time.Minute
)

// failedReason stands in for the agent's error text, which the snapshot does
// not carry; the run it belongs to has the whole of it.
const failedReason = "the last run failed; open the session for what it said"

// Source is a process that can produce the snapshot: the launcher, or a
// checkout's own server.
type Source struct {
	Dir    string
	Writer Writer
	// Repos is what this process can see; a nil Probe is the real one.
	Repos func() ([]sessions.Repo, error)
	Probe sessions.Prober
}

// Loop writes the snapshot now, then at the cadence the last graph called for.
// The first one is immediate: a reader a second after start deserves an answer.
func (s Source) Loop(ctx context.Context) {
	if s.Dir == "" || s.Repos == nil {
		return
	}
	timer := time.NewTimer(every(s.Tick(ctx)))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			timer.Reset(every(s.Tick(ctx)))
		}
	}
}

// every is how long until the next tick.
func every(working bool) time.Duration {
	if working {
		return busyEvery
	}
	return idleEvery
}

// Tick takes the lease and writes one snapshot, reporting whether an agent
// was running in it. A refused tick probes nothing and reads the held one.
func (s Source) Tick(ctx context.Context) bool {
	if !Claim(s.Dir, s.Writer, time.Now()) {
		held, _ := Read(s.Dir)
		return busy(held.Repos)
	}
	repos, err := s.Repos()
	if err != nil {
		// A failed scan is not an idle machine: hold the last known cadence.
		slog.Debug("machine snapshot: no repositories", "err", err)
		held, _ := Read(s.Dir)
		return busy(held.Repos)
	}
	graph := sessions.Graph(ctx, repos, s.Probe)
	redact(graph)
	host, _ := os.Hostname()
	snap := Machine{WrittenAt: time.Now(), Writer: s.Writer, Host: host, Repos: graph}
	if err := Write(s.Dir, snap); err != nil {
		slog.Debug("machine snapshot: not written", "err", err)
	}
	return busy(graph)
}

// redact drops the agent's own words. A failure carries the tail of the
// subprocess's stderr, which is where a delegate token would surface.
func redact(repos []sessions.GraphRepo) {
	for _, repo := range repos {
		for _, dir := range repo.Checkouts {
			if dir.Server != nil && dir.Server.Agent != nil && dir.Server.Agent.Error != "" {
				dir.Server.Agent.Error = failedReason
			}
		}
	}
}

// busy is whether any checkout has an agent running.
func busy(repos []sessions.GraphRepo) bool {
	for _, repo := range repos {
		for _, dir := range repo.Checkouts {
			if dir.Server != nil && dir.Server.Agent != nil && dir.Server.Agent.Running {
				return true
			}
		}
	}
	return false
}
