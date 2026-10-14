package web

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// bare gives the repository a remote to push to, since a push with none is
// not a push refused — it is a push that cannot be tried.
func bare(t *testing.T, root string) {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "origin.git")
	cmd := exec.Command("git", "init", "--bare", "-q", remote)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git init --bare: %s", out)
	gitIn(t, root, "remote", "add", "origin", remote)
}

func TestPushWorktreeRefusesAPathOutsideTheRepository(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/worktrees/push", `{"path":"/etc"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "not one of this repository's worktrees")
}

func TestPushWorktreeSendsTheBranchUpstream(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	bare(t, root)

	one := gitdiff.WorktreePath(root, "one")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), root, one, "one"))
	require.NoError(t, os.WriteFile(filepath.Join(one, "new.go"), []byte("package main\n"), 0o600))
	gitIn(t, one, "add", ".")
	gitIn(t, one, "commit", "-qm", "work")

	rec := do(t, s, "POST", "/api/worktrees/push", `{"path":"`+one+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"pushed":"one"`)

	// The point of the verb: unpushed was what kept the worktree, and the
	// scan has to agree it no longer does.
	after := do(t, s, "GET", "/api/worktrees", "")
	require.Equal(t, http.StatusOK, after.Code)
	assert.NotContains(t, after.Body.String(), `"keep":"unpushed"`)
}

func TestPushWorktreeReportsGitsOwnRefusal(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	one := gitdiff.WorktreePath(root, "one")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), root, one, "one"))

	// No remote, so there is nowhere to push.
	rec := do(t, s, "POST", "/api/worktrees/push", `{"path":"`+one+`"}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestCloseWorktreeRefusesAPathOutsideTheRepository(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/worktrees/close", `{"path":"/etc"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "not one of this repository's worktrees")
}

// Stopping the session that is answering would lose the answer with it.
func TestCloseWorktreeRefusesTheSessionAnsweringTheRequest(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/worktrees/close", `{"path":"`+s.space().homeRoot+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "this session")
}

func TestCloseWorktreeSaysSoWhenNothingIsServingIt(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	one := gitdiff.WorktreePath(root, "one")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), root, one, "one"))

	rec := do(t, s, "POST", "/api/worktrees/close", `{"path":"`+one+`"}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "no ade is running")
}

// stacked makes a parent worktree with a commit and a child branched off it
// with one of its own, which is the shape merge-into-the-rail is for.
func stacked(t *testing.T, s *Server) (parent, child string) {
	t.Helper()
	root := s.space().homeRoot
	ctx := context.Background()

	parent = gitdiff.WorktreePath(root, "parent")
	require.NoError(t, gitdiff.AddWorktree(ctx, root, parent, "parent"))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "p.go"), []byte("package main\n"), 0o600))
	gitIn(t, parent, "add", ".")
	gitIn(t, parent, "commit", "-qm", "parent work")

	child = gitdiff.WorktreePath(root, "child")
	require.NoError(t, gitdiff.AddWorktreeFrom(ctx, root, child, "child", "parent"))
	require.NoError(t, os.WriteFile(filepath.Join(child, "c.go"), []byte("package main\n"), 0o600))
	gitIn(t, child, "add", ".")
	gitIn(t, child, "commit", "-qm", "child work")
	return parent, child
}

