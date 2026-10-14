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

// overlapRepo is main, a branch "one" with two commits, and a branch "two"
// with a commit of its own and then a cherry-pick of one's first. The cherry
// -pick needs its own parent to get its own SHA: picked straight onto main it
// would be byte-for-byte the commit it copied.
func overlapRepo(t *testing.T) (dir string, git func(args ...string)) {
	t.Helper()
	dir = t.TempDir()

	git = func(args ...string) {
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
	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	git("init", "-b", "main", "-q")
	write("base.go", "package main\n")
	git("add", ".")
	git("commit", "-qm", "base")

	git("checkout", "-qb", "one")
	write("a.go", "package main\n\nfunc a() {}\n")
	git("add", ".")
	git("commit", "-qm", "add a")
	write("b.go", "package main\n\nfunc b() {}\n")
	git("add", ".")
	git("commit", "-qm", "add b")

	git("checkout", "-q", "main")
	git("checkout", "-qb", "two")
	write("own.go", "package main\n\nfunc own() {}\n")
	git("add", ".")
	git("commit", "-qm", "two's own work")
	git("cherry-pick", "one~1")
	return dir, git
}

func TestCommitsListsTheRangeNewestFirst(t *testing.T) {
	dir, _ := overlapRepo(t)

	got, err := Commits(context.Background(), dir, "main", "one")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "add b", got[0].Subject, "newest first")
	assert.Equal(t, "add a", got[1].Subject)
	assert.Equal(t, "t", got[0].Author)
	assert.False(t, got[0].At.IsZero(), "the commit time is read")
}

func TestCommitsIsEmptyForABranchWithNothingOfItsOwn(t *testing.T) {
	dir, _ := overlapRepo(t)

	got, err := Commits(context.Background(), dir, "one", "main")
	require.NoError(t, err)
	assert.Empty(t, got)
}

// The point of the patch-id: the same work on two branches under two SHAs is
// one patch, and that is how a cherry-picked worktree is found.
func TestACherryPickKeepsThePatchIDAndChangesTheSHA(t *testing.T) {
	dir, _ := overlapRepo(t)
	ctx := context.Background()

	one, err := Commits(ctx, dir, "main", "one")
	require.NoError(t, err)
	two, err := Commits(ctx, dir, "main", "two")
	require.NoError(t, err)
	require.Len(t, two, 2)

	picked := findCommit(t, one, "add a")
	copied := findCommit(t, two, "add a")
	assert.NotEqual(t, picked.SHA, copied.SHA, "a cherry-pick is its own commit")
	assert.NotEmpty(t, copied.PatchID)
	assert.Equal(t, picked.PatchID, copied.PatchID, "the same diff is the same patch")

	other := findCommit(t, one, "add b")
	assert.NotEqual(t, other.PatchID, copied.PatchID, "a different diff is not")
}

func TestPatchIDsKeyEveryCommitOfTheRange(t *testing.T) {
	dir, _ := overlapRepo(t)

	ids, err := PatchIDs(context.Background(), dir, "main", "two")
	require.NoError(t, err)
	assert.Len(t, ids, 2, "one entry per commit, from one call")
}

func TestPatchIDsAreEmptyWithNothingInTheRange(t *testing.T) {
	dir, _ := overlapRepo(t)

	ids, err := PatchIDs(context.Background(), dir, "one", "main")
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestCommitsRefusesAnIncompleteRange(t *testing.T) {
	dir, _ := overlapRepo(t)

	_, err := Commits(context.Background(), dir, "", "one")
	require.Error(t, err)
}

func TestCommitsFailsOnAnUnknownRef(t *testing.T) {
	dir, _ := overlapRepo(t)

	_, err := Commits(context.Background(), dir, "main", "nope")
	require.Error(t, err)
}

// A subject with a NUL-unsafe character in it still parses: the format
// separates on NUL precisely so a subject can hold anything else.
func TestCommitSubjectsSurviveAwkwardCharacters(t *testing.T) {
	dir, git := overlapRepo(t)
	git("checkout", "-q", "one")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "c.go"), []byte("package main\n"), 0o600))
	git("add", ".")
	git("commit", "-qm", "fix: a|b\ttab and \"quotes\"")

	got, err := Commits(context.Background(), dir, "main", "one")
	require.NoError(t, err)
	assert.Equal(t, `fix: a|b	tab and "quotes"`, got[0].Subject)
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
}

