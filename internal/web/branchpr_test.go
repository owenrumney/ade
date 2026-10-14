package web

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/ghpr"
)

func TestCountFailing(t *testing.T) {
	assert.Zero(t, countFailing(nil))
	assert.Equal(t, 2, countFailing([]ghpr.Check{
		{Bucket: "fail"}, {Bucket: "pass"}, {Bucket: "fail"}, {Bucket: "pending"},
	}))
}

func TestBranchPRChangedIgnoresTheTimestamp(t *testing.T) {
	base := &BranchPR{Number: 7, State: "OPEN", Checks: []ghpr.Check{{Name: "test", Bucket: "pass"}}}

	tests := []struct {
		name  string
		after *BranchPR
		want  bool
	}{
		{name: "identical but re-checked", after: &BranchPR{Number: 7, State: "OPEN",
			Checks: []ghpr.Check{{Name: "test", Bucket: "pass"}}, CheckedAt: time.Now()}},
		{name: "a check started failing", want: true, after: &BranchPR{Number: 7, State: "OPEN",
			Checks: []ghpr.Check{{Name: "test", Bucket: "fail"}}}},
		{name: "a check appeared", want: true, after: &BranchPR{Number: 7, State: "OPEN",
			Checks: []ghpr.Check{{Name: "test", Bucket: "pass"}, {Name: "lint", Bucket: "pass"}}}},
		{name: "merged", want: true, after: &BranchPR{Number: 7, State: "MERGED",
			Checks: []ghpr.Check{{Name: "test", Bucket: "pass"}}}},
		{name: "the pull request went away", want: true, after: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A poll that finds nothing new must not make every browser refetch.
			assert.Equal(t, tt.want, branchPRChanged(base, tt.after))
		})
	}
}

func TestFixChecksNeedsSomethingFailing(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/checks/fix", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no pull request")

	s.setBranchPR(&BranchPR{Number: 7, Checks: []ghpr.Check{{Name: "test", Bucket: "pass"}}})
	rec = do(t, s, "POST", "/api/checks/fix", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "nothing is failing")
}

func TestFailingChecksSelectsOnlyFailures(t *testing.T) {
	got := failingChecks([]ghpr.Check{
		{Name: "a", Bucket: "fail"}, {Name: "b", Bucket: "pass"}, {Name: "c", Bucket: "fail"},
	})
	assert.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Name)
	assert.Equal(t, "c", got[1].Name)
}

func TestStateCarriesTheBranchPR(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, Title: "Cart", Failing: 1,
		Checks: []ghpr.Check{{Name: "test", Bucket: "fail", Link: "https://ci/1"}}})

	rec := do(t, s, "GET", "/api/state", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"branchPr"`)
	assert.Contains(t, rec.Body.String(), "https://ci/1")
}

// "The agent is already running" is a refusal with nothing behind it.
func TestInvestigatingChecksQueuesBehindARunningTurn(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, Checks: []ghpr.Check{{Name: "test", Bucket: "fail"}}})

	_, ok := s.agent.begin(s.ctx, kindChat)
	require.True(t, ok, "something else holds the agent")

	rec := do(t, s, "POST", "/api/checks/fix", "")
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `"queued":1`)

	// A button pressed twice still means once.
	rec = do(t, s, "POST", "/api/checks/fix", "")
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `"queued":1`)

	waiting := s.waiting()
	require.Len(t, waiting, 1)
	assert.Equal(t, "Fix checks", waiting[0].Kind)
}

// The run it waited behind may have fixed them, so it reads them again.
func TestAQueuedInvestigationIsDroppedWhenNothingIsFailing(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, Checks: []ghpr.Check{{Name: "test", Bucket: "fail"}}})

	_, ok := s.agent.begin(s.ctx, kindChat)
	require.True(t, ok)
	rec := do(t, s, "POST", "/api/checks/fix", "")
	require.Equal(t, http.StatusAccepted, rec.Code)

	s.setBranchPR(&BranchPR{Number: 7, Checks: []ghpr.Check{{Name: "test", Bucket: "pass"}}})
	s.agent.finish(nil)
	s.drain()

	assert.Empty(t, s.waiting(), "it was taken off the queue")
	assert.False(t, s.agent.State().Running, "and nothing was started for it")
}
