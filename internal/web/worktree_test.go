package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/session"
)

// gitIn runs git in a checkout the test set up.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

func TestSlug(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{name: "plain", file: "cart-redesign.md", want: "cart-redesign"},
		{name: "nested", file: "designs/cart.md", want: "cart"},
		{name: "spaces and punctuation", file: "Cart Redesign (v2).md", want: "Cart-Redesign-v2"},
		{name: "leading and trailing junk", file: "--cart--.md", want: "cart"},
		{name: "nothing usable", file: "---.md", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Slug(tt.file))
		})
	}
}

func TestProposedWorktree(t *testing.T) {
	s := newTestServer(t)
	assert.Nil(t, s.proposedWorktree(), "no document, nothing to name it after")

	writeDoc(t, s, "cart-redesign.md", "# Cart\n")
	s.setDoc("cart-redesign.md")

	proposed := s.proposedWorktree()
	require.NotNil(t, proposed)
	assert.Equal(t, "cart-redesign", proposed.Branch)
	assert.Equal(t, gitdiff.WorktreePath(s.ws.homeRoot, "cart-redesign"), proposed.Path)

	s.ws.snapshot = true
	assert.Nil(t, s.proposedWorktree(), "a snapshot has nothing to check out")
}

func TestRelocateSwapsTheWholeCheckout(t *testing.T) {
	s := newTestServer(t)
	before := s.space()

	// A thread anchored to the old checkout, to prove the store does not travel.
	_, err := before.store.Add(comments.Thread{Scope: comments.ScopeRepo,
		Comments: []comments.Comment{{Author: comments.AuthorYou, Body: "old"}}})
	require.NoError(t, err)
	require.Len(t, before.store.List(), 1)

	path := filepath.Join(t.TempDir(), "ade-cart")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), before.homeRoot, path, "cart"))
	require.NoError(t, s.relocate(context.Background(), path))

	after := s.space()
	assert.NotSame(t, before, after, "the struct is replaced, not mutated")
	assert.Equal(t, path, after.homeRoot)
	assert.Equal(t, "cart", after.branch)
	assert.Equal(t, path, after.opts.Repo, "the diff must come from the new checkout")
	assert.NotEqual(t, before.gitDir, after.gitDir)
	assert.Empty(t, after.store.List(), "threads were anchored to a diff that no longer applies")
	assert.Len(t, before.store.List(), 1, "the old store is left alone, not emptied")
}

func TestTheRecordFollowsTheServerIntoAWorktree(t *testing.T) {
	s := newTestServer(t)
	s.direct, s.startedAt = "http://127.0.0.1:1/?t=x", time.Now()
	s.record("http://127.0.0.1:2/s/x/?t=y")
	start := session.Path(s.startGitDir)
	ctx := context.Background()

	first := filepath.Join(t.TempDir(), "ade-cart")
	require.NoError(t, gitdiff.AddWorktree(ctx, s.space().homeRoot, first, "cart"))
	require.NoError(t, s.relocate(ctx, first))
	firstRecord := session.Path(s.space().gitDir)
	rec, ok := session.Read(firstRecord)
	require.True(t, ok, "the folder an editor opens must name the server serving it")
	assert.Equal(t, os.Getpid(), rec.PID)
	assert.Equal(t, s.startRoot, rec.Root, "the root must match what /healthz says")
	assert.Equal(t, "http://127.0.0.1:2/s/x/?t=y", rec.Proxy)

	second := filepath.Join(t.TempDir(), "ade-till")
	require.NoError(t, gitdiff.AddWorktree(ctx, s.space().homeRoot, second, "till"))
	require.NoError(t, s.relocate(ctx, second))
	_, ok = session.Read(firstRecord)
	assert.False(t, ok, "a worktree left behind no longer names this server")
	_, ok = session.Read(session.Path(s.space().gitDir))
	assert.True(t, ok)
	_, ok = session.Read(start)
	assert.True(t, ok, "the record where it started stays")
}

