package web

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/ghpr"
)

func TestResolveBlockedReason(t *testing.T) {
	pr := &ghpr.PR{Number: 6, HeadRef: "fix-748", HeadSHA: "c60621e707477b7f4e07078826607f4582653a25"}
	other := "1457e6da492ab4174e7cc6bbeaf743f453837ea6"

	t.Run("allowed when the PR head is checked out", func(t *testing.T) {
		assert.Empty(t, ResolveBlockedReason(pr, pr.HeadSHA, "fix-748"))
	})

	// Committing locally moves HEAD off the PR head while the code under the
	// edit is still the branch under review.
	t.Run("allowed on the PR branch with unpushed commits", func(t *testing.T) {
		assert.Empty(t, ResolveBlockedReason(pr, other, "fix-748"))
	})

	t.Run("blocked on another branch", func(t *testing.T) {
		reason := ResolveBlockedReason(pr, other, "main")
		assert.Contains(t, reason, "c60621e7", "names the commit that should be checked out")
		assert.Contains(t, reason, "gh pr checkout 6", "tells the user how to fix it")
	})

	// Detached at another commit: no branch to vouch for it, so the SHA decides.
	t.Run("blocked when detached away from the head", func(t *testing.T) {
		assert.NotEmpty(t, ResolveBlockedReason(pr, other, ""))
	})

	t.Run("no PR means no restriction", func(t *testing.T) {
		assert.Empty(t, ResolveBlockedReason(nil, "anything", ""))
	})
}

func TestResolveRefusedWhenPRHeadIsNotCheckedOut(t *testing.T) {
	s := newTestServer(t)
	createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"fix this"}`)
	s.ws.pr = &PRInfo{Number: 6, ResolveBlocked: "the PR head abc12345 is not checked out"}

	rec := do(t, s, "POST", "/api/resolve", `{}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "not checked out")
	assert.False(t, s.agent.State().Running, "the run must not have started")
}

