package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/sandbox"
)

// repoAt is a checkout with one commit on main, which is all a worktree needs.
func repoAt(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.MkdirAll(root, 0o750))
	gitAt(t, root, "init", "-b", "main", "-q")
	commitAt(t, root, "a.go")
	return root
}

func TestEveryOtherRepositoryGetsAWorktreeOnTheSameBranch(t *testing.T) {
	ctx := context.Background()
	home := repoAt(t, "home")
	one := repoAt(t, "one")
	two := repoAt(t, "two")

	homeTree := gitdiff.WorktreePath(home, "cancel-button")
	_, err := gitdiff.EnsureWorktree(ctx, home, homeTree, "cancel-button")
	require.NoError(t, err)
	gitDir, err := gitdiff.GitDir(ctx, homeTree)
	require.NoError(t, err)

	require.NoError(t, alongside(ctx, gitDir, "cancel-button", []string{one, two}))

	for _, repo := range []string{one, two} {
		path := gitdiff.WorktreePath(repo, "cancel-button")
		assert.DirExists(t, path)
		assert.Equal(t, "cancel-button", gitdiff.Branch(ctx, path))
	}

	ws, err := sandbox.LoadWorkspace(sandbox.SessionPath(gitDir))
	require.NoError(t, err)
	require.Len(t, ws.Paths, 2, "both are this session's own mounts")
	for _, path := range ws.Paths {
		assert.True(t, path.RW, "the agent edits them, so they are not read-only")
	}
}

func TestARepositoryThatAlreadyHasTheBranchChecksItOutRatherThanFailing(t *testing.T) {
	ctx := context.Background()
	home := repoAt(t, "home")
	other := repoAt(t, "other")
	// The branch is there with no worktree on it: a second session on the same
	// ticket, after the first one's checkout was removed.
	gitAt(t, other, "branch", "cancel-button")

	homeTree := gitdiff.WorktreePath(home, "cancel-button")
	_, err := gitdiff.EnsureWorktree(ctx, home, homeTree, "cancel-button")
	require.NoError(t, err)
	gitDir, err := gitdiff.GitDir(ctx, homeTree)
	require.NoError(t, err)

	require.NoError(t, alongside(ctx, gitDir, "cancel-button", []string{other}))

	path := gitdiff.WorktreePath(other, "cancel-button")
	assert.Equal(t, "cancel-button", gitdiff.Branch(ctx, path))
}

func TestABranchAlreadyCheckedOutElsewhereIsWorkedInWhereItIs(t *testing.T) {
	ctx := context.Background()
	home := repoAt(t, "home")
	other := repoAt(t, "other")
	// The main checkout is sitting on the branch, which git will not hand to a
	// second worktree.
	gitAt(t, other, "checkout", "-q", "-b", "cancel-button")

	homeTree := gitdiff.WorktreePath(home, "cancel-button")
	_, err := gitdiff.EnsureWorktree(ctx, home, homeTree, "cancel-button")
	require.NoError(t, err)
	gitDir, err := gitdiff.GitDir(ctx, homeTree)
	require.NoError(t, err)

	require.NoError(t, alongside(ctx, gitDir, "cancel-button", []string{other}))

	ws, err := sandbox.LoadWorkspace(sandbox.SessionPath(gitDir))
	require.NoError(t, err)
	require.Len(t, ws.Paths, 1)
	assert.Equal(t, "cancel-button", gitdiff.Branch(ctx, ws.Paths[0].Path))
	assert.NoDirExists(t, gitdiff.WorktreePath(other, "cancel-button"),
		"the checkout that already has the branch is the one it works in")
}

func TestAWorktreeMadeBeforeALaterRepositoryFailedIsRemoved(t *testing.T) {
	ctx := context.Background()
	home := repoAt(t, "home")
	one := repoAt(t, "one")
	two := repoAt(t, "two")
	// Something in the way at the path two's worktree would take.
	require.NoError(t, os.MkdirAll(gitdiff.WorktreePath(two, "cancel-button"), 0o750))

	homeTree := gitdiff.WorktreePath(home, "cancel-button")
	_, err := gitdiff.EnsureWorktree(ctx, home, homeTree, "cancel-button")
	require.NoError(t, err)
	gitDir, err := gitdiff.GitDir(ctx, homeTree)
	require.NoError(t, err)

	err = alongside(ctx, gitDir, "cancel-button", []string{one, two})

	require.Error(t, err)
	assert.NoDirExists(t, gitdiff.WorktreePath(one, "cancel-button"), "the one it did make is taken back")
	assert.NoFileExists(t, sandbox.SessionPath(gitDir))
}

func TestSomewhereThatIsNotACheckoutIsRefusedRatherThanDropped(t *testing.T) {
	ctx := context.Background()
	home := repoAt(t, "home")
	plain := t.TempDir()

	homeTree := gitdiff.WorktreePath(home, "cancel-button")
	_, err := gitdiff.EnsureWorktree(ctx, home, homeTree, "cancel-button")
	require.NoError(t, err)
	gitDir, err := gitdiff.GitDir(ctx, homeTree)
	require.NoError(t, err)

	err = alongside(ctx, gitDir, "cancel-button", []string{plain})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a checkout")
	assert.NoFileExists(t, sandbox.SessionPath(gitDir), "nothing is recorded when one of them cannot be made")
}
