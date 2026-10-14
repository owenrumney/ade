package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPaletteAdeDoesNotKnowIsRefused(t *testing.T) {
	for _, id := range palettes {
		got, err := CheckPalette(id)
		require.NoErrorf(t, err, "%s is in the list but refused", id)
		assert.Equal(t, id, got)
	}

	got, err := CheckPalette("")
	require.NoError(t, err)
	assert.Equal(t, DefaultPalette, got, "unset is the default, not an error")

	_, err = CheckPalette("neon")
	assert.Error(t, err)
	assert.Equal(t, DefaultPalette, PaletteID("neon"), "an unknown id falls back rather than sticking")
}

func TestTheOldGhosttyDeepStillResolves(t *testing.T) {
	// It was a palette before the deeper background became a checkbox, and it
	// is what every settings file written so far says. Rejecting it would
	// fall back to the default and silently lighten someone's page.
	assert.Equal(t, "ghostty", PaletteID("ghostty-deep"))

	got, err := CheckPalette("ghostty-deep")
	require.NoError(t, err, "a saved setting is not a typo")
	assert.Equal(t, "ghostty", got)
}

func TestWhatTheDeeperBackgroundMigratesTo(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		name    string
		palette string
		flag    *bool
		want    bool
	}{
		{"the old deep palette means deep", "ghostty-deep", nil, true},
		{"a file from before the setting means deep", "", nil, true},
		{"the plain palette does not", "ghostty", nil, false},
		{"a chosen palette does not", "ayu", nil, false},
		{"an explicit flag wins over the palette", "ghostty-deep", &off, false},
		{"and turns it on for one that would not", "ayu", &on, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DeepFor(tc.palette, tc.flag))
		})
	}
}

func TestGhosttyDeepIsNoLongerOffered(t *testing.T) {
	// Read, but not a choice: the dropdown has one Ghostty.
	assert.NotContains(t, palettes, "ghostty-deep")
}
