package diskuse

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, path string, n int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, make([]byte, n), 0o600))
}

func TestMeasureNamesWhatDominatesADirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "chat.db"), 4000)
	write(t, filepath.Join(dir, "docs", "main.md"), 300)
	write(t, filepath.Join(dir, "docs", "ayu.png"), 200)
	write(t, filepath.Join(dir, "server.log"), 100)
	write(t, filepath.Join(dir, "server.json"), 10)

	got := Measure("here", dir)
	assert.Equal(t, int64(4610), got.Bytes)
	require.Len(t, got.Parts, 3, "the smallest is not named")
	assert.Equal(t, "chat.db", got.Parts[0].Name)
	assert.Equal(t, int64(4000), got.Parts[0].Bytes)
	// A subtree counts as its total, so a left-behind worktree shows as one.
	assert.Equal(t, "docs", got.Parts[1].Name)
	assert.Equal(t, int64(500), got.Parts[1].Bytes)
	assert.True(t, got.Parts[1].Dir)
}

func TestAPlaceThatIsNotThereMeasuresZero(t *testing.T) {
	// Not every checkout has ever been served, and that is not an error.
	got := Measure("gone", filepath.Join(t.TempDir(), "nope"))
	assert.Zero(t, got.Bytes)
	assert.Empty(t, got.Parts)
}

func TestMeasureAllDropsTheEmptyAndSortsTheRest(t *testing.T) {
	big, small, empty := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, filepath.Join(big, "chat.db"), 900)
	write(t, filepath.Join(small, "chat.db"), 10)

	got := MeasureAll([]Store{
		{Label: "small", Path: small},
		{Label: "empty", Path: empty},
		{Label: "big", Path: big},
	})
	require.Len(t, got, 2, "a place holding nothing is not a row")
	assert.Equal(t, "big", got[0].Label)
	assert.Equal(t, "small", got[1].Label)
	assert.Equal(t, int64(910), Total(got))
}

func TestAdeDirFollowsAWorktreeToItsRealGitDir(t *testing.T) {
	// A worktree's .git is a file naming the directory the state really
	// lives in, so a plain join would measure somewhere that does not exist.
	root := t.TempDir()
	main := filepath.Join(root, "repo")
	real := filepath.Join(main, ".git", "worktrees", "feature")
	require.NoError(t, os.MkdirAll(real, 0o750))
	assert.Equal(t, filepath.Join(main, ".git", "ade"), AdeDir(main))

	tree := filepath.Join(root, "feature")
	require.NoError(t, os.MkdirAll(tree, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(tree, ".git"), []byte("gitdir: "+real+"\n"), 0o600))
	assert.Equal(t, filepath.Join(real, "ade"), AdeDir(tree))
}

func TestSomethingThatIsNotACheckoutHasNoAdeDir(t *testing.T) {
	assert.Empty(t, AdeDir(t.TempDir()))
}
