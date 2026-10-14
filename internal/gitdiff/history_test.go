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

// renamedRepo is one file written three times and renamed once, so --follow
// has something to follow and the old name is a real answer.
func renamedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	git := func(args ...string) {
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
	write("cart.go", "package cart\n\nvar total = 1\n")
	git("add", ".")
	git("commit", "-qm", "add the cart")
	write("cart.go", "package cart\n\nvar total = 2\n")
	git("add", ".")
	git("commit", "-qm", "bump the total")
	git("mv", "cart.go", "basket.go")
	git("commit", "-qm", "rename the cart")
	write("basket.go", "package cart\n\nvar total = 3\n")
	write("other.go", "package cart\n")
	git("add", ".")
	git("commit", "-qm", "bump again and add another file")
	return dir
}

func TestHistoryFollowsAFileThroughItsRename(t *testing.T) {
	dir := renamedRepo(t)

	past, err := History(context.Background(), dir, "basket.go", 0)
	require.NoError(t, err)
	require.Len(t, past, 4, "every commit that touched it, under either name")

	assert.Equal(t, "bump again and add another file", past[0].Subject, "newest first")
	assert.Equal(t, "add the cart", past[3].Subject)
	assert.Equal(t, "t", past[0].Author)
	assert.False(t, past[0].At.IsZero())

	// The name at each commit, which is what reading the blob there needs.
	assert.Equal(t, "basket.go", past[0].Path)
	assert.Equal(t, "cart.go", past[3].Path)
}

// The commit also touched other.go, and --name-only lists both. The path kept
// is the followed file's, not whichever name git printed first.
func TestHistoryKeepsTheFollowedFilesName(t *testing.T) {
	dir := renamedRepo(t)

	past, err := History(context.Background(), dir, "other.go", 0)
	require.NoError(t, err)
	require.Len(t, past, 1)
	assert.Equal(t, "other.go", past[0].Path)
}

func TestHistoryStopsAtTheLimit(t *testing.T) {
	dir := renamedRepo(t)

	past, err := History(context.Background(), dir, "basket.go", 2)
	require.NoError(t, err)
	assert.Len(t, past, 2)
}

func TestHistoryNeedsAFile(t *testing.T) {
	dir := renamedRepo(t)

	_, err := History(context.Background(), dir, "  ", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs a file")

	// git answers an unknown path with an empty log, not a failure, and an
	// empty list is the honest answer: nothing has touched it.
	past, err := History(context.Background(), dir, "never-existed.go", 0)
	require.NoError(t, err)
	assert.Empty(t, past)
}

func TestShowAtReadsTheFileUnderTheNameItHadThen(t *testing.T) {
	dir := renamedRepo(t)
	ctx := context.Background()

	past, err := History(ctx, dir, "basket.go", 0)
	require.NoError(t, err)
	require.Len(t, past, 4)

	now, err := ShowAt(ctx, dir, past[0].SHA, past[0].Path)
	require.NoError(t, err)
	assert.Contains(t, now, "var total = 3")

	// The oldest commit predates the rename, so the current name is not there
	// and the path the row carries is the one that works.
	then, err := ShowAt(ctx, dir, past[3].SHA, past[3].Path)
	require.NoError(t, err)
	assert.Contains(t, then, "var total = 1")

	_, err = ShowAt(ctx, dir, past[3].SHA, "basket.go")
	require.Error(t, err)
}
