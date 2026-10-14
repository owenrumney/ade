package fdlimit

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRaisingNeverLowersTheLimit(t *testing.T) {
	from, to, err := Raise()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, to, from, "it lifts or leaves it, never lowers")
	assert.Positive(t, to)
}

func TestRaisingTwiceIsNotAnError(t *testing.T) {
	_, first, err := Raise()
	require.NoError(t, err)
	_, second, err := Raise()
	require.NoError(t, err)
	assert.Equal(t, first, second, "the second is a no-op at the same ceiling")
}

func TestNowCountsOpenDescriptors(t *testing.T) {
	before := Now()
	require.Positive(t, before.Open, "a running process has descriptors open")
	require.Positive(t, before.Limit)

	held := make([]*os.File, 0, 8)
	for range 8 {
		f, err := os.Open(os.DevNull)
		require.NoError(t, err)
		held = append(held, f)
	}
	t.Cleanup(func() {
		for _, f := range held {
			_ = f.Close()
		}
	})

	assert.Greater(t, Now().Open, before.Open, "opening files moves the count")
}

func TestNearIsOnlyTrueCloseToTheCeiling(t *testing.T) {
	assert.False(t, State{Open: 40, Limit: 256}.Near())
	assert.False(t, State{Open: 204, Limit: 256}.Near(), "exactly 80% is not yet near")
	assert.True(t, State{Open: 250, Limit: 256}.Near())
	assert.False(t, State{Open: 9, Limit: 0}.Near(), "no limit read, no claim")
}
