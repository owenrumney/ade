package port

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/scratch"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

// repo is a target to port into: a checkout on main with one commit.
func repo(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "service")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	gitOut(t, dir, "init", "-b", "main")
	gitOut(t, dir, "config", "user.name", "Someone")
	gitOut(t, dir, "config", "user.email", "someone@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("service\n"), 0o644))
	gitOut(t, dir, "add", "-A")
	gitOut(t, dir, "commit", "-m", "first")
	return dir
}

// working makes a scratch with a file in it, uncommitted unless asked.
func working(t *testing.T, name string, commit bool) string {
	t.Helper()

	dir, err := scratch.New(context.Background(), name)
	require.NoError(t, err)
	gitOut(t, dir, "config", "user.name", "Someone")
	gitOut(t, dir, "config", "user.email", "someone@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "limiter.go"), []byte("package limiter\n"), 0o644))
	if commit {
		gitOut(t, dir, "add", "-A")
		gitOut(t, dir, "commit", "-m", "a rate limiter")
	}
	return dir
}

func TestUncommittedScratchWorkLandsInAWorktreeOfTheRepository(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	into := repo(t)
	from := working(t, "rate limiting", false)

	moved, err := Move(context.Background(), from, into, "")
	require.NoError(t, err)

	// Most scratch work is never committed, so it is committed on the way out.
	assert.True(t, moved.Committed)
	assert.Equal(t, 1, moved.Commits)
	assert.Equal(t, "rate-limiting", moved.Branch)
	assert.Equal(t, filepath.Join(filepath.Dir(moved.Repo), "service-rate-limiting"), moved.Path)

	// The work is there, on its own branch, above the repository's own history
	// and with nothing of the scratch's base commit in it.
	assert.FileExists(t, filepath.Join(moved.Path, "limiter.go"))
	assert.FileExists(t, filepath.Join(moved.Path, "README.md"))
	assert.Equal(t, "rate-limiting", gitOut(t, moved.Path, "rev-parse", "--abbrev-ref", "HEAD"))
	assert.Equal(t, "2", gitOut(t, moved.Path, "rev-list", "--count", "HEAD"))
	assert.Equal(t, wip, gitOut(t, moved.Path, "log", "-1", "--format=%s"))

	// And the repository's own checkout is untouched by any of it.
	assert.Equal(t, "main", gitOut(t, into, "rev-parse", "--abbrev-ref", "HEAD"))
	assert.NoFileExists(t, filepath.Join(into, "limiter.go"))
}

func TestEveryScratchCommitIsCarriedInOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	into := repo(t)
	from := working(t, "three steps", true)
	for _, step := range []string{"second", "third"} {
		require.NoError(t, os.WriteFile(filepath.Join(from, step+".go"), []byte("package x\n"), 0o644))
		gitOut(t, from, "add", "-A")
		gitOut(t, from, "commit", "-m", step)
	}

	moved, err := Move(context.Background(), from, into, "")
	require.NoError(t, err)
	assert.False(t, moved.Committed, "there was nothing outstanding to commit")
	assert.Equal(t, 3, moved.Commits)

	log := gitOut(t, moved.Path, "log", "--format=%s", "main..HEAD", "--reverse")
	assert.Equal(t, "a rate limiter\nsecond\nthird", log)
}

func TestTheBranchCanBeNamed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	into := repo(t)
	from := working(t, "vague name", false)

	moved, err := Move(context.Background(), from, into, "webhook-429s")
	require.NoError(t, err)
	assert.Equal(t, "webhook-429s", moved.Branch)
	assert.Equal(t, "webhook-429s", gitOut(t, moved.Path, "rev-parse", "--abbrev-ref", "HEAD"))
}

func TestPortingRefusesAnythingThatIsNotAScratch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	into := repo(t)
	from := working(t, "somewhere", false)

	// An ordinary repository is not a scratch: porting is not a way to move
	// commits between two checkouts you happen to name.
	_, err := Move(context.Background(), repo(t), into, "")
	require.ErrorContains(t, err, "is not a scratch")

	_, err = Move(context.Background(), from, t.TempDir(), "")
	require.ErrorContains(t, err, "is not a checkout")

	_, err = Move(context.Background(), from, from, "")
	require.ErrorContains(t, err, "the scratch itself")
}

func TestAnEmptyScratchHasNothingToPort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	into := repo(t)
	from, err := scratch.New(context.Background(), "never used")
	require.NoError(t, err)

	_, err = Move(context.Background(), from, into, "")
	require.ErrorContains(t, err, "nothing in")

	// And the worktree that would have held it was never made.
	assert.NoFileExists(t, filepath.Join(filepath.Dir(into), "service-never-used"))
}

func TestAConflictLeavesTheWorktreeToFinishByHand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	into := repo(t)
	from := working(t, "clashing", false)
	require.NoError(t, os.WriteFile(filepath.Join(from, "README.md"), []byte("something else\n"), 0o644))

	moved, err := Move(context.Background(), from, into, "")
	require.ErrorContains(t, err, "did not apply cleanly")
	require.ErrorContains(t, err, "cherry-pick --continue")

	// The half-applied checkout is the thing to fix, so it has to still exist.
	assert.DirExists(t, moved.Path)
	assert.Contains(t, gitOut(t, moved.Path, "status", "--porcelain"), "README.md")
}

func TestRemoveOnlyEverTakesAScratch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	keep := repo(t)
	require.ErrorContains(t, Remove(context.Background(), keep), "is not a scratch")
	assert.DirExists(t, keep)

	from := working(t, "done with", false)
	require.NoError(t, Remove(context.Background(), from))
	assert.NoDirExists(t, from)
}
