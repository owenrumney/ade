package worktrees

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// repo is a main checkout on main with one commit, in its own directory so
// worktrees can be added beside it.
func repo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	git(t, dir, "init", "-b", "main", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n"), 0o600))
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "base")
	return dir
}

func add(t *testing.T, root, slug string) string {
	t.Helper()
	path := gitdiff.WorktreePath(root, slug)
	require.NoError(t, gitdiff.AddWorktree(context.Background(), root, path, slug))
	return path
}

func find(t *testing.T, list []Worktree, path string) Worktree {
	t.Helper()
	wt, ok := Find(list, path)
	require.True(t, ok, "%s is not in the scan", path)
	return wt
}

func TestScanLeavesTheMainCheckoutOut(t *testing.T) {
	root := repo(t)
	add(t, root, "one")

	found, err := Scan(context.Background(), root, false)
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.NotEqual(t, root, found[0].Path)
	assert.Equal(t, KindSession, found[0].Kind)
	assert.Equal(t, "one", found[0].Branch)
	assert.False(t, found[0].LastUsed.IsZero())
}

func TestAnUntouchedWorktreeCanGo(t *testing.T) {
	root := repo(t)
	path := add(t, root, "idle")

	// Nothing was done here: no commits beyond main, nothing uncommitted. It
	// is the worktree a session that went nowhere leaves behind.
	wt := find(t, must(Scan(context.Background(), root, false))(t), path)
	assert.Empty(t, wt.Keep)
	assert.True(t, wt.Removable)
	assert.True(t, wt.Merged, "a branch with nothing on it is already in main")

	require.NoError(t, Remove(context.Background(), root, wt))
	assert.NoDirExists(t, path)
	// The branch is the record of the work, and is never touched.
	git(t, root, "rev-parse", "--verify", "idle")
}

func TestUncommittedWorkKeepsAWorktree(t *testing.T) {
	root := repo(t)
	path := add(t, root, "busy")
	require.NoError(t, os.WriteFile(filepath.Join(path, "new.go"), []byte("package main\n"), 0o600))

	// Even under force: an edit that exists nowhere else is not the CLI's to
	// throw away on a flag meant for unpushed commits.
	for _, force := range []bool{false, true} {
		wt := find(t, must(Scan(context.Background(), root, force))(t), path)
		assert.Equal(t, KeepUncommitted, wt.Keep)
		assert.Equal(t, 1, wt.Uncommitted)
		assert.False(t, wt.Removable, "force=%v", force)
		assert.Error(t, Remove(context.Background(), root, wt))
	}
	assert.DirExists(t, path)
}

func TestUnpushedCommitsKeepAWorktreeUnlessForced(t *testing.T) {
	root := repo(t)
	path := add(t, root, "work")
	require.NoError(t, os.WriteFile(filepath.Join(path, "b.go"), []byte("package main\n"), 0o600))
	git(t, path, "add", ".")
	git(t, path, "commit", "-qm", "work")

	// Never pushed and not in main: the commit exists only on this branch,
	// which removing the worktree keeps — so force may take the checkout.
	wt := find(t, must(Scan(context.Background(), root, false))(t), path)
	assert.Equal(t, KeepUnpushed, wt.Keep)
	assert.False(t, wt.Merged)
	assert.Positive(t, wt.Unpushed)
	assert.False(t, wt.Removable)

	forced := find(t, must(Scan(context.Background(), root, true))(t), path)
	assert.True(t, forced.Removable)
	require.NoError(t, Remove(context.Background(), root, forced))
	assert.NoDirExists(t, path)
	git(t, root, "rev-parse", "--verify", "work")
}

func TestAMergedBranchCanGo(t *testing.T) {
	root := repo(t)
	path := add(t, root, "done")
	require.NoError(t, os.WriteFile(filepath.Join(path, "b.go"), []byte("package main\n"), 0o600))
	git(t, path, "add", ".")
	git(t, path, "commit", "-qm", "done")
	git(t, root, "merge", "-q", "done")

	// Merged into main, so nothing on the branch is only on the branch.
	wt := find(t, must(Scan(context.Background(), root, false))(t), path)
	assert.True(t, wt.Merged)
	assert.Empty(t, wt.Keep)
	assert.True(t, wt.Removable)
}

func TestAWorktreeOffAnUnmergedBranchWithNothingOfItsOwnCanGo(t *testing.T) {
	root := repo(t)
	git(t, root, "checkout", "-qb", "feature")
	require.NoError(t, os.WriteFile(filepath.Join(root, "f.go"), []byte("package main\n"), 0o600))
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "feature")
	path := add(t, root, "spike")

	// The worktree was branched from feature, which main does not have. That
	// commit is feature's, not the worktree's: nothing here exists only here.
	wt := find(t, must(Scan(context.Background(), root, false))(t), path)
	assert.Empty(t, wt.Keep)
	assert.True(t, wt.Removable)
}

func TestFindOnlyMatchesWhatWasScanned(t *testing.T) {
	list := []Worktree{{Path: "/a"}, {Path: "/b"}}
	_, ok := Find(list, "/b")
	assert.True(t, ok)
	_, ok = Find(list, "/etc")
	assert.False(t, ok)
}

func must[T any](v T, err error) func(t *testing.T) T {
	return func(t *testing.T) T {
		t.Helper()
		require.NoError(t, err)
		return v
	}
}

// A session branch has no upstream of its own until it is pushed, so what
// keeps its worktree is "work that exists nowhere else", not "ahead of
// origin/main". The two used to be confused: the branch tracked origin/main,
// so Ahead counted against main and called it unpushed.
func TestANeverPushedWorktreeIsKeptForWorkNothingElseHolds(t *testing.T) {
	root := repo(t)
	ctx := context.Background()
	path := add(t, root, "one")

	// Nothing committed: nothing is held only here.
	found, err := Scan(ctx, root, false)
	require.NoError(t, err)
	wt := find(t, found, path)
	assert.Zero(t, wt.Unpushed)
	assert.True(t, wt.Merged, "every commit it has is on another branch too")
	assert.True(t, wt.Removable)

	require.NoError(t, os.WriteFile(filepath.Join(path, "new.go"), []byte("package main\n"), 0o600))
	git(t, path, "add", ".")
	git(t, path, "commit", "-qm", "work only here")

	found, err = Scan(ctx, root, false)
	require.NoError(t, err)
	wt = find(t, found, path)
	assert.Equal(t, 1, wt.Unpushed, "one commit exists nowhere else")
	assert.False(t, wt.Merged)
	assert.Equal(t, KeepUnpushed, wt.Keep)
	assert.False(t, wt.Removable)
}
