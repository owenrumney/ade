package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMutingAPullRequestSurvivesAndUnmutes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	assert.Empty(t, MutedReviews(), "nothing is muted on a first run")

	require.NoError(t, SetMutedReview("owenrumney/ade#12", true))
	require.NoError(t, SetMutedReview("acme/other#3", true))
	assert.True(t, MutedReviews()["owenrumney/ade#12"])
	assert.True(t, MutedReviews()["acme/other#3"])

	// Muting twice is not an error, and does not double up.
	require.NoError(t, SetMutedReview("owenrumney/ade#12", true))
	assert.Len(t, MutedReviews(), 2)

	require.NoError(t, SetMutedReview("owenrumney/ade#12", false))
	assert.False(t, MutedReviews()["owenrumney/ade#12"])
	assert.True(t, MutedReviews()["acme/other#3"], "unmuting one leaves the other")

	assert.Error(t, SetMutedReview("  ", true), "a key is required")
}
