package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTitlesRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	assert.Empty(t, Titles())

	require.NoError(t, SetTitle("abc", "  Rate limit the webhook  "))
	require.NoError(t, SetTitle("def", "Graph of the machine"))
	assert.Equal(t, map[string]string{"abc": "Rate limit the webhook", "def": "Graph of the machine"}, Titles())

	// Forgotten with an empty title; nothing named is an error.
	require.NoError(t, SetTitle("abc", ""))
	assert.Equal(t, map[string]string{"def": "Graph of the machine"}, Titles())
	assert.Error(t, SetTitle("", "x"))
}
