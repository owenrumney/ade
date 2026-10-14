package web

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/ghpr"
)

func upstreamThread(t *testing.T, s *Server, id, body string) comments.Thread {
	t.Helper()
	thread, err := s.ws.store.Add(comments.Thread{
		File: "a.go", Side: comments.SideRight, Line: 3,
		ExternalID: id,
		Comments:   []comments.Comment{{Author: "reviewer", Body: body}},
	})
	require.NoError(t, err)
	return thread
}

func TestImportedSelectsOnlyUpstreamThreads(t *testing.T) {
	s := newTestServer(t)
	upstreamThread(t, s, "991", "why not a map?")

	// A comment you wrote yourself has nobody to reply to.
	_, err := s.ws.store.Add(comments.Thread{Scope: comments.ScopeRepo,
		Comments: []comments.Comment{{Author: comments.AuthorYou, Body: "note to self"}}})
	require.NoError(t, err)

	got := s.imported()
	require.Len(t, got, 1)
	assert.Equal(t, "991", got[0].ExternalID)
}

func TestImportedIgnoresResolved(t *testing.T) {
	s := newTestServer(t)
	thread := upstreamThread(t, s, "991", "why not a map?")
	_, err := s.ws.store.SetResolved(thread.ID, true)
	require.NoError(t, err)

	assert.Empty(t, s.imported())
}

func TestDraftRepliesNeedsSomethingToAnswer(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/draft-replies", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no open review comments")
}

func TestDraftReplyQueuesBehindTheAgentLock(t *testing.T) {
	s := newTestServer(t)
	thread := upstreamThread(t, s, "991", "why not a map?")

	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	// Answering every comment and answering one are different asks, so both
	// wait rather than the second being read as the first pressed twice.
	for _, path := range []string{"/api/draft-replies", "/api/threads/" + thread.ID + "/draft-reply"} {
		rec := do(t, s, "POST", path, "")
		assert.Equal(t, http.StatusAccepted, rec.Code, path)
	}
	assert.Len(t, s.waiting(), 2)
}

func TestDraftReplyNeedsAThreadThatExists(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/threads/nope/draft-reply", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestImportingUpstreamCommentsIsIdempotent(t *testing.T) {
	s := newTestServer(t)

	threads := []ghpr.Thread{{Root: ghpr.ReviewComment{
		ID: 991, Path: "a.go", Line: 3, Side: comments.SideRight,
		Author: "reviewer", Body: "why not a map?",
	}}}

	assert.Equal(t, 1, s.importComments(threads))
	// The poll runs every couple of minutes; a second pass must not duplicate.
	assert.Equal(t, 0, s.importComments(threads))
	assert.Len(t, s.ws.store.List(), 1)
}

func TestADraftedReplyIsMarkedAsSuch(t *testing.T) {
	s := newTestServer(t)
	thread := upstreamThread(t, s, "991", "why not a map?")

	// The author is what the UI keys off to say this was never sent anywhere.
	_, err := s.ws.store.Reply(thread.ID, comments.AuthorDraft, "because order matters")
	require.NoError(t, err)

	got, err := s.ws.store.Get(thread.ID)
	require.NoError(t, err)
	require.Len(t, got.Comments, 2)
	assert.Equal(t, comments.AuthorDraft, got.Comments[1].Author)
	assert.NotEqual(t, comments.AuthorAgent, got.Comments[1].Author)
}
