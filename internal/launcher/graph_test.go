package launcher

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/owenrumney/ade/internal/session"
)

func TestTheGraphEndpointNeedsTheToken(t *testing.T) {
	s := newTestLauncher(t)
	req := do(t, s, "GET", "/api/graph?t=wrong", "")
	assert.Equal(t, http.StatusForbidden, req.Code)
}

func TestTheLiveKeyIsOnlyTheCheckoutsWithAServer(t *testing.T) {
	m := newFixture(t)
	m.checkout(t, "live", 27431, true)
	m.checkout(t, "stale", 25000, false)
	m.checkout(t, "never-opened", 0, false)
	s := newLauncherOn(t, m)

	key := s.liveKey(context.Background())
	assert.Contains(t, key, filepath.Join(m.code, "live"))
	assert.NotContains(t, key, filepath.Join(m.code, "stale"),
		"a record whose server is gone is not something running")
	assert.NotContains(t, key, filepath.Join(m.code, "never-opened"))

	// The key has to move when the agent starts working, or the sweep sees no
	// change and says nothing.
	assert.Equal(t, key, s.liveKey(context.Background()), "an unchanged fixture keeps its key")
	s.probe = func(_ context.Context, rec *session.Record) (session.Health, bool) {
		health, live := m.prober()(context.Background(), rec)
		if live {
			health.Agent = &session.AgentHealth{Running: true, Kind: "chat"}
		}
		return health, live
	}
	assert.NotEqual(t, key, s.liveKey(context.Background()))
}

func TestNobodyWatchingIsNobodyToTell(t *testing.T) {
	m := newFixture(t)
	m.checkout(t, "live", 27431, true)
	s := newLauncherOn(t, m)
	s.liveEvery = time.Millisecond

	// With no client on the hub, a sweep is probes spent on nobody.
	probes := 0
	s.probe = func(ctx context.Context, rec *session.Record) (session.Health, bool) {
		probes++
		return m.prober()(ctx, rec)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	s.watchLive(ctx)
	assert.Zero(t, probes, "an unwatched launcher swept anyway")
}