func TestUnrecordLeavesAnotherServersRecord(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, session.Write(session.Path(dir), session.Record{PID: os.Getpid() + 1, URL: "http://x"}))
	unrecord(dir)
	_, ok := session.Read(session.Path(dir))
	assert.True(t, ok)
}

func TestRelocateClearsWhatDescribedTheOldCheckout(t *testing.T) {
	s := newTestServer(t)
	s.draft = &CommitDraft{Title: "old"}
	s.created = &CreatedPR{URL: "https://github.com/o/r/pull/1"}
	require.NotEmpty(t, s.diffHash)

	path := filepath.Join(t.TempDir(), "ade-cart")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), s.ws.homeRoot, path, "cart"))
	require.NoError(t, s.relocate(context.Background(), path))

	assert.Nil(t, s.currentDraft(), "the commit draft described the old index")
	assert.Nil(t, s.currentCreatedPR(), "that PR was for the old branch")
	assert.NotNil(t, s.space().store)
}

func TestRelocateMovesTheAgentAndDropsItsSession(t *testing.T) {
	s := newTestServer(t)
	s.agent.session = "abc123"
	s.agent.sessionBytes = 4096

	path := filepath.Join(t.TempDir(), "ade-cart")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), s.ws.homeRoot, path, "cart"))
	require.NoError(t, s.relocate(context.Background(), path))

	// Claude Code keys transcripts by working directory, so carrying the id over
	// would resume nothing while looking like it had.
	state := s.agent.State()
	assert.Empty(t, state.Session)
	assert.Zero(t, state.SessionBytes)
	assert.Equal(t, path, s.agent.dir)
}

func TestRelocateIsSafeAgainstConcurrentReaders(t *testing.T) {
	s := newTestServer(t)
	path := filepath.Join(t.TempDir(), "ade-cart")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), s.ws.homeRoot, path, "cart"))

	// This is the test the workspace refactor exists for: under -race, any field
	// still read directly off Server would be caught here.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				req := httptest.NewRequest("GET", "/api/state", nil)
				req.Header.Set("X-Ade-Token", s.token)
				rec := httptest.NewRecorder()
				s.routes().ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					panic("state read failed during relocation")
				}
			}
		}()
	}

	require.NoError(t, s.relocate(context.Background(), path))
	close(stop)
	wg.Wait()

	assert.Equal(t, path, s.space().homeRoot)
}

func TestGoIntoAWorktree(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "cart.md", "# Cart\n\nDebounce the totals.\n")
	s.setDoc("cart.md")

	origin := s.space().homeRoot
	proposed := s.proposedWorktree()
	require.NotNil(t, proposed)
	t.Cleanup(func() { _ = os.RemoveAll(proposed.Path) })

	tree, err := s.intoWorktree(context.Background())
	require.NoError(t, err)
	assert.Equal(t, proposed.Path, tree.Path)
	assert.DirExists(t, tree.Path)

	assert.Equal(t, tree.Path, s.space().homeRoot, "ade follows the build")
	assert.Equal(t, "cart", s.space().branch)
	assert.NotEqual(t, origin, s.space().homeRoot)

	// The document stays where it was, so there is one copy of the design and
	// the build prompt still resolves it.
	assert.Contains(t, s.docsDir, origin)
	assert.True(t, filepath.IsAbs(s.docPath("cart.md")), "from the worktree the doc is outside the root")
}

