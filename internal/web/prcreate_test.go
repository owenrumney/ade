package web

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
)

func TestPRCreateQueuesBehindAConcurrentRun(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/pr-create", "")
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `"queued":1`)

	// A button pressed twice still means once.
	rec = do(t, s, "POST", "/api/pr-create", "")
	assert.Contains(t, rec.Body.String(), `"queued":1`)
}

func TestPRCreateRejectsAnExistingPR(t *testing.T) {
	s := newTestServer(t)
	s.ws.pr = &PRInfo{Number: 7}

	rec := do(t, s, "POST", "/api/pr-create", "")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "already PR #7")
}

func TestPRCreateRejectsASnapshot(t *testing.T) {
	s := newTestServer(t)
	s.ws.snapshot = true

	rec := do(t, s, "POST", "/api/pr-create", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "snapshot")
}

func TestPushRejectsASnapshot(t *testing.T) {
	s := newTestServer(t)
	s.ws.snapshot = true

	rec := do(t, s, "POST", "/api/push", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "snapshot")
}

func TestRefreshWorktreeIsSkippedForASnapshot(t *testing.T) {
	s := newTestServer(t)
	s.ws.snapshot = true
	s.worktree = nil

	assert.False(t, s.refreshWorktree(context.Background()))
	assert.Nil(t, s.currentWorktree())
}

func TestRefreshWorktreeReportsOnlyRealChanges(t *testing.T) {
	s := newTestServer(t)
	require.False(t, s.refreshWorktree(context.Background()), "an unchanged worktree is not a change")

	require.NoError(t, gitdiff.Stage(context.Background(), s.ws.homeRoot, []string{"a.go"}))
	assert.False(t, s.refreshWorktree(context.Background()), "a.go was already committed, so nothing moved")
}

func TestPRCreateRefusesTheBaseBranch(t *testing.T) {
	s := newTestServer(t)

	s.mu.Lock()
	s.diff.Commits = 0
	s.mu.Unlock()

	rec := do(t, s, "POST", "/api/pr-create", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no commits ahead of the base")
}

func TestDiffCountsCommitsAheadOfTheBase(t *testing.T) {
	s := newTestServer(t)

	// The test repo branches feature off main with one commit on top.
	s.mu.RLock()
	defer s.mu.RUnlock()
	assert.Equal(t, 1, s.diff.Commits)
}

func TestPRCreateRefusesWhenTheBranchAlreadyHasOne(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 960, Title: "Reap abandoned executions"})

	rec := do(t, s, "POST", "/api/pr-create", "")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "already has PR #960")
}
