package rewind

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), identity...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

func write(t *testing.T, root, path, body string) {
	t.Helper()
	full := filepath.Join(root, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o600))
}

func read(t *testing.T, root, path string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, path))
	require.NoError(t, err)
	return string(body)
}

// repo has one commit, an ignore rule, a staged edit and an untracked file.
func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitIn(t, root, "init", "-q")
	write(t, root, ".gitignore", "build/\n")
	write(t, root, "kept.go", "kept\n")
	write(t, root, "edited.go", "before\n")
	write(t, root, "doomed.go", "doomed\n")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "first")
	write(t, root, "staged.go", "staged\n")
	gitIn(t, root, "add", "staged.go")
	write(t, root, "untracked.txt", "mine\n")
	return root
}

func TestRestorePutsBackEditsAddsAndDeletes(t *testing.T) {
	ctx, root := context.Background(), repo(t)
	name, err := Save(ctx, root)
	require.NoError(t, err)

	write(t, root, "edited.go", "after\n")
	write(t, root, "new/deep/made.go", "made\n")
	write(t, root, "untracked.txt", "changed\n")
	require.NoError(t, os.Remove(filepath.Join(root, "doomed.go")))

	plan, err := Plan(ctx, root, name)
	require.NoError(t, err)
	assert.ElementsMatch(t, []Change{
		{Path: "edited.go", Do: "restore"},
		{Path: "untracked.txt", Do: "restore"},
		{Path: "new/deep/made.go", Do: "delete"},
		{Path: "doomed.go", Do: "recreate"},
	}, plan)
	assert.Equal(t, "after\n", read(t, root, "edited.go"), "a plan changes nothing")

	changed, _, err := Restore(ctx, root, name)
	require.NoError(t, err)
	assert.Len(t, changed, 4)
	assert.Equal(t, "before\n", read(t, root, "edited.go"))
	assert.Equal(t, "mine\n", read(t, root, "untracked.txt"))
	assert.Equal(t, "doomed\n", read(t, root, "doomed.go"))
	assert.NoDirExists(t, filepath.Join(root, "new"))
	assert.Equal(t, "kept\n", read(t, root, "kept.go"))
}

func TestTheIndexAndHeadAreLeftAlone(t *testing.T) {
	ctx, root := context.Background(), repo(t)
	status := gitIn(t, root, "status", "--porcelain")
	head := gitIn(t, root, "rev-parse", "HEAD")

	name, err := Save(ctx, root)
	require.NoError(t, err)
	assert.Equal(t, status, gitIn(t, root, "status", "--porcelain"))

	write(t, root, "edited.go", "after\n")
	_, _, err = Restore(ctx, root, name)
	require.NoError(t, err)
	assert.Equal(t, status, gitIn(t, root, "status", "--porcelain"))
	assert.Equal(t, head, gitIn(t, root, "rev-parse", "HEAD"))
}

func TestAnIgnoredFileIsNeitherSavedNorRestored(t *testing.T) {
	ctx, root := context.Background(), repo(t)
	write(t, root, "build/out.bin", "old\n")
	name, err := Save(ctx, root)
	require.NoError(t, err)

	write(t, root, "build/out.bin", "new\n")
	plan, err := Plan(ctx, root, name)
	require.NoError(t, err)
	assert.Empty(t, plan)
	assert.Equal(t, "new\n", read(t, root, "build/out.bin"))
}

func TestARestoreCanItselfBeUndone(t *testing.T) {
	ctx, root := context.Background(), repo(t)
	name, err := Save(ctx, root)
	require.NoError(t, err)
	write(t, root, "edited.go", "after\n")

	_, before, err := Restore(ctx, root, name)
	require.NoError(t, err)
	require.Equal(t, "before\n", read(t, root, "edited.go"))

	_, _, err = Restore(ctx, root, before)
	require.NoError(t, err)
	assert.Equal(t, "after\n", read(t, root, "edited.go"))
}

func TestACommitSinceTheSnapshotRefusesTheRestore(t *testing.T) {
	ctx, root := context.Background(), repo(t)
	name, err := Save(ctx, root)
	require.NoError(t, err)

	write(t, root, "edited.go", "after\n")
	gitIn(t, root, "commit", "-q", "-am", "second")

	_, err = Plan(ctx, root, name)
	require.ErrorIs(t, err, ErrMoved)
	_, _, err = Restore(ctx, root, name)
	require.ErrorIs(t, err, ErrMoved)
	assert.Equal(t, "after\n", read(t, root, "edited.go"))
}

func TestASnapshotThatIsNotThereIsGone(t *testing.T) {
	ctx, root := context.Background(), repo(t)
	for _, name := range []string{"", "deadbeefdeadbeef", "../heads/main", "HEAD"} {
		_, err := Plan(ctx, root, name)
		require.ErrorIs(t, err, ErrGone, name)
	}
}

func TestOnlyTheNewestSnapshotsAreKept(t *testing.T) {
	ctx, root := context.Background(), repo(t)
	for range Keep + 3 {
		_, err := Save(ctx, root)
		require.NoError(t, err)
	}
	out := gitIn(t, root, "for-each-ref", "--format=%(refname)", refs)
	assert.Len(t, strings.Fields(out), Keep)
}

func TestARepositoryWithNoCommitsCanBeSnapshotted(t *testing.T) {
	ctx, root := context.Background(), t.TempDir()
	gitIn(t, root, "init", "-q")
	write(t, root, "a.txt", "one\n")
	name, err := Save(ctx, root)
	require.NoError(t, err)

	write(t, root, "a.txt", "two\n")
	_, _, err = Restore(ctx, root, name)
	require.NoError(t, err)
	assert.Equal(t, "one\n", read(t, root, "a.txt"))
}

// Same size, same second as the index: git's stat cache alone calls it clean.
func TestAnEditGitsStatCacheWouldMissIsStillSeen(t *testing.T) {
	ctx, root := context.Background(), repo(t)
	for range 20 {
		write(t, root, "edited.go", "before\n")
		gitIn(t, root, "add", "edited.go")
		name, err := Save(ctx, root)
		require.NoError(t, err)

		write(t, root, "edited.go", "after!\n")
		plan, err := Plan(ctx, root, name)
		require.NoError(t, err)
		require.Equal(t, []Change{{Path: "edited.go", Do: "restore"}}, plan)
	}
}
