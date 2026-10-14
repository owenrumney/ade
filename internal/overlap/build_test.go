package overlap

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

// A rebase and a cherry-pick need a git identity, which a machine may not have.
func TestMain(m *testing.M) {
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		if err := os.Setenv(k, "t"); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}

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

func repo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	git(t, dir, "init", "-b", "main", "-q")
	write(t, dir, "a.go", "package main\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "base")
	return dir
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

// commit adds a worktree on its own branch and puts one commit in it.
func commit(t *testing.T, root, slug, file, body string) string {
	t.Helper()
	path := gitdiff.WorktreePath(root, slug)
	require.NoError(t, gitdiff.AddWorktree(context.Background(), root, path, slug))
	write(t, path, file, body)
	git(t, path, "add", ".")
	git(t, path, "commit", "-qm", slug+": "+file)
	return path
}

func nodeAt(t *testing.T, g Graph, path string) Node {
	t.Helper()
	for _, n := range g.Nodes {
		if n.Path == path {
			return n
		}
	}
	t.Fatalf("no node for %s in %v", path, g.Nodes)
	return Node{}
}

func TestBuildMeasuresEveryWorktreeAgainstTheBase(t *testing.T) {
	root := repo(t)
	one := commit(t, root, "one", "one.go", "package main\n\nfunc one() {}\n")
	empty := gitdiff.WorktreePath(root, "empty")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), root, empty, "empty"))

	g, err := Build(context.Background(), root)
	require.NoError(t, err)
	assert.Equal(t, "main", g.BaseBranch)

	assert.Len(t, nodeAt(t, g, one).Commits, 1)
	assert.False(t, nodeAt(t, g, one).Redundant)

	assert.Empty(t, nodeAt(t, g, empty).Commits)
	assert.True(t, nodeAt(t, g, empty).Redundant, "a branch off base holds nothing of its own")
}

func TestBuildIncludesTheMainCheckoutAndNeverCallsItRedundant(t *testing.T) {
	root := repo(t)
	commit(t, root, "one", "one.go", "package main\n")

	g, err := Build(context.Background(), root)
	require.NoError(t, err)

	main := nodeAt(t, g, root)
	assert.True(t, main.Main)
	assert.False(t, main.Redundant)
}

// The whole point, end to end: the same work in two worktrees under two SHAs.
func TestBuildFindsACherryPickBetweenTwoWorktrees(t *testing.T) {
	root := repo(t)
	body := "package main\n\nfunc shared() {}\n"
	one := commit(t, root, "one", "shared.go", body)
	// Its own commit first, so the pick gets its own parent and its own SHA.
	two := commit(t, root, "two", "own.go", "package main\n")
	git(t, two, "cherry-pick", "one")

	g, err := Build(context.Background(), root)
	require.NoError(t, err)

	dupes := kinds(g.Edges, KindDuplicates)
	require.Len(t, dupes, 1, "edges: %v", g.Edges)
	assert.Equal(t, 1, dupes[0].Count)
	assert.ElementsMatch(t, []string{one, two}, []string{dupes[0].From, dupes[0].To})
	assert.Empty(t, kinds(g.Edges, KindShares), "no commit is on both by SHA")
}

func TestBuildFindsAWorktreeWhoseWorkIsAlreadyOnAnother(t *testing.T) {
	root := repo(t)
	ahead := commit(t, root, "ahead", "x.go", "package main\n\nfunc x() {}\n")
	write(t, ahead, "y.go", "package main\n\nfunc y() {}\n")
	git(t, ahead, "add", ".")
	git(t, ahead, "commit", "-qm", "and y")

	// A worktree started from ahead's first commit: everything it has, ahead
	// has too.
	behind := gitdiff.WorktreePath(root, "behind")
	require.NoError(t, gitdiff.AddWorktreeFrom(context.Background(), root, behind, "behind", "ahead~1"))

	g, err := Build(context.Background(), root)
	require.NoError(t, err)

	absorbed := kinds(g.Edges, KindAbsorbed)
	require.Len(t, absorbed, 1, "edges: %v", g.Edges)
	assert.Equal(t, behind, absorbed[0].From)
	assert.Equal(t, ahead, absorbed[0].To)
	assert.Equal(t, []string{behind}, Removable(g))
}

func TestBuildFailsOutsideARepository(t *testing.T) {
	_, err := Build(context.Background(), t.TempDir())
	require.Error(t, err)
}