func findCommit(t *testing.T, commits []Rev, subject string) Rev {
	t.Helper()
	for _, c := range commits {
		if c.Subject == subject {
			return c
		}
	}
	t.Fatalf("no commit %q in %v", subject, commits)
	return Rev{}
}

// A squash merge is the case every cheaper test misses: the work is on the
// base and not one of the branch's commits is.
func TestLandedInSeesASquashMergedBranch(t *testing.T) {
	dir, git := overlapRepo(t)
	ctx := context.Background()

	git("checkout", "-q", "main")
	git("merge", "--squash", "-q", "one")
	git("commit", "-qm", "squashed one")

	ahead, err := Commits(ctx, dir, "main", "one")
	require.NoError(t, err)
	require.NotEmpty(t, ahead, "the branch still looks like it holds work")

	landed, err := LandedIn(ctx, dir, "main", "one")
	require.NoError(t, err)
	assert.True(t, landed, "merging it into main would change nothing")
}

func TestLandedInIsFalseForABranchWithWorkOfItsOwn(t *testing.T) {
	dir, _ := overlapRepo(t)

	landed, err := LandedIn(context.Background(), dir, "main", "one")
	require.NoError(t, err)
	assert.False(t, landed)
}

// A branch whose patch is not on the base is not landed, however badly it
// would merge. The old merge-tree test called every conflict "not landed",
// which was also false for a branch that had landed and then drifted.
func TestLandedInIsFalseForABranchThatConflicts(t *testing.T) {
	dir, git := overlapRepo(t)
	ctx := context.Background()

	git("checkout", "-q", "main")
	write(t, dir, "a.go", "package main\n\nvar ours = 1\n")
	git("add", ".")
	git("commit", "-qm", "ours")
	git("checkout", "-qb", "clash", "main~1")
	write(t, dir, "a.go", "package main\n\nvar theirs = 2\n")
	git("add", ".")
	git("commit", "-qm", "theirs")

	landed, err := LandedIn(ctx, dir, "main", "clash")
	require.NoError(t, err)
	assert.False(t, landed)
}

func TestLandedInIsTrueForABranchAtTheBase(t *testing.T) {
	dir, _ := overlapRepo(t)

	landed, err := LandedIn(context.Background(), dir, "main", "main")
	require.NoError(t, err)
	assert.True(t, landed)
}

// The dangerous false positive: squash-merged, then worked on again. Calling
// that landed would offer to remove a worktree holding the only copy.
func TestLandedInIsFalseWhenWorkFollowedTheSquash(t *testing.T) {
	dir, git := overlapRepo(t)
	ctx := context.Background()

	git("checkout", "-q", "main")
	git("merge", "--squash", "-q", "one")
	git("commit", "-qm", "squashed one")

	git("checkout", "-q", "one")
	write(t, dir, "c.go", "package main\n\nfunc c() {}\n")
	git("add", ".")
	git("commit", "-qm", "and more after the merge")

	landed, err := LandedIn(ctx, dir, "main", "one")
	require.NoError(t, err)
	assert.False(t, landed, "the last commit is nowhere else")
}

// A branch the base moved past still counts: its patch is upstream under
// another SHA, which is all removing the worktree needs.
func TestLandedInSeesABranchLeftBehindByTheBase(t *testing.T) {
	dir, git := overlapRepo(t)
	ctx := context.Background()

	git("checkout", "-q", "main")
	git("merge", "--squash", "-q", "one")
	git("commit", "-qm", "squashed one")
	write(t, dir, "later.go", "package main\n\nfunc later() {}\n")
	git("add", ".")
	git("commit", "-qm", "work that came after")

	landed, err := LandedIn(ctx, dir, "main", "one")
	require.NoError(t, err)
	assert.True(t, landed, "being behind is not holding work of its own")
}
