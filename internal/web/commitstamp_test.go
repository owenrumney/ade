package web

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/lineage"
)

// recordTurn puts one range of a staged file into lineage, the way settleTurn
// would have when the agent wrote it.
func recordTurn(t *testing.T, s *Server, file string, start, lines int, summary string) {
	t.Helper()
	require.NoError(t, s.refresh(context.Background()))

	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	anchor, ok := comments.Locate(diff, file, comments.SideRight, start)
	require.True(t, ok, "line %d of %s should be anchorable", start, file)

	store := s.lineageStore()
	require.NotNil(t, store)
	require.NoError(t, store.Add(lineage.Entry{
		Session: "s1", Summary: summary,
		Ranges: []lineage.Range{{File: file, Start: start, Lines: lines, Anchor: anchor}},
	}))
}

func TestCommittingStampsTheLinesTheTurnWrote(t *testing.T) {
	s := newTestServer(t)
	stage(t, s.ws.homeRoot, "b.go", "package main\n\nvar y = 1\n")
	recordTurn(t, s, "b.go", 1, 3, "add a y")

	rec := do(t, s, "POST", "/api/commit", `{"title":"feat(b): add y"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	head, err := gitdiff.HeadSHA(context.Background(), s.ws.homeRoot)
	require.NoError(t, err)
	turns := s.lineageStore().ForCommit(head, "b.go")
	require.Len(t, turns, 1)
	assert.Equal(t, "add a y", turns[0].Summary)
}

func TestAmendingMovesTheStampOntoTheNewCommit(t *testing.T) {
	s := newTestServer(t)
	stage(t, s.ws.homeRoot, "b.go", "package main\n\nvar y = 1\n")
	recordTurn(t, s, "b.go", 1, 3, "add a y")

	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/commit", `{"title":"feat(b): add y"}`).Code)
	first, err := gitdiff.HeadSHA(context.Background(), s.ws.homeRoot)
	require.NoError(t, err)

	rec := do(t, s, "POST", "/api/commit", `{"title":"feat(b): add y properly","amend":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	second, err := gitdiff.HeadSHA(context.Background(), s.ws.homeRoot)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	assert.Empty(t, s.lineageStore().ForCommit(first, ""), "the rewritten commit is gone")
	assert.Len(t, s.lineageStore().ForCommit(second, "b.go"), 1)
}

func TestALineNoTurnWroteIsNotStamped(t *testing.T) {
	s := newTestServer(t)
	stage(t, s.ws.homeRoot, "b.go", "package main\n")

	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/commit", `{"title":"chore: by hand"}`).Code)

	head, err := gitdiff.HeadSHA(context.Background(), s.ws.homeRoot)
	require.NoError(t, err)
	assert.Empty(t, s.lineageStore().ForCommit(head, ""))
}

func TestDrainingTheQueueStampsATerminalCommit(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	require.NoError(t, lineage.InstallHook(ctx, s.ws.homeRoot))

	stage(t, s.ws.homeRoot, "b.go", "package main\n\nvar y = 1\n")
	recordTurn(t, s, "b.go", 1, 3, "add a y")
	gitOut(t, s.ws.homeRoot, "commit", "-qm", "feat(b): add y")

	s.drainCommits(ctx)

	head, err := gitdiff.HeadSHA(ctx, s.ws.homeRoot)
	require.NoError(t, err)
	turns := s.lineageStore().ForCommit(head, "b.go")
	require.Len(t, turns, 1)
	assert.Equal(t, "add a y", turns[0].Summary)
}
