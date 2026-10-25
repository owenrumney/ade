package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheStashLedgerIsPerRepositoryAndBranch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	assert.Empty(t, Stashed("/code/ade/.git", "feat/x"), "nothing is held on a first run")

	require.NoError(t, KeepStash("/code/ade/.git", "feat/x", "abc123"))
	require.NoError(t, KeepStash("/code/ade/.git", "feat/y", "def456"))
	require.NoError(t, KeepStash("/code/other/.git", "feat/x", "999aaa"))

	assert.Equal(t, "abc123", Stashed("/code/ade/.git", "feat/x"))
	assert.Equal(t, "999aaa", Stashed("/code/other/.git", "feat/x"),
		"the same branch name in another repository is another entry")
	assert.Equal(t,
		map[string]string{"feat/x": "abc123", "feat/y": "def456"},
		StashedIn("/code/ade/.git"))

	require.NoError(t, ForgetStash("/code/ade/.git", "feat/x"))
	assert.Empty(t, Stashed("/code/ade/.git", "feat/x"))
	assert.Equal(t, map[string]string{"feat/y": "def456"}, StashedIn("/code/ade/.git"))
}

// A branch with a slash in it is the usual case, and the key is one string.
func TestAStashKeyKeepsBranchesWithSlashesApart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	require.NoError(t, KeepStash("/code/ade/.git", "feat/a/b", "one"))
	require.NoError(t, KeepStash("/code/ade/.git", "feat/a", "two"))

	assert.Equal(t, "one", Stashed("/code/ade/.git", "feat/a/b"))
	assert.Equal(t, "two", Stashed("/code/ade/.git", "feat/a"))
	assert.Len(t, StashedIn("/code/ade/.git"), 2)
}