func TestIntoWorktreeNeedsADocument(t *testing.T) {
	s := newTestServer(t)
	_, err := s.intoWorktree(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no document")
}

func TestSetScope(t *testing.T) {
	s := newTestServer(t)
	require.Equal(t, gitdiff.ScopeBranch, s.space().opts.Scope)

	// A thread on the branch view, to prove the store survives a scope change
	// where a relocation would replace it.
	before := s.space().store
	_, err := before.Add(comments.Thread{Scope: comments.ScopeRepo,
		Comments: []comments.Comment{{Author: comments.AuthorYou, Body: "keep me"}}})
	require.NoError(t, err)

	require.NoError(t, s.setScope(context.Background(), gitdiff.ScopeUncommitted))

	after := s.space()
	assert.Equal(t, gitdiff.ScopeUncommitted, after.opts.Scope)
	assert.Same(t, before, after.store, "the same checkout keeps its threads")
	assert.Len(t, after.store.List(), 1)
	assert.Equal(t, s.ws.homeRoot, after.homeRoot, "nothing else about the checkout moved")
}

func TestSetScopeRejectsNonsense(t *testing.T) {
	s := newTestServer(t)
	require.Error(t, s.setScope(context.Background(), "sideways"))
	assert.Equal(t, gitdiff.ScopeBranch, s.space().opts.Scope, "a bad scope changes nothing")

	rec := do(t, s, "POST", "/api/scope", `{"scope":"sideways"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSetScopeIsANoOpWhenAlreadyThere(t *testing.T) {
	s := newTestServer(t)
	before := s.space()
	require.NoError(t, s.setScope(context.Background(), gitdiff.ScopeBranch))
	assert.Same(t, before, s.space(), "nothing is rebuilt for a scope it is already on")
}

func TestScopeEndpointSwitchesTheView(t *testing.T) {
	s := newTestServer(t)

	// The test repo has one commit on feature and a clean tree, so the
	// uncommitted view is empty where the branch view is not.
	require.NotEmpty(t, s.diff.Files)

	rec := do(t, s, "POST", "/api/scope", `{"scope":"uncommitted"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body stateResponse
	rec = do(t, s, "GET", "/api/state", "")
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "uncommitted", body.Meta.Scope)

	diff := getDiff(t, s)
	assert.Empty(t, diff.Diff.Files, "nothing is pending commit in the test repo")
	assert.Equal(t, 1, diff.Diff.Commits, "the branch is still a commit ahead")
	rec = do(t, s, "GET", "/api/diff", "")

	// Empty is not the same as absent: a nil slice marshals as null, which the
	// browser cannot iterate. assert.Empty passes either way, so check the wire.
	assert.Contains(t, rec.Body.String(), `"files":[]`)
	assert.NotContains(t, rec.Body.String(), `"files":null`)
}

func TestHealthzNeedsNoToken(t *testing.T) {
	s := newTestServer(t)

	// The reuse check runs before the caller has a token, so this one endpoint
	// has to answer without it.
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var health session.Health
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &health))
	assert.True(t, health.Ade)
	assert.Equal(t, s.ws.homeRoot, health.Root)
	assert.Equal(t, os.Getpid(), health.PID)
	// The port is predictable, so what the agent is doing is not for anyone
	// who can reach loopback.
	assert.Empty(t, health.Branch)
	assert.Nil(t, health.Agent)
	assert.NotContains(t, rec.Body.String(), "feature")
}

func TestHealthzSaysWhatTheAgentIsDoingToTheTokenHolder(t *testing.T) {
	s := newTestServer(t)
	s.stack(queued{kind: kindChat, message: "later"})
	s.agent.setSandbox(true)
	spoke := time.Now().Add(-11 * time.Minute).Truncate(time.Millisecond)
	s.agent.lastEvent = spoke

	for _, path := range []string{"/healthz", "/healthz?t=" + s.token} {
		req := httptest.NewRequest("GET", path, nil)
		if path == "/healthz" {
			req.Header.Set("X-Ade-Token", s.token)
		}
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var health session.Health
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &health))
		assert.Equal(t, "feature", health.Branch)
		require.NotNil(t, health.Agent)
		assert.Equal(t, 1, health.Agent.Queued)
		assert.True(t, health.Agent.Sandbox, "the graph says which agents are in a container")
		assert.True(t, spoke.Equal(health.Agent.LastEventAt), "how long it has been quiet is not knowable from outside")
	}

	// The wrong token is no token.
	req := httptest.NewRequest("GET", "/healthz?t=nope", nil)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var health session.Health
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &health))
	assert.True(t, health.Ade)
	assert.Nil(t, health.Agent)
}