func TestMergeWorktreeLandsTheChildOnTheRailItForkedFrom(t *testing.T) {
	s := newTestServer(t)
	parent, child := stacked(t, s)

	rec := do(t, s, "POST", "/api/worktrees/merge",
		`{"path":"`+child+`","into":"`+parent+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"merged":"child"`)

	// The child now holds nothing the parent does not, which is what makes
	// the checkout droppable.
	left, err := gitdiff.Commits(context.Background(), parent, "child", "parent")
	require.NoError(t, err)
	assert.NotEmpty(t, left, "the merge commit is on the parent")
	none, err := gitdiff.Commits(context.Background(), child, "parent", "child")
	require.NoError(t, err)
	assert.Empty(t, none, "the child has nothing of its own left")
}

// A conflict the resolver cannot settle must leave nothing behind: a
// half-merged checkout is worse than a refusal. The test suite runs no real
// agent, so this is also the path where the model itself is unavailable.
func TestMergeWorktreePutsTheTreeBackWhenTheResolveFails(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	ctx := context.Background()

	parent := gitdiff.WorktreePath(root, "parent")
	require.NoError(t, gitdiff.AddWorktree(ctx, root, parent, "parent"))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "x.go"), []byte("package main\n\nvar x = 1\n"), 0o600))
	gitIn(t, parent, "add", ".")
	gitIn(t, parent, "commit", "-qm", "parent x")

	child := gitdiff.WorktreePath(root, "child")
	require.NoError(t, gitdiff.AddWorktreeFrom(ctx, root, child, "child", "parent~1"))
	require.NoError(t, os.WriteFile(filepath.Join(child, "x.go"), []byte("package main\n\nvar x = 2\n"), 0o600))
	gitIn(t, child, "add", ".")
	gitIn(t, child, "commit", "-qm", "child x")

	before, err := gitdiff.HeadSHA(ctx, parent)
	require.NoError(t, err)

	rec := do(t, s, "POST", "/api/worktrees/merge",
		`{"path":"`+child+`","into":"`+parent+`"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "nothing was merged")

	after, err := gitdiff.HeadSHA(ctx, parent)
	require.NoError(t, err)
	assert.Equal(t, before, after, "the parent is where it was")
	status, err := gitdiff.WorktreeStatus(ctx, parent)
	require.NoError(t, err)
	assert.Empty(t, status.Unstaged, "no conflict markers are left in the tree")
	assert.Empty(t, status.Staged)
}

func TestMergeWorktreeRefusesADirtyTarget(t *testing.T) {
	s := newTestServer(t)
	parent, child := stacked(t, s)
	require.NoError(t, os.WriteFile(filepath.Join(parent, "wip.go"), []byte("package main\n"), 0o600))
	gitIn(t, parent, "add", ".")

	rec := do(t, s, "POST", "/api/worktrees/merge",
		`{"path":"`+child+`","into":"`+parent+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "uncommitted")
}

func TestMergeWorktreeRefusesWorkTheMergeWouldNotCarry(t *testing.T) {
	s := newTestServer(t)
	parent, child := stacked(t, s)
	require.NoError(t, os.WriteFile(filepath.Join(child, "wip.go"), []byte("package main\n"), 0o600))
	gitIn(t, child, "add", ".")

	rec := do(t, s, "POST", "/api/worktrees/merge",
		`{"path":"`+child+`","into":"`+parent+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "would not carry")
}

func TestMergeWorktreeRefusesAPathOutsideTheRepository(t *testing.T) {
	s := newTestServer(t)
	_, child := stacked(t, s)

	rec := do(t, s, "POST", "/api/worktrees/merge", `{"path":"`+child+`","into":"/etc"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = do(t, s, "POST", "/api/worktrees/merge", `{"path":"/etc","into":"`+child+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMergeWorktreeRefusesABranchIntoItself(t *testing.T) {
	s := newTestServer(t)
	_, child := stacked(t, s)

	rec := do(t, s, "POST", "/api/worktrees/merge",
		`{"path":"`+child+`","into":"`+child+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "into itself")
}

// The common case, and the one the stacked test does not cover: a branch off
// the base merges into the repository's own checkout.
func TestMergeWorktreeLandsABranchOnTheRepositorysOwnCheckout(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	ctx := context.Background()

	one := gitdiff.WorktreePath(root, "one")
	require.NoError(t, gitdiff.AddWorktree(ctx, root, one, "one"))
	require.NoError(t, os.WriteFile(filepath.Join(one, "one.go"), []byte("package main\n"), 0o600))
	gitIn(t, one, "add", ".")
	gitIn(t, one, "commit", "-qm", "work on one")

	rec := do(t, s, "POST", "/api/worktrees/merge", `{"path":"`+one+`","into":"`+root+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// It landed on whatever the checkout has out, and the branch now holds
	// nothing that checkout does not.
	left, err := gitdiff.Commits(ctx, root, "one", gitdiff.Branch(ctx, root))
	require.NoError(t, err)
	assert.NotEmpty(t, left)
	none, err := gitdiff.Commits(ctx, one, gitdiff.Branch(ctx, root), "one")
	require.NoError(t, err)
	assert.Empty(t, none, "one has nothing of its own left")
}

func TestMergeWorktreeRefusesADirtyRepositoryCheckout(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	ctx := context.Background()

	one := gitdiff.WorktreePath(root, "one")
	require.NoError(t, gitdiff.AddWorktree(ctx, root, one, "one"))
	require.NoError(t, os.WriteFile(filepath.Join(one, "one.go"), []byte("package main\n"), 0o600))
	gitIn(t, one, "add", ".")
	gitIn(t, one, "commit", "-qm", "work on one")
	require.NoError(t, os.WriteFile(filepath.Join(root, "wip.go"), []byte("package main\n"), 0o600))
	gitIn(t, root, "add", ".")

	rec := do(t, s, "POST", "/api/worktrees/merge", `{"path":"`+one+`","into":"`+root+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "uncommitted")
}
