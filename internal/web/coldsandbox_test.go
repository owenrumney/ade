package web

import (
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"testing"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/cleanse"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// boxed turns the sandbox on with doubles for the runtime and the login, and
// hands back every OneShotOpts a cold run made.
func boxed(t *testing.T, s *Server) *[]bridge.OneShotOpts {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	oldDetect, oldLogin, oldRun := detectRuntime, sandboxLogin, runOneShot
	detectRuntime = func(context.Context) (sandbox.Runtime, error) { return noRuntime{}, nil }
	sandboxLogin = func(_ context.Context, home string) (string, error) { return filepath.Join(home, "creds.json"), nil }
	seen := &[]bridge.OneShotOpts{}
	runOneShot = func(_ context.Context, opts bridge.OneShotOpts) (*bridge.Turn, error) {
		*seen = append(*seen, opts)
		return &bridge.Turn{Text: "[]"}, nil
	}
	t.Cleanup(func() { detectRuntime, sandboxLogin, runOneShot = oldDetect, oldLogin, oldRun })
	s.agent.setSandbox(true)
	return seen
}

func TestAColdRunGoesInTheContainerTheSessionGets(t *testing.T) {
	s := newTestServer(t)
	seen := boxed(t, s)

	_, err := s.coldClaude(context.Background(), bridge.OneShotOpts{Prompt: "hello"})
	require.NoError(t, err)
	require.Len(t, *seen, 1)
	spec := (*seen)[0].Sandbox
	require.NotNil(t, spec, "the run went to the host with the sandbox on")
	assert.NotNil(t, (*seen)[0].Runtime)
	assert.Contains(t, spec.Name, "-cold", "beside the session's container, not over it")
	assert.NotEqual(t, sandbox.ContainerName(s.space().homeRoot), spec.Name)
}

func TestConcurrentColdRunsDoNotShareAContainerName(t *testing.T) {
	s := newTestServer(t)
	boxed(t, s)

	// Every run is held until the last one has its name, so the slots really
	// are claimed at once rather than reused one after another.
	var mu sync.Mutex
	names, started := map[string]bool{}, 0
	all := make(chan struct{})
	runOneShot = func(_ context.Context, opts bridge.OneShotOpts) (*bridge.Turn, error) {
		mu.Lock()
		names[opts.Sandbox.Name] = true
		started++
		last := started == sandbox.ColdSlots
		mu.Unlock()
		if last {
			close(all)
		}
		<-all
		return &bridge.Turn{Text: "[]"}, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < sandbox.ColdSlots; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.coldClaude(context.Background(), bridge.OneShotOpts{Prompt: "hello"})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
	assert.Len(t, names, sandbox.ColdSlots, "a name taken twice stops the run that holds it")
}

func TestAColdRunIsRefusedWhenEveryContainerNameIsTaken(t *testing.T) {
	s := newTestServer(t)
	boxed(t, s)
	for slot := 0; slot < sandbox.ColdSlots; slot++ {
		_, ok := s.agent.takeColdSlot()
		require.True(t, ok)
	}
	_, err := s.coldClaude(context.Background(), bridge.OneShotOpts{Prompt: "hello"})
	require.Error(t, err, "reusing a live container's name would kill it")
}

func TestOffTheSandboxAColdRunIsStillTheHost(t *testing.T) {
	s := newTestServer(t)
	seen := boxed(t, s)
	s.agent.setSandbox(false)

	_, err := s.coldClaude(context.Background(), bridge.OneShotOpts{Prompt: "hello"})
	require.NoError(t, err)
	require.Len(t, *seen, 1)
	assert.Nil(t, (*seen)[0].Sandbox)
}

// A named agent is a cold run, so it is contained like any other.
func TestANamedAgentRunsInTheContainer(t *testing.T) {
	s := newTestServer(t)
	seen := boxed(t, s)
	defineUserAgent(t, "security", "---\nname: security\n---\ntrust nothing\n")

	rec := do(t, s, "POST", "/api/review", `{"agent":"security"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Eventually(t, func() bool { return len(s.sideRuns()) == 0 }, 5e9, 1e7)

	require.Len(t, *seen, 1)
	require.NotNil(t, (*seen)[0].Sandbox)
	assert.Equal(t, "security", (*seen)[0].Agent)
}

// A cheap model with no tools reads no checkout. Boxing it bought nothing and
// cost a VM boot inside the call's own budget, which is what killed it.
func TestACheapRunStaysOnTheHostWithTheSandboxOn(t *testing.T) {
	s := newTestServer(t)
	seen := boxed(t, s)

	_, err := s.cheapRunner(context.Background(), cleanse.Model, cleanse.Role(), "rewrite this")
	require.NoError(t, err)
	require.Len(t, *seen, 1)
	assert.Nil(t, (*seen)[0].Sandbox, "a rewrite booted a container to read a reply it was handed")
	assert.Nil(t, (*seen)[0].Runtime)
	assert.Equal(t, []string{}, (*seen)[0].Tools)

	// Every cold slot is still free: a named agent is not waiting behind a rewrite.
	for slot := 0; slot < sandbox.ColdSlots; slot++ {
		_, ok := s.agent.takeColdSlot()
		require.True(t, ok)
	}
}
