package web

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/runlog"
)

func TestRunStateTracksProgress(t *testing.T) {
	runner := newAgentRunner(t.TempDir(), "sess-123", "", "", "", nil)

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
	runner := newAgentRunner(t.TempDir(), "", "", "", "", nil)
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
	runner := newAgentRunner(t.TempDir(), "", "", "", "", nil)
	_, ok := runner.begin(context.Background(), "review")
	require.True(t, ok)

	assert.WithinDuration(t, time.Now(), runner.State().StartedAt, 2*time.Second)
}

// The scan lists no session for a checkout whose record holds none, so an
// import, or any `ade --session`, vanished on the next start.
func TestAConversationNamedOnTheCommandLineIsRecorded(t *testing.T) {
	gitDir := filepath.Join(t.TempDir(), ".git")
	mem := runlog.Open(runlog.Path(gitDir))

	newAgentRunner(t.TempDir(), "sess-123", "", "claude", "", mem)
	assert.Equal(t, "sess-123", runlog.Read(runlog.Path(gitDir)).Session)

	// "none" asks for a fresh conversation, which there is nothing to record.
	fresh := runlog.Open(runlog.Path(filepath.Join(t.TempDir(), ".git")))
	newAgentRunner(t.TempDir(), sessionNone, "", "claude", "", fresh)
	assert.Empty(t, runlog.Read(fresh.Path()).Session)

	// Nothing named still takes what the checkout was last in.
	runner := newAgentRunner(t.TempDir(), "", "", "claude", "", mem)
	assert.Equal(t, "sess-123", runner.Current())
}
