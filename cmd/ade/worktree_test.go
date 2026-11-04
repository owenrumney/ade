package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/docs"
	"github.com/owenrumney/ade/internal/gitdiff"
)

func gitAt(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// from is the start ref as EnsureWorktreeFrom wants it: resolved only when a
// worktree actually has to be made.
func from(root string) func() string {
	return func() string { return freshBase(context.Background(), root) }
}

func commitAt(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("package main\n"), 0o600))
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-qm", name)
}

// cloned is a checkout of a shared origin, so the base can move on the remote
// without the checkout hearing about it — a main not pulled for a week.
func cloned(t *testing.T) (checkout, elsewhere string) {
	t.Helper()
	seed := filepath.Join(t.TempDir(), "seed")
	require.NoError(t, os.MkdirAll(seed, 0o750))
	gitAt(t, seed, "init", "-b", "main", "-q")
	commitAt(t, seed, "a.go")

	origin := filepath.Join(t.TempDir(), "origin.git")
	gitAt(t, seed, "clone", "--bare", "-q", seed, origin)

	checkout = filepath.Join(t.TempDir(), "proj")
	gitAt(t, seed, "clone", "-q", origin, checkout)
	elsewhere = filepath.Join(t.TempDir(), "other")
	gitAt(t, seed, "clone", "-q", origin, elsewhere)
	return checkout, elsewhere
}

func TestAWorktreeStartsAtTheFetchedBaseNotAStaleHead(t *testing.T) {
	root, elsewhere := cloned(t)
	commitAt(t, elsewhere, "theirs.go")
	gitAt(t, elsewhere, "push", "-q", "origin", "main")

	path := gitdiff.WorktreePath(root, "fix")
	added, err := gitdiff.EnsureWorktreeFrom(context.Background(), root, path, "fix", from(root))
	require.NoError(t, err)
	assert.True(t, added)
	t.Cleanup(func() { _ = gitdiff.RemoveWorktreeForce(context.Background(), root, path) })

	// The checkout never pulled, so only the fetch inside freshBase can have
	// put this commit in the new worktree.
	assert.FileExists(t, filepath.Join(path, "theirs.go"))
}

func TestAWorktreeMadeInsideAnotherStartsAtTheBase(t *testing.T) {
	root, _ := cloned(t)
	first := gitdiff.WorktreePath(root, "first")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), root, first, "first"))
	commitAt(t, first, "first.go")

	// Run from inside the first worktree, which is what ade create does when
	// you start the second ticket without leaving the first.
	second := gitdiff.WorktreePath(root, "second")
	_, err := gitdiff.EnsureWorktreeFrom(context.Background(), first, second, "second", from(first))
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(second, "first.go"),
		"the second ticket is not built on the first")
}

func TestARepositoryWithNoOriginStillGetsAWorktree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(root, 0o750))
	gitAt(t, root, "init", "-b", "main", "-q")
	commitAt(t, root, "a.go")

	path := gitdiff.WorktreePath(root, "fix")
	added, err := gitdiff.EnsureWorktreeFrom(context.Background(), root, path, "fix", from(root))
	require.NoError(t, err)
	assert.True(t, added)
	assert.FileExists(t, filepath.Join(path, "a.go"), "the fetch failed, so the local main stood in")
}

// Every design in the store is a design.md, so slugging the file alone put
// each one on a worktree called "design".
func TestAStoreDesignGetsAWorktreeNamedAfterItsDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(root, 0o750))
	gitAt(t, root, "init", "-b", "main", "-q")
	commitAt(t, root, "a.go")

	path, _, branch, _, err := intoWorktree(context.Background(), root, "cart-redesign/design.md", "main")
	require.NoError(t, err)
	t.Cleanup(func() { _ = gitdiff.RemoveWorktreeForce(context.Background(), root, path) })

	assert.Equal(t, "cart-redesign", branch)
	assert.Equal(t, gitdiff.WorktreePath(root, "cart-redesign"), path)
}

// docs.Ensure answers with a bare file name, so Start work on a store design
// reached intoWorktree with "design.md" and no directory to be named after.
func TestWhatEnsureAnswersStillNamesTheWorktree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(root, 0o750))
	gitAt(t, root, "init", "-b", "main", "-q")
	commitAt(t, root, "a.go")

	dir := filepath.Join(t.TempDir(), "designs", "cart-redesign")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, docs.StoreDoc), []byte("# Cart\n"), 0o600))

	file, _, err := docs.Ensure(dir, "cart-redesign", "")
	require.NoError(t, err)
	require.Equal(t, docs.StoreDoc, file)

	path, _, branch, _, err := intoWorktree(context.Background(), root, filepath.Join(dir, file), "main")
	require.NoError(t, err)
	t.Cleanup(func() { _ = gitdiff.RemoveWorktreeForce(context.Background(), root, path) })

	assert.Equal(t, "cart-redesign", branch)
}

// A branch left behind by an earlier run, with its worktree gone, made Start
// work fail: `worktree add -b` refuses a name the repository already has.
func TestABranchWithNoWorktreeIsCheckedOutRatherThanRefused(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(root, 0o750))
	gitAt(t, root, "init", "-b", "main", "-q")
	commitAt(t, root, "a.go")
	gitAt(t, root, "branch", "trk-1334")

	path, _, branch, added, err := intoWorktree(context.Background(), root, "docs/TRK-1334.md", "main")
	require.NoError(t, err)
	t.Cleanup(func() { _ = gitdiff.RemoveWorktreeForce(context.Background(), root, path) })

	assert.Equal(t, "trk-1334", branch)
	assert.True(t, added, "the checkout is new even though the branch is not")
}

// The branch is already checked out somewhere, and git allows only one
// checkout of a branch, so that worktree is where the work belongs.
func TestAnExistingWorktreeOnTheBranchIsWhereTheWorkLands(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(root, 0o750))
	gitAt(t, root, "init", "-b", "main", "-q")
	commitAt(t, root, "a.go")

	first := gitdiff.WorktreePath(root, "trk-1334")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), root, first, "trk-1334"))
	t.Cleanup(func() { _ = gitdiff.RemoveWorktreeForce(context.Background(), root, first) })

	path, _, branch, added, err := intoWorktree(context.Background(), root, "docs/TRK-1334.md", "main")
	require.NoError(t, err)

	assert.Equal(t, first, path)
	assert.Equal(t, "trk-1334", branch)
	assert.False(t, added)
}

func TestComingBackToATicketIsNotRebased(t *testing.T) {
	root, _ := cloned(t)
	path := gitdiff.WorktreePath(root, "fix")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), root, path, "fix"))
	commitAt(t, path, "mine.go")
	head, err := gitdiff.HeadSHA(context.Background(), path)
	require.NoError(t, err)

	asked := 0
	added, err := gitdiff.EnsureWorktreeFrom(context.Background(), root, path, "fix", func() string {
		asked++
		return freshBase(context.Background(), root)
	})
	require.NoError(t, err)
	assert.False(t, added)
	assert.Zero(t, asked, "reuse must not wait on a fetch it has no use for")

	again, err := gitdiff.HeadSHA(context.Background(), path)
	require.NoError(t, err)
	assert.Equal(t, head, again, "an existing worktree is reused as it is; ade sync moves it")
}
