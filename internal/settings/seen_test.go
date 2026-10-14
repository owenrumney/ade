package settings

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeenRemembersWhenYouLooked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	at := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, MarkSeen("abc", at))

	got := Seen()
	require.Contains(t, got, "abc")
	assert.True(t, at.Equal(got["abc"]), "%s is not %s", got["abc"], at)
}

func TestAnUnnamedSessionIsNotMarked(t *testing.T) {
	// A conversation names itself on its first event; before that there is
	// nothing to key on, and an empty key would swallow every one of them.
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, MarkSeen("  ", time.Now()))
	assert.Empty(t, Seen())
}

func TestAnUnreadableTimeIsNotASeenSession(t *testing.T) {
	// The file is edited by hand often enough; a bad value should hide the
	// session from the list rather than claim it was seen in year zero.
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, seen.set("abc", "not a time"))
	assert.Empty(t, Seen())
}