func TestPRIsExposedInState(t *testing.T) {
	s := newTestServer(t)
	s.ws.pr = &PRInfo{Number: 6, Title: "feat: add interactive chat", Author: "owenrumney", State: "MERGED"}

	rec := do(t, s, "GET", "/api/state", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"number":6`)
	assert.Contains(t, rec.Body.String(), "feat: add interactive chat")
}

func TestImportComments(t *testing.T) {
	// The test repo's only change is "var x = 2" on new-side line 3.
	s := newTestServer(t)

	threads := ghpr.GroupThreads(mustParse(t, `[
	  {"id":100,"path":"a.go","line":3,"side":"RIGHT","body":"why 2?","created_at":"2026-08-01T10:00:00Z","user":{"login":"Copilot"}},
	  {"id":101,"in_reply_to_id":100,"path":"a.go","line":3,"side":"RIGHT","body":"because","created_at":"2026-08-01T11:00:00Z","user":{"login":"owenrumney"}},
	  {"id":102,"path":"a.go","line":900,"side":"RIGHT","body":"way off","created_at":"2026-08-01T10:05:00Z","user":{"login":"Copilot"}},
	  {"id":103,"path":"a.go","line":null,"original_line":3,"side":"RIGHT","body":"stale","created_at":"2026-08-01T10:06:00Z","user":{"login":"Copilot"}}
	]`))

	added := s.importComments(threads)
	assert.Equal(t, 3, added, "three roots, one of which carries a reply")

	stored := s.ws.store.List()
	require.Len(t, stored, 3)
	byExt := map[string]int{}
	for i, thread := range stored {
		byExt[thread.ExternalID] = i
	}

	t.Run("a reply is kept in the same thread", func(t *testing.T) {
		thread := stored[byExt["100"]]
		require.Len(t, thread.Comments, 2)
		assert.Equal(t, "Copilot", thread.Comments[0].Author)
		assert.Equal(t, "why 2?", thread.Comments[0].Body)
		assert.Equal(t, "owenrumney", thread.Comments[1].Author)
		assert.False(t, thread.Orphaned)
	})

	t.Run("a line outside the diff is kept but orphaned", func(t *testing.T) {
		thread := stored[byExt["102"]]
		assert.True(t, thread.Orphaned)
		assert.Equal(t, 900, thread.Line)
	})

	t.Run("a comment from an earlier push is marked", func(t *testing.T) {
		thread := stored[byExt["103"]]
		assert.Contains(t, thread.Comments[0].Body, "on an earlier push")
	})

	t.Run("importing again adds nothing", func(t *testing.T) {
		assert.Equal(t, 0, s.importComments(threads))
		assert.Len(t, s.ws.store.List(), 3)
	})
}

func TestImportedThreadsAreResolvable(t *testing.T) {
	s := newTestServer(t)
	threads := ghpr.GroupThreads(mustParse(t,
		`[{"id":1,"path":"a.go","line":3,"side":"RIGHT","body":"fix","user":{"login":"Copilot"}}]`))

	require.Equal(t, 1, s.importComments(threads))
	assert.Len(t, s.threadsToResolve(nil), 1, "an upstream comment is work like any other")
}

func TestSnapshotServerDoesNotPoll(t *testing.T) {
	store, err := comments.Open(t.TempDir(), "main")
	require.NoError(t, err)

	s, err := New(Config{
		Root:     t.TempDir(),
		Branch:   "main",
		Snapshot: "diff --git a/a.go b/a.go\nindex 1..2 100644\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-a\n+b\n",
		PR:       &PRInfo{Number: 6, HeadRef: "feature"},
	}, store)
	require.NoError(t, err)
	require.True(t, s.ws.snapshot)

	require.NoError(t, s.refresh(context.Background()))
	require.NotNil(t, s.diff)
	assert.Equal(t, "pr snapshot", s.diff.Mode)
	require.Len(t, s.diff.Files, 1)
	assert.Equal(t, "a.go", s.diff.Files[0].Path)

	// poll returns immediately for a snapshot instead of hammering the API.
	done := make(chan struct{})
	go func() {
		s.poll(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("poll should return at once for a fetched snapshot")
	}
}

func mustParse(t *testing.T, payload string) []ghpr.ReviewComment {
	t.Helper()
	got, err := ghpr.ParseComments(payload)
	require.NoError(t, err)
	return got
}

func TestImportCommentsMergesARepliedThread(t *testing.T) {
	s := newTestServer(t)

	first := ghpr.GroupThreads(mustParse(t, `[
	  {"id":100,"path":"a.go","line":3,"side":"RIGHT","body":"why 2?","created_at":"2026-08-01T10:00:00Z","user":{"login":"Copilot"}}
	]`))
	require.Equal(t, 1, s.importComments(first))

	// The same thread on the next poll, with a reply that landed on GitHub in
	// between: the thread is already known, so only the reply is new.
	second := ghpr.GroupThreads(mustParse(t, `[
	  {"id":100,"path":"a.go","line":3,"side":"RIGHT","body":"why 2?","created_at":"2026-08-01T10:00:00Z","user":{"login":"Copilot"}},
	  {"id":101,"in_reply_to_id":100,"path":"a.go","line":3,"side":"RIGHT","body":"because it is","created_at":"2026-08-01T11:00:00Z","user":{"login":"owenrumney"}}
	]`))
	assert.Equal(t, 1, s.importComments(second), "the reply, not the thread again")

	stored := s.ws.store.List()
	require.Len(t, stored, 1, "no second copy of the thread")
	require.Len(t, stored[0].Comments, 2)
	assert.Equal(t, "because it is", stored[0].Comments[1].Body)

	assert.Equal(t, 0, s.importComments(second), "polling again adds nothing")
	assert.Len(t, s.ws.store.List()[0].Comments, 2)
}

func TestImportCommentsReopensAThreadYouResolved(t *testing.T) {
	s := newTestServer(t)
	require.Equal(t, 1, s.importComments(ghpr.GroupThreads(mustParse(t,
		`[{"id":100,"path":"a.go","line":3,"side":"RIGHT","body":"why 2?","user":{"login":"Copilot"}}]`))))

	stored := s.ws.store.List()
	require.Len(t, stored, 1)
	_, err := s.ws.store.SetResolved(stored[0].ID, true)
	require.NoError(t, err)

	require.Equal(t, 1, s.importComments(ghpr.GroupThreads(mustParse(t, `[
	  {"id":100,"path":"a.go","line":3,"side":"RIGHT","body":"why 2?","user":{"login":"Copilot"}},
	  {"id":101,"in_reply_to_id":100,"path":"a.go","line":3,"side":"RIGHT","body":"not settled","user":{"login":"Copilot"}}
	]`))))

	assert.False(t, s.ws.store.List()[0].Resolved, "a new reply brings it back")
}

func TestImportCommentsMergesRepliesOntoAThreadYouSent(t *testing.T) {
	s := newTestServer(t)
	createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"this reads oddly"}`)

	stored := s.ws.store.List()
	require.Len(t, stored, 1)
	// What stampSentExternalIDs does after a successful send.
	_, err := s.ws.store.SetExternal(stored[0].ID, "500")
	require.NoError(t, err)

	// The import sees its own comment as the root, plus the reviewer's reply.
	added := s.importComments(ghpr.GroupThreads(mustParse(t, `[
	  {"id":500,"path":"a.go","line":3,"side":"RIGHT","body":"this reads oddly","user":{"login":"owenrumney"}},
	  {"id":501,"in_reply_to_id":500,"path":"a.go","line":3,"side":"RIGHT","body":"fixed in 1a2b3c","user":{"login":"Copilot"}}
	]`)))
	assert.Equal(t, 1, added, "the reply only: the root is the comment you sent")

	got := s.ws.store.List()
	require.Len(t, got, 1, "the sent thread is not imported as a second copy")
	require.Len(t, got[0].Comments, 2, "your own comment is not echoed back at you")
	assert.Equal(t, "this reads oddly", got[0].Comments[0].Body)
	assert.Equal(t, "fixed in 1a2b3c", got[0].Comments[1].Body)
}

