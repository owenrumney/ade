package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArchivingASessionSurvivesAReread(t *testing.T) {
	atHome(t)

	require.NoError(t, SetArchived("sess-1", true))
	require.NoError(t, SetArchived("sess-2", true))
	assert.Equal(t, map[string]bool{"sess-1": true, "sess-2": true}, Archived())

	// Bringing one back takes it out of the file rather than marking it false.
	require.NoError(t, SetArchived("sess-1", false))
	assert.Equal(t, map[string]bool{"sess-2": true}, Archived())

	// Saying the same thing twice is not an error, and not a write.
	require.NoError(t, SetArchived("sess-2", true))
	require.NoError(t, SetArchived("sess-9", false))
	assert.Equal(t, map[string]bool{"sess-2": true}, Archived())
}

func TestArchivingNeedsAnId(t *testing.T) {
	atHome(t)
	assert.Error(t, SetArchived("", true))
}

func TestTheArchiveIsPrivateAndSurvivesRubbish(t *testing.T) {
	dir := atHome(t)
	path := filepath.Join(dir, "archived.json")

	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))

	// A broken file is an empty archive, not a refusal to start.
	assert.Empty(t, Archived())

	require.NoError(t, SetArchived("sess-1", true))
	assert.True(t, Archived()["sess-1"])

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestHiddenSessionsAreKeptApartFromArchivedOnes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	require.NoError(t, Hide("one-shot"))
	require.NoError(t, Hide("one-shot"), "again is fine")
	require.NoError(t, Hide("another"))
	assert.Error(t, Hide(""))

	assert.Equal(t, map[string]bool{"one-shot": true, "another": true}, Hidden())
	assert.Empty(t, Archived(), "hiding is not archiving")
}

func TestForgettingASessionTakesItOutOfEveryList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	require.NoError(t, SetArchived("old", true))
	require.NoError(t, SetTitle("old", "the one about carts"))
	require.NoError(t, SetArchived("keep", true))

	gone, err := Forget("old")
	require.NoError(t, err)
	assert.Equal(t, 1, gone)

	assert.True(t, Hidden()["old"], "a forgotten session is never listed again")
	assert.False(t, Archived()["old"], "and is not left in the archive nothing reads")
	assert.True(t, Archived()["keep"], "the rest of the archive is untouched")
	assert.NotContains(t, Titles(), "old", "its name goes with it")
}

func TestForgettingIsIdempotentAndBulk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	gone, err := Forget("a", "b", "", "  ")
	require.NoError(t, err)
	assert.Equal(t, 2, gone, "blank ids are not sessions")

	gone, err = Forget("b", "c")
	require.NoError(t, err)
	assert.Equal(t, 1, gone, "only what was not already forgotten counts")

	assert.Equal(t, map[string]bool{"a": true, "b": true, "c": true}, Hidden())

	_, err = Forget()
	assert.Error(t, err, "asking to forget nothing is a mistake worth saying")
}

// A name you typed is final; the flag is how ade knows the difference.
func TestANameYouTypedIsYours(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	require.NoError(t, SetTitle("s1", "what ade called it"))
	assert.False(t, NamedByYou("s1"))

	require.NoError(t, SetTitleByYou("s1", "what I call it"))
	assert.True(t, NamedByYou("s1"))
	assert.Equal(t, "what I call it", Titles()["s1"])

	// Clearing it hands the session back to ade.
	require.NoError(t, SetTitleByYou("s1", ""))
	assert.False(t, NamedByYou("s1"))
}
