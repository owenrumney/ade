package editor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

func TestWhichFrontEndAdeOpens(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Nothing said: the editor, which is where ade has always opened.
	assert.Equal(t, "", Wanted())

	require.NoError(t, settings.Save(settings.Settings{Front: "window"}))
	assert.Equal(t, "window", Wanted())

	// One invocation's override, so the other front end is a prefix away.
	t.Setenv("ADE_FRONT", "editor")
	assert.Equal(t, "editor", Wanted())
}

func TestTheWindowIsNotOpenedWhenItIsNotInstalled(t *testing.T) {
	// The fallback is the editor, so this has to be an error and not a
	// silent nothing: `ade .` opening no window at all is the worst of it.
	assert.ErrorIs(t, OpenWindow(t.TempDir()), ErrNoWindow)
}
