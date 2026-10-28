package designs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// identity keeps git from refusing to commit on a machine with none set.
func identity(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "ade")
	t.Setenv("GIT_AUTHOR_EMAIL", "ade@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "ade")
	t.Setenv("GIT_COMMITTER_EMAIL", "ade@example.com")
}

// bare is a remote to push to, without a network.
func bare(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	require.NoError(t, exec.Command("git", "init", "--bare", "-b", branch, dir).Run())
	return dir
}

func TestARemoteSetAfterTheStoreWasMadeIsStillWiredUp(t *testing.T) {
	identity(t)
	root := filepath.Join(t.TempDir(), "designs")
	ctx := context.Background()

	alone := New(root, "")
	require.NoError(t, alone.Ensure(ctx))

	out, err := exec.Command("git", "-C", root, "remote").Output()
	require.NoError(t, err)
	require.Empty(t, string(out), "a store with no remote has none")

	remote := bare(t)
	require.NoError(t, New(root, remote).Ensure(ctx))

	out, err = exec.Command("git", "-C", root, "remote", "get-url", "origin").Output()
	require.NoError(t, err)
	assert.Equal(t, remote+"\n", string(out))
}

func TestAChangedRemoteIsFollowedRatherThanLeftPointingAtTheOldOne(t *testing.T) {
	identity(t)
	root := filepath.Join(t.TempDir(), "designs")
	ctx := context.Background()
	require.NoError(t, New(root, bare(t)).Ensure(ctx))

	moved := bare(t)
	require.NoError(t, New(root, moved).Ensure(ctx))

	out, err := exec.Command("git", "-C", root, "remote", "get-url", "origin").Output()
	require.NoError(t, err)
	assert.Equal(t, moved+"\n", string(out))
}

func TestTwoStoresThatBeganApartJoinRatherThanRefusing(t *testing.T) {
	identity(t)
	ctx := context.Background()
	remote := bare(t)

	one := filepath.Join(t.TempDir(), "one")
	first := New(one, remote)
	require.NoError(t, first.Ensure(ctx))
	_, err := first.Write("first", Meta{}, []byte("# first\n"))
	require.NoError(t, err)
	require.NoError(t, first.Sync(ctx))

	// A second machine inits its own store rather than cloning, which is
	// what happens when the remote was empty at the time.
	two := filepath.Join(t.TempDir(), "two")
	require.NoError(t, New(two, "").Ensure(ctx))
	second := New(two, remote)
	_, err = second.Write("second", Meta{}, []byte("# second\n"))
	require.NoError(t, err)
	require.NoError(t, second.Sync(ctx))

	for _, name := range []string{"first", "second"} {
		_, err := os.Stat(filepath.Join(two, name, File))
		assert.NoError(t, err, "%s should have survived the join", name)
	}
}
