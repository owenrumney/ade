package gitdiff

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The editor draws its own red and green from two whole files, so each scope
// has to answer with the ends that scope actually compares.
func TestBlobAnswersBothEndsOfEachScope(t *testing.T) {
	dir := singleLineRepo(t,
		map[string]string{"a.txt": "one\n"},
		map[string]string{"a.txt": "two\n"})
	// Uncommitted work on top of the commit, so the three scopes differ.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("three\n"), 0o600))

	ctx := context.Background()
	read := func(scope, side string) string {
		out, err := Blob(ctx, dir, Options{Repo: dir, Base: "main", Scope: scope}, "a.txt", side)
		require.NoError(t, err)
		return string(out)
	}

	assert.Equal(t, "one\n", read(ScopeBranch, SideLeft), "the branch starts at the base")
	assert.Equal(t, "three\n", read(ScopeBranch, SideRight), "and ends at the working tree")

	assert.Equal(t, "one\n", read(ScopeCommitted, SideLeft))
	assert.Equal(t, "two\n", read(ScopeCommitted, SideRight), "committed stops at HEAD")

	assert.Equal(t, "two\n", read(ScopeUncommitted, SideLeft), "uncommitted starts at HEAD")
	assert.Equal(t, "three\n", read(ScopeUncommitted, SideRight))
}

func TestBlobOnFilesThatOnlyHaveOneSide(t *testing.T) {
	dir := singleLineRepo(t,
		map[string]string{"gone.txt": "bye\n"},
		map[string]string{"gone.txt": "", "new.txt": "hello\n"})
	ctx := context.Background()
	opts := Options{Repo: dir, Base: "main", Scope: ScopeBranch}

	// An added file has no left and a deleted one no right. Both read as
	// empty: a diff viewer wants one blank side, not a failure.
	added, err := Blob(ctx, dir, opts, "new.txt", SideLeft)
	require.NoError(t, err)
	assert.Empty(t, added)

	removed, err := Blob(ctx, dir, opts, "gone.txt", SideRight)
	require.NoError(t, err)
	assert.Empty(t, removed)

	kept, err := Blob(ctx, dir, opts, "gone.txt", SideLeft)
	require.NoError(t, err)
	assert.Equal(t, "bye\n", string(kept))
}

// A rev range names both ends, so neither side is the working tree.
func TestBlobOnARevRangeReadsBothCommits(t *testing.T) {
	dir := singleLineRepo(t,
		map[string]string{"a.txt": "one\n"},
		map[string]string{"a.txt": "two\n"})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("three\n"), 0o600))

	ctx := context.Background()
	opts := Options{Repo: dir, Rev: "main..feature"}
	left, err := Blob(ctx, dir, opts, "a.txt", SideLeft)
	require.NoError(t, err)
	assert.Equal(t, "one\n", string(left))

	right, err := Blob(ctx, dir, opts, "a.txt", SideRight)
	require.NoError(t, err)
	assert.Equal(t, "two\n", string(right), "the range ends at feature, not at the working tree")
}

// A ref git cannot read is a failure, not an empty side: as an empty side the
// editor draws every line of every file as added.
func TestBlobSaysSoWhenTheRefIsNotThere(t *testing.T) {
	dir := singleLineRepo(t, map[string]string{"a.txt": "one\n"}, map[string]string{"a.txt": "two\n"})

	_, err := Blob(context.Background(), dir, Options{Repo: dir, Rev: "no-such-ref"}, "a.txt", SideLeft)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotInDiff)
}

// A symlink is a path out of the repository that reads as one inside it, and
// os.ReadFile follows it.
func TestBlobRefusesASymlinkOutOfTheRepository(t *testing.T) {
	dir := singleLineRepo(t, map[string]string{"a.txt": "one\n"}, map[string]string{"a.txt": "two\n"})
	secret := filepath.Join(t.TempDir(), "id_ed25519")
	require.NoError(t, os.WriteFile(secret, []byte("private\n"), 0o600))
	require.NoError(t, os.Symlink(secret, filepath.Join(dir, "link.txt")))

	opts := Options{Repo: dir, Base: "main", Scope: ScopeBranch}
	_, err := Blob(context.Background(), dir, opts, "link.txt", SideRight)
	assert.ErrorIs(t, err, ErrNotInDiff)
}

// The path comes off a request, so it is the one thing here that is not
// already known to be inside the repository.
func TestBlobRefusesAPathOutsideTheRepository(t *testing.T) {
	dir := singleLineRepo(t, map[string]string{"a.txt": "one\n"}, map[string]string{"a.txt": "two\n"})
	opts := Options{Repo: dir, Base: "main", Scope: ScopeBranch}

	for _, path := range []string{"../escape", "/etc/passwd", "", "  ", "a/../../out"} {
		_, err := Blob(context.Background(), dir, opts, path, SideRight)
		assert.ErrorIs(t, err, ErrNotInDiff, "path %q", path)
	}
}
