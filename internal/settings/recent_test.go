package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// atHome points ade's state directory at a temp dir for the duration.
func atHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, ".ade")
}

func TestTouchPutsTheNewestFirst(t *testing.T) {
	atHome(t)
	now := time.Now()

	require.NoError(t, Touch("/code/one", "main", now.Add(-2*time.Hour)))
	require.NoError(t, Touch("/code/two", "fix-1", now.Add(-time.Hour)))
	require.NoError(t, Touch("/code/three", "main", now))

	// Directories that do not exist are skipped, so read the file directly.
	path, err := RecentPath()
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var history []Workspace
	require.NoError(t, json.Unmarshal(data, &history))
	require.Len(t, history, 3)
	assert.Equal(t, "/code/three", history[0].Path)
	assert.Equal(t, "three", history[0].Name)
	assert.Equal(t, "main", history[0].Branch)
}

func TestOpeningSomewhereAgainMovesItUp(t *testing.T) {
	atHome(t)
	now := time.Now()

	require.NoError(t, Touch("/code/one", "main", now.Add(-time.Hour)))
	require.NoError(t, Touch("/code/two", "main", now.Add(-30*time.Minute)))
	require.NoError(t, Touch("/code/one", "fix-2", now))

	path, err := RecentPath()
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var history []Workspace
	require.NoError(t, json.Unmarshal(data, &history))

	// One line per checkout: going back to it moves it up rather than adding a
	// second line saying the same thing.
	require.Len(t, history, 2)
	assert.Equal(t, "/code/one", history[0].Path)
	assert.Equal(t, "fix-2", history[0].Branch, "the branch you were last on")
}

func TestRecentsSkipWhatHasGone(t *testing.T) {
	atHome(t)
	here := t.TempDir()

	require.NoError(t, Touch(here, "main", time.Now()))
	require.NoError(t, Touch("/code/deleted-since", "main", time.Now().Add(-time.Hour)))

	// A checkout that has been removed or renamed is not somewhere to go back
	// to, and offering it would start a server nowhere.
	got := Recents(5)
	require.Len(t, got, 1)
	assert.Equal(t, here, got[0].Path)
}

func TestRecentsAreBounded(t *testing.T) {
	atHome(t)

	dirs := make([]string, 8)
	for i := range dirs {
		dirs[i] = t.TempDir()
		require.NoError(t, Touch(dirs[i], "main", time.Now().Add(time.Duration(i)*time.Minute)))
	}

	// Five is what fits in a dropdown you read at a glance.
	assert.Len(t, Recents(Recent), Recent)
	assert.Len(t, Recents(2), 2)
	assert.Len(t, Recents(0), Recent, "no limit means the default, not none")
}

func TestHistoryIsPrivateAndSurvivesRubbish(t *testing.T) {
	dir := atHome(t)
	path := filepath.Join(dir, "recent.json")

	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))

	// A broken history is no history, not a refusal to start.
	assert.Empty(t, Recents(5))

	here := t.TempDir()
	require.NoError(t, Touch(here, "main", time.Now()))
	require.Len(t, Recents(5), 1)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