func TestACacheDoesNotReReadAWorktreeThatHasNotMoved(t *testing.T) {
	root := repo(t)
	one := commit(t, root, "one", "one.go", "package main\n")
	cache := new(Cache)

	first, err := cache.Build(context.Background(), root)
	require.NoError(t, err)
	again, err := cache.Build(context.Background(), root)
	require.NoError(t, err)
	// Not the whole graph: LastUsed comes from the scan and moves every time
	// git touches a worktree, which reading it does.
	assert.Equal(t, first.Edges, again.Edges)
	assert.Equal(t, nodeAt(t, first, one).Commits, nodeAt(t, again, one).Commits)

	write(t, one, "two.go", "package main\n\nfunc two() {}\n")
	git(t, one, "add", ".")
	git(t, one, "commit", "-qm", "and two")

	moved, err := cache.Build(context.Background(), root)
	require.NoError(t, err)
	assert.Len(t, nodeAt(t, moved, one).Commits, 2, "a moved tip is read again")
}

// The base moving invalidates every worktree: base..branch is a different
// range even where no branch moved.
func TestACacheIsDroppedWhenTheBaseMoves(t *testing.T) {
	root := repo(t)
	one := commit(t, root, "one", "one.go", "package main\n\nfunc one() {}\n")
	cache := new(Cache)

	first, err := cache.Build(context.Background(), root)
	require.NoError(t, err)
	assert.Len(t, nodeAt(t, first, one).Commits, 1)

	// one's commit lands on the base, so it now holds nothing of its own.
	git(t, root, "merge", "-q", "--no-edit", "one")

	after, err := cache.Build(context.Background(), root)
	require.NoError(t, err)
	assert.Empty(t, nodeAt(t, after, one).Commits)
	assert.True(t, nodeAt(t, after, one).Redundant)
}

func TestACacheForgetsAWorktreeThatHasGone(t *testing.T) {
	root := repo(t)
	one := commit(t, root, "one", "one.go", "package main\n")
	cache := new(Cache)

	_, err := cache.Build(context.Background(), root)
	require.NoError(t, err)
	require.NoError(t, gitdiff.RemoveWorktreeForce(context.Background(), root, one))

	_, err = cache.Build(context.Background(), root)
	require.NoError(t, err)

	cache.mu.Lock()
	defer cache.mu.Unlock()
	assert.NotContains(t, cache.held, one)
}

// The case every cheaper test misses: the forge squashed the branch, so the
// work is on the base and not one of the branch's commits is.
func TestBuildSeesASquashMergedWorktreeAsRemovable(t *testing.T) {
	root := repo(t)
	one := commit(t, root, "one", "one.go", "package main\n\nfunc one() {}\n")
	git(t, root, "merge", "--squash", "-q", "one")
	git(t, root, "commit", "-qm", "squashed one")

	g, err := Build(context.Background(), root)
	require.NoError(t, err)

	node := nodeAt(t, g, one)
	assert.NotEmpty(t, node.Commits, "its commits are still only on it")
	assert.False(t, node.Redundant, "which is why redundant does not catch it")
	assert.True(t, node.Landed, "but merging it into the base would change nothing")
	assert.Contains(t, Removable(g), one)
}

func TestBuildDoesNotCallAWorktreeWithItsOwnWorkLanded(t *testing.T) {
	root := repo(t)
	one := commit(t, root, "one", "one.go", "package main\n\nfunc one() {}\n")

	g, err := Build(context.Background(), root)
	require.NoError(t, err)
	assert.False(t, nodeAt(t, g, one).Landed)
	assert.Empty(t, Removable(g))
}

// The verdict has to survive the cache, which keys on the branch tip: a
// squash lands on the base and moves no branch tip at all.
func TestACacheSeesASquashThatMovedNoBranchTip(t *testing.T) {
	root := repo(t)
	one := commit(t, root, "one", "one.go", "package main\n\nfunc one() {}\n")
	cache := new(Cache)

	first, err := cache.Build(context.Background(), root)
	require.NoError(t, err)
	assert.False(t, nodeAt(t, first, one).Landed)

	git(t, root, "merge", "--squash", "-q", "one")
	git(t, root, "commit", "-qm", "squashed one")

	after, err := cache.Build(context.Background(), root)
	require.NoError(t, err)
	assert.True(t, nodeAt(t, after, one).Landed, "the base moved, so the read is redone")
}