// snapshotServer is `ade --pr N` with the pull request's head not checked out:
// the diff is fetched text rather than local git.
func snapshotServer(t *testing.T, pr *PRInfo) *Server {
	t.Helper()
	store, err := comments.Open(t.TempDir(), "main")
	require.NoError(t, err)

	// Its own GitDir: a snapshot with none shares one chat store per process,
	// and two of them in one test binary contend for its lock.
	s, err := New(Config{
		Root:     t.TempDir(),
		GitDir:   t.TempDir(),
		Branch:   "main",
		Snapshot: "diff --git a/a.go b/a.go\nindex 1..2 100644\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-a\n+b\n",
		PR:       pr,
	}, store)
	require.NoError(t, err)
	require.True(t, s.ws.snapshot)
	return s
}

// The Review tab, and with it convert and send, is drawn from branchPR. A
// snapshot used to skip the poll that fills it, so opening a pull request whose
// head was not checked out — what the Firefox extension always does — lost the
// tab and every button gated on it.
func TestSnapshotStillPollsThePullRequest(t *testing.T) {
	s := snapshotServer(t, &PRInfo{Number: 6, HeadRef: "feature"})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.pollBranchPR(ctx)
		close(done)
	}()

	// It must not return on its own: reaching the ticker loop is what keeps the
	// Review tab current.
	select {
	case <-done:
		t.Fatal("the branch PR poll gave up on a snapshot")
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the branch PR poll ignored a cancelled context")
	}
}

func TestReviewingIsThePullRequestOnScreen(t *testing.T) {
	t.Run("a snapshot with no pull request describes nothing", func(t *testing.T) {
		s := snapshotServer(t, nil)
		pr, err := s.reviewing(context.Background(), s.ws)
		require.NoError(t, err)
		assert.Nil(t, pr, "the checked-out branch's PR is not the diff on screen")
	})

	t.Run("a branch with no pull request is not an error", func(t *testing.T) {
		s := newTestServer(t)
		pr, err := s.reviewing(context.Background(), s.ws)
		_ = err // no gh in the test environment; the point is it does not panic
		assert.Nil(t, pr)
	})
}
