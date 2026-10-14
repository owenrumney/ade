package safepath

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestANameStaysInsideItsDirectory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a", "b"), 0o750))

	for _, name := range []string{"one.md", "a/b/deep.md", "./one.md", "a/../one.md"} {
		path, err := Inside(root, name)
		require.NoError(t, err, name)
		assert.True(t, under(root, path), name)
	}
	for _, name := range []string{"", ".", "..", "../out.md", "/etc/passwd", "a/../../out.md"} {
		_, err := Inside(root, name)
		assert.Error(t, err, name)
	}
}

// The roots here are written by an agent, which can plant a link in one.
func TestALinkOutOfTheDirectoryIsRefused(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "id_ed25519")
	require.NoError(t, os.WriteFile(secret, []byte("key"), 0o600))
	require.NoError(t, os.Symlink(secret, filepath.Join(root, "notes.md")))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "evil")))

	_, err := Inside(root, "notes.md")
	assert.Error(t, err, "a link as the file itself")

	// The one Lstat on the last part alone misses: the directory is the link.
	_, err = Inside(root, "evil/id_ed25519")
	assert.Error(t, err, "a link as a directory along the way")

	// And the file need not exist yet: writing through the link is the same
	// escape as reading through it.
	_, err = Inside(root, "evil/new.md")
	assert.Error(t, err, "a file about to be created behind the link")
}

// A link that stays inside is not an escape, and the docs root is reached
// through .scratch, which is itself a link.
func TestALinkThatStaysInsideIsAllowed(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "docs")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "real"), 0o750))
	require.NoError(t, os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")))
	require.NoError(t, os.WriteFile(filepath.Join(root, "real", "one.md"), []byte("# one\n"), 0o600))

	_, err := Inside(root, "link/one.md")
	assert.NoError(t, err)

	// The root reached through a link of its own resolves to the same place.
	viaLink := filepath.Join(base, "scratch")
	require.NoError(t, os.Symlink(root, viaLink))
	_, err = Inside(viaLink, "real/one.md")
	assert.NoError(t, err)
}

func TestAMissingDirectoryIsNotAnEscape(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-there-yet")
	path, err := Inside(root, "one.md")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "one.md"), path)
}
