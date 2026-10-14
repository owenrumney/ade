package gitdiff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

// prRepo clones testRepo into a bare origin and publishes feature as the head
// of pull request 7, which is what GitHub serves at refs/pull/7/head.
func prRepo(t *testing.T) string {
	t.Helper()
	source := testRepo(t)
	origin := filepath.Join(t.TempDir(), "origin.git")

	gitIn(t, source, "clone", "--bare", "-q", source, origin)
	gitIn(t, origin, "update-ref", "refs/pull/7/head", "refs/heads/feature")

	clone := filepath.Join(t.TempDir(), "work")
	gitIn(t, t.TempDir(), "clone", "-q", "-b", "main", origin, clone)
	return clone
}

func TestTrackedListsOnlyWhatGitKnows(t *testing.T) {
	files, err := Tracked(context.Background(), testRepo(t))

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"added.go", "base.go", "kept.go"}, files)
	assert.NotContains(t, files, "scratch.go")
}

// Readable is what Explore reads from, so it has to cover the new files the
// diff shows without ever reaching an ignored one.
func TestReadableAddsUntrackedButNotIgnored(t *testing.T) {
	dir := testRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("hidden.go\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hidden.go"), []byte("package main\n"), 0o600))

	files, err := Readable(context.Background(), dir)

	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{".gitignore", "added.go", "base.go", "kept.go", "scratch.go"}, files)
}

func TestReviewWorktreePathIsUnderTheGitDir(t *testing.T) {
	dir := testRepo(t)

	path, err := ReviewWorktreePath(context.Background(), dir, "pr-7")

	require.NoError(t, err)
	common, err := CommonGitDir(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(common, "ade", "worktrees", "pr-7"), path)
}

func TestReviewWorktreePathNeedsASlug(t *testing.T) {
	_, err := ReviewWorktreePath(context.Background(), testRepo(t), "")

	assert.Error(t, err)
}

func TestAddPRWorktreeChecksOutTheFetchedHead(t *testing.T) {
	ctx := context.Background()
	dir := prRepo(t)
	path, err := ReviewWorktreePath(ctx, dir, "pr-7")
	require.NoError(t, err)

	require.NoError(t, AddPRWorktree(ctx, dir, path, 7))

	assert.FileExists(t, filepath.Join(path, "added.go"))
	// Detached, so the review never sits on a branch you might push.
	assert.Contains(t, gitOut(t, path, "status", "-sb"), "HEAD (no branch)")
	assert.Error(t, AddPRWorktree(ctx, dir, path, 7), "refuses an existing path")
}

func TestAddPRWorktreeNeedsAPathAndNumber(t *testing.T) {
	dir := prRepo(t)

	assert.Error(t, AddPRWorktree(context.Background(), dir, "", 7))
	assert.Error(t, AddPRWorktree(context.Background(), dir, filepath.Join(dir, "x"), 0))
}

func TestCommitsAheadCountsWorkTheRefDoesNotHave(t *testing.T) {
	ctx := context.Background()
	dir := prRepo(t)
	path, err := ReviewWorktreePath(ctx, dir, "pr-7")
	require.NoError(t, err)
	require.NoError(t, AddPRWorktree(ctx, dir, path, 7))

	n, err := CommitsAhead(ctx, path, "refs/ade/pr-7")
	require.NoError(t, err)
	assert.Zero(t, n)

	require.NoError(t, os.WriteFile(filepath.Join(path, "added.go"), []byte("package main\n"), 0o600))
	gitIn(t, path, "commit", "-qam", "local edit")

	n, err = CommitsAhead(ctx, path, "refs/ade/pr-7")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestCommitsAheadFailsOnAnUnknownRef(t *testing.T) {
	_, err := CommitsAhead(context.Background(), testRepo(t), "refs/ade/nope")

	assert.Error(t, err)
}

func TestRemoveWorktreeAndPrune(t *testing.T) {
	ctx := context.Background()
	dir := prRepo(t)
	path, err := ReviewWorktreePath(ctx, dir, "pr-7")
	require.NoError(t, err)
	require.NoError(t, AddPRWorktree(ctx, dir, path, 7))

	require.NoError(t, RemoveWorktree(ctx, dir, path))
	assert.NoDirExists(t, path)

	// Prune clears the record left when a directory is deleted by hand.
	require.NoError(t, AddPRWorktree(ctx, dir, path, 7))
	require.NoError(t, os.RemoveAll(path))
	require.NoError(t, PruneWorktrees(ctx, dir))
	assert.NotContains(t, gitOut(t, dir, "worktree", "list"), "pr-7")
}

func TestRemoveWorktreeRefusesUncommittedWork(t *testing.T) {
	ctx := context.Background()
	dir := prRepo(t)
	path, err := ReviewWorktreePath(ctx, dir, "pr-7")
	require.NoError(t, err)
	require.NoError(t, AddPRWorktree(ctx, dir, path, 7))
	require.NoError(t, os.WriteFile(filepath.Join(path, "added.go"), []byte("package main\n"), 0o600))

	assert.Error(t, RemoveWorktree(ctx, dir, path))
}
