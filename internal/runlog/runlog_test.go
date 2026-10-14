package runlog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheAgentsMemorySurvivesBeingReopened(t *testing.T) {
	path := Path(t.TempDir())

	log := Open(path)
	require.NoError(t, log.SetSession("sess-abc"))

	// The design document and the comments already survived a restart. Which
	// conversation produced them is worth as much.
	assert.Equal(t, "sess-abc", Open(path).Session())
}

func TestTheFileIsPrivate(t *testing.T) {
	path := Path(t.TempDir())
	require.NoError(t, Open(path).SetSession("sess-abc"))

	// A session id resumes a conversation about unreleased work, so it is
	// nobody else's on a shared box.
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestAnOlderFileWithRunsInItStillReads(t *testing.T) {
	path := Path(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	// What the file looked like when it also held the run history.
	require.NoError(t, os.WriteFile(path, []byte(`{"session":"sess-old","runs":[{"kind":"resolve"}]}`), 0o600))

	assert.Equal(t, "sess-old", Open(path).Session())
}

func TestRubbishOnDiskIsAnEmptyMemory(t *testing.T) {
	tests := map[string]string{
		"not json": "{{{",
		"a list":   `[{"kind":"resolve"}]`,
		"empty":    "",
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			path := Path(t.TempDir())
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

			// Refusing to start because an old file will not parse would be
			// worse than forgetting it.
			log := Open(path)
			assert.Empty(t, log.Session())

			// And it is repaired by the next write rather than staying broken.
			require.NoError(t, log.SetSession("sess-new"))
			assert.Equal(t, "sess-new", Open(path).Session())
		})
	}
}

func TestClearForgetsEverything(t *testing.T) {
	path := Path(t.TempDir())
	log := Open(path)
	require.NoError(t, log.SetSession("sess-abc"))

	require.NoError(t, log.Clear())
	assert.Empty(t, log.Session())
	assert.Empty(t, Open(path).Session())
	require.NoError(t, log.Clear(), "clearing what is not there is success")
}

func TestAMemoryWithNowhereToLiveStillWorks(t *testing.T) {
	// A server with no checkout behind it, and the zero value a test reaches
	// for, both have to behave rather than panic.
	for name, log := range map[string]*Log{"no path": Open(""), "nil": nil} {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, log.Session())
			assert.Empty(t, log.Path())
			require.NoError(t, log.SetSession("sess-abc"))
			require.NoError(t, log.Clear())
			assert.Empty(t, log.Session(), "nothing was remembered")
		})
	}
}

func TestPathIsUnderTheGitDir(t *testing.T) {
	assert.Equal(t, filepath.Join("/repo/.git", "ade", "agent.json"), Path("/repo/.git"))
	assert.Empty(t, Path(""))
}

func TestTheWikiDraftOutlivesTheProcessThatAllocatedIt(t *testing.T) {
	// The draft id names a mount and authorises the MCP call. A restart that
	// allocated a new one would leave the old draft orphaned and unpublishable.
	path := filepath.Join(t.TempDir(), "run.json")
	first := Open(path)
	require.NoError(t, first.SetWikiDraft("draft-abc"))
	require.NoError(t, first.SetAgentSession("session-1", "claude"))

	again := Open(path)
	assert.Equal(t, "draft-abc", again.WikiDraft())
	assert.Equal(t, "session-1", again.Session(), "and the provider session is still its own field")

	require.NoError(t, again.Clear())
	assert.Empty(t, Open(path).WikiDraft(), "a cleared memory keeps no draft")
}
