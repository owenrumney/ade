package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionGroupsKeepTheirOrderAndRejectOverlaps(t *testing.T) {
	atHome(t)

	group, err := CreateSessionGroup("/code/widget", []string{"old", "new"})
	require.NoError(t, err)
	assert.NotEmpty(t, group.ID)
	assert.Equal(t, []string{"old", "new"}, group.Members)
	assert.Equal(t, []SessionGroup{group}, SessionGroups())

	_, err = CreateSessionGroup("/code/widget", []string{"new", "other"})
	assert.Error(t, err)
	_, err = CreateSessionGroup("/code/widget", []string{"one"})
	assert.Error(t, err)
}

func TestDeletingSessionsUpdatesTheirGroups(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	group, err := CreateSessionGroup("/code/widget", []string{"one", "two", "three"})
	require.NoError(t, err)

	require.NoError(t, DropSessionsFromGroups("one"))
	got := SessionGroups()
	require.Len(t, got, 1)
	assert.Equal(t, []string{"two", "three"}, got[0].Members)

	require.NoError(t, DropSessionsFromGroups("two"))
	assert.Empty(t, SessionGroups())
	assert.Error(t, DeleteSessionGroup(group.ID), "an already-removed group is not restored")
}

func TestSessionGroupsIgnoreRubbishAndArePrivate(t *testing.T) {
	dir := atHome(t)
	path := filepath.Join(dir, "session-groups.json")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(path, []byte(`[{"id":"bad"}]`), 0o600))
	assert.Empty(t, SessionGroups())

	_, err := CreateSessionGroup("/code/widget", []string{"one", "two"})
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestForgettingASessionRemovesItsGroupMembership(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := CreateSessionGroup("/code/widget", []string{"old", "keep"})
	require.NoError(t, err)

	_, err = Forget("old")
	require.NoError(t, err)
	assert.Empty(t, SessionGroups())
}

func TestDismissingASessionSuggestionKeepsItsFingerprint(t *testing.T) {
	atHome(t)
	require.NoError(t, SetSessionGroupSuggestionDismissed("/code/widget", "members-a"))
	assert.Equal(t, map[string]string{"/code/widget": "members-a"}, DismissedSessionGroupSuggestions())
	require.NoError(t, SetSessionGroupSuggestionDismissed("/code/widget", ""))
	assert.Empty(t, DismissedSessionGroupSuggestions())
}
