package web

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
)

func TestOpenPRValidatesTheNumber(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/api/prs/0", "/api/prs/-1", "/api/prs/abc"} {
		rec := do(t, s, "POST", path, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, path)
	}
}

func TestOpenPRWaitsForTheAgent(t *testing.T) {
	s := newTestServer(t)

	// A pull request has to be open for the close path to get as far as the
	// agent check at all.
	s.mu.Lock()
	s.ws.pr = &PRInfo{Number: 7}
	s.mu.Unlock()

	_, ok := s.agent.begin(context.Background(), "build")
	require.True(t, ok)
	defer s.agent.finish(nil)

	// Swapping what the agent is working on, mid-run, is how you get edits
	// applied against a diff nobody is looking at.
	for _, req := range [][2]string{{"POST", "/api/prs/7"}, {"DELETE", "/api/prs"}} {
		rec := do(t, s, req[0], req[1], "")
		assert.Equal(t, http.StatusConflict, rec.Code, req[1])
	}
}

func TestClosePRNeedsOneOpen(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "DELETE", "/api/prs", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no pull request is open")
}

func TestSwapReplacesTheDiffWithTheWorkspace(t *testing.T) {
	s := newTestServer(t)
	before := s.space()

	// A pull request keeps its own store: its threads are anchored to its diff,
	// and mixing them into the branch's would orphan both on the way back.
	store, err := comments.Open(before.gitDir, "pr-7")
	require.NoError(t, err)
	fixed := gitdiff.FromText("base", "theirs", "")

	next := &workspace{
		homeRoot: before.homeRoot, branch: before.branch, gitDir: before.gitDir,
		opts: before.opts, store: store, snapshot: true,
		pr:     &PRInfo{Number: 7, ResolveBlocked: "not checked out"},
		source: func(context.Context) (*gitdiff.Diff, error) { return fixed, nil },
	}
	require.NoError(t, s.swap(context.Background(), next))

	after := s.space()
	assert.True(t, after.snapshot)
	assert.Equal(t, 7, after.pr.Number)
	assert.NotSame(t, before.store, after.store, "the pull request does not share the branch's threads")
	assert.Contains(t, after.store.Path(), "pr-7")

	// Anything describing the old view has to go with it.
	assert.Nil(t, s.currentDraft())
	assert.Nil(t, s.currentCreatedPR())
}

func TestResolveIsRefusedOnCodeThatIsNotCheckedOut(t *testing.T) {
	s := newTestServer(t)
	thread := createThread(t, s, `{"scope":"repo","body":"fix this"}`)
	require.NotEmpty(t, thread.ID)

	s.mu.Lock()
	s.ws.pr = &PRInfo{Number: 968, ResolveBlocked: "the PR head is not checked out"}
	s.mu.Unlock()

	rec := do(t, s, "POST", "/api/resolve", `{}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "not checked out")
}

func TestOpenPRsReachTheBrowser(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "GET", "/api/state", "")
	require.Equal(t, http.StatusOK, rec.Code)

	// Absent rather than null: the client filters and counts it.
	assert.NotContains(t, rec.Body.String(), `"openPrs":null`)
}

func TestFixingChecksIsRefusedOnCodeThatIsNotCheckedOut(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 962, Failing: 1, Checks: []ghpr.Check{{Name: "test", Bucket: "fail"}}})

	s.mu.Lock()
	s.ws.pr = &PRInfo{Number: 962, ResolveBlocked: "the PR head is not checked out"}
	s.mu.Unlock()

	// It edits files, so it lands wherever you happen to be — the same reason
	// Resolve is refused.
	rec := do(t, s, "POST", "/api/checks/fix", "")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "not checked out")
}
