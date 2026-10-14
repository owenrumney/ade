package statedir

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestADirectoryLeftByTheOldNameIsCarriedOver(t *testing.T) {
	git := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(git, "sow"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(git, "sow", "chat.db"), []byte("sessions"), 0o600))

	dir := Git(git)

	assert.Equal(t, filepath.Join(git, "ade"), dir)
	body, err := os.ReadFile(filepath.Join(dir, "chat.db"))
	require.NoError(t, err, "the sessions came with it")
	assert.Equal(t, "sessions", string(body))
	assert.NoDirExists(t, filepath.Join(git, "sow"))
}

func TestAnExistingDirectoryIsNotOverwrittenByTheOldOne(t *testing.T) {
	git := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(git, "sow"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(git, "sow", "chat.db"), []byte("old"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(git, "ade"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(git, "ade", "chat.db"), []byte("live"), 0o600))

	body, err := os.ReadFile(filepath.Join(Git(git), "chat.db"))
	require.NoError(t, err)
	assert.Equal(t, "live", string(body), "the live directory wins")
	assert.DirExists(t, filepath.Join(git, "sow"), "and the old one is left to look at")
}

func TestACheckoutWithNeitherDirectoryJustGetsTheName(t *testing.T) {
	git := t.TempDir()

	assert.Equal(t, filepath.Join(git, "ade"), Git(git))
	assert.NoDirExists(t, filepath.Join(git, "ade"), "naming it does not create it")
}

func TestHomeCarriesTheDottedNameOver(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".sow", "projects"), 0o750))

	dir := Home(home)

	assert.Equal(t, filepath.Join(home, ".ade"), dir)
	assert.DirExists(t, filepath.Join(dir, "projects"))
}

func TestAFileNamedLikeTheOldDirectoryIsLeftAlone(t *testing.T) {
	git := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(git, "sow"), []byte("not a directory"), 0o600))

	assert.Equal(t, filepath.Join(git, "ade"), Git(git))
	assert.FileExists(t, filepath.Join(git, "sow"))
}

func TestAnEmptyDirectoryDoesNotStrandTheOldOne(t *testing.T) {
	git := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(git, "sow"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(git, "sow", "chat.db"), []byte("sessions"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(git, "ade"), 0o750))

	body, err := os.ReadFile(filepath.Join(Git(git), "chat.db"))
	require.NoError(t, err, "an empty directory is not state to protect")
	assert.Equal(t, "sessions", string(body))
	assert.NoDirExists(t, filepath.Join(git, "sow"))
}

func TestTheMachineDirectorySitsUnderTheStateDirectory(t *testing.T) {
	home := t.TempDir()

	assert.Equal(t, filepath.Join(home, ".ade", "machine"), MachineDir(home))
}