func TestHealthzReportsWhereItStartedNotWhereItMoved(t *testing.T) {
	s := newTestServer(t)
	origin := s.startRoot

	path := filepath.Join(t.TempDir(), "ade-cart")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), s.ws.homeRoot, path, "cart"))
	require.NoError(t, s.relocate(context.Background(), path))

	// The record identifies the process, so health has to keep answering for
	// where it began or the record under that git dir would be orphaned.
	assert.Equal(t, path, s.space().homeRoot)
	assert.Equal(t, origin, s.startRoot)
}

func TestRebindKeepsTheAgentWhereRelocateMovesIt(t *testing.T) {
	t.Run("rebind keeps the session", func(t *testing.T) {
		s := newTestServer(t)
		s.agent.session = "abc123"

		require.NoError(t, s.rebind(context.Background(), s.ws.homeRoot))

		// Same directory, so the transcript is still findable and the design
		// conversation survives a branch switch.
		assert.Equal(t, "abc123", s.agent.State().Session)
	})

	t.Run("relocate drops it", func(t *testing.T) {
		s := newTestServer(t)
		s.agent.session = "abc123"

		path := filepath.Join(t.TempDir(), "ade-cart")
		require.NoError(t, gitdiff.AddWorktree(context.Background(), s.ws.homeRoot, path, "cart"))
		require.NoError(t, s.relocate(context.Background(), path))

		assert.Empty(t, s.agent.State().Session)
	})
}

func TestFollowBranch(t *testing.T) {
	s := newTestServer(t)
	require.Equal(t, "feature", s.space().branch)

	gitIn(t, s.ws.homeRoot, "checkout", "-qb", "other")
	s.followBranch(context.Background())

	assert.Equal(t, "other", s.space().branch)
	assert.Contains(t, s.space().store.Path(), "other", "a branch gets its own comment store")
}

func TestFollowBranchStaysPutDuringARun(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "build")
	require.True(t, ok)
	defer s.agent.finish(nil)

	// A build can create a branch; rebinding under it would swap the store the
	// work is about to be reviewed in.
	gitIn(t, s.ws.homeRoot, "checkout", "-qb", "other")
	s.followBranch(context.Background())

	assert.Equal(t, "feature", s.space().branch)
}

func TestRematchDesignFollowsTheBranch(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "DEV-82.md", "# Eighty two\n")
	s.setDoc("DEV-82.md")

	gitIn(t, s.ws.homeRoot, "checkout", "-qb", "feat/dev-82-thing")
	s.followBranch(context.Background())
	assert.Equal(t, "DEV-82.md", s.currentDoc(), "the ticket still matches")

	gitIn(t, s.ws.homeRoot, "checkout", "-qb", "feat/unrelated")
	s.followBranch(context.Background())
	assert.Empty(t, s.currentDoc(), "no document for this branch, so no Design tab")
}

func TestProposedWorktreeDeclinesWhenAlreadyInIt(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "FIX-999.md", "# Fix\n")
	s.setDoc("FIX-999.md")

	require.NotNil(t, s.proposedWorktree(), "on feature, a worktree is worth offering")

	// ade create --linear lands you on exactly this branch, and offering
	// ade-fix-999-FIX-999 off it is nonsense.
	gitIn(t, s.ws.homeRoot, "checkout", "-qb", "fix-999")
	s.followBranch(context.Background())
	assert.Nil(t, s.proposedWorktree())

	// The refusal said "there is no document" for a year, with one open.
	_, err := s.intoWorktree(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already fix-999")
}
