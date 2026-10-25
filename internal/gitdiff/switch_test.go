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

// gitAt runs git in a repository the test owns, with an identity: a commit
// needs one and the machine's own is not the test's to rely on.
func gitAt(t *testing.T, dir string, args ...string) {
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

// clean is testRepo with its deliberate mess put back: these tests make their
// own dirt and a leftover edit is somebody else's fixture.
func clean(t *testing.T) string {
	t.Helper()
	dir := testRepo(t)
	gitAt(t, dir, "checkout", "-q", "--", ".")
	require.NoError(t, os.Remove(filepath.Join(dir, "scratch.go")))
	gitAt(t, dir, "checkout", "-q", "main")
	return dir
}

func wrote(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
}

func TestCheckoutBranchCarriesWorkThatDoesNotConflict(t *testing.T) {
	dir := clean(t)
	ctx := context.Background()

	// A file neither branch has: git takes it across.
	wrote(t, dir, "kept.go", "package main\n\nfunc a() {}\n// mine\n")

	require.NoError(t, CheckoutBranch(ctx, dir, "feature"))
	assert.Equal(t, "feature", Branch(ctx, dir))

	body, err := os.ReadFile(filepath.Join(dir, "kept.go"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "// mine", "the edit came with the switch")
}

func TestCheckoutBranchSaysWhenLocalChangesAreInTheWay(t *testing.T) {
	dir := clean(t)
	ctx := context.Background()

	// base.go differs between main and feature, so an edit to it blocks.
	wrote(t, dir, "base.go", "package main\n\nvar x = 99\n")

	err := CheckoutBranch(ctx, dir, "feature")
	require.ErrorIs(t, err, ErrLocalChanges)
	assert.Equal(t, "main", Branch(ctx, dir), "a refusal leaves the branch alone")
}

func TestCheckoutBranchRefusesANameGitWouldNot(t *testing.T) {
	err := CheckoutBranch(context.Background(), clean(t), "not a branch")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a branch name git will take")
}

func TestAStashIsFoundByItsCommitAndNotByItsIndex(t *testing.T) {
	dir := clean(t)
	ctx := context.Background()
	wrote(t, dir, "base.go", "package main\n\nvar x = 99\n")

	sha, err := StashKeep(ctx, dir, "ade: off main")
	require.NoError(t, err)
	require.NotEmpty(t, sha)
	assert.Equal(t, []string{"base.go"}, StashFiles(ctx, dir, sha))
	assert.True(t, StashHeld(ctx, dir, sha))

	// Somebody else's entry, pushed on top: ours is no longer stash@{0}.
	wrote(t, dir, "kept.go", "package main\n\nfunc a() {}\n// theirs\n")
	other, err := StashKeep(ctx, dir, "someone else")
	require.NoError(t, err)
	require.NotEqual(t, sha, other)

	stuck, err := StashApplyAt(ctx, dir, sha)
	require.NoError(t, err)
	assert.Empty(t, stuck)
	body, err := os.ReadFile(filepath.Join(dir, "base.go"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "var x = 99", "ours came back, not theirs")

	require.NoError(t, StashDropAt(ctx, dir, sha))
	assert.False(t, StashHeld(ctx, dir, sha))
	assert.True(t, StashHeld(ctx, dir, other), "the other entry is left alone")
}

func TestStashKeepSaysNothingWasSetAsideWhenTheTreeIsClean(t *testing.T) {
	dir := clean(t)
	sha, err := StashKeep(context.Background(), dir, "ade: nothing")
	require.NoError(t, err)
	assert.Empty(t, sha)
}

// The record outlives the entry: someone drops it by hand, and applying has to
// say so rather than report work put back that is not there.
func TestApplyingAStashThatHasGoneSaysSo(t *testing.T) {
	dir := clean(t)
	ctx := context.Background()
	wrote(t, dir, "base.go", "package main\n\nvar x = 99\n")

	sha, err := StashKeep(ctx, dir, "ade: off main")
	require.NoError(t, err)
	require.NoError(t, StashDropAt(ctx, dir, sha))

	_, err = StashApplyAt(ctx, dir, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no longer in the stash")

	// Dropping what has already gone is not a failure.
	assert.NoError(t, StashDropAt(ctx, dir, sha))
}

func TestStashApplyAtNamesWhatConflicted(t *testing.T) {
	dir := clean(t)
	ctx := context.Background()
	wrote(t, dir, "base.go", "package main\n\nvar x = 99\n")

	sha, err := StashKeep(ctx, dir, "ade: off main")
	require.NoError(t, err)

	// The same file, changed another way, with the stash still holding its own.
	wrote(t, dir, "base.go", "package main\n\nvar x = 7\n")
	gitAt(t, dir, "commit", "-qam", "someone else got there first")

	stuck, err := StashApplyAt(ctx, dir, sha)
	require.NoError(t, err)
	assert.Equal(t, []string{"base.go"}, stuck)
	assert.True(t, StashHeld(ctx, dir, sha), "a conflicted apply leaves the entry")
}

func TestBranchesByDateIsNewestFirst(t *testing.T) {
	dir := clean(t)
	found, err := BranchesByDate(context.Background(), dir)
	require.NoError(t, err)
	require.Len(t, found, 2)
	assert.Equal(t, "feature", found[0].Name, "the branch committed to last comes first")
	assert.Equal(t, "main", found[1].Name)
	assert.NotEmpty(t, found[0].At)
}
