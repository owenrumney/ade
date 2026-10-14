package ghpr

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLiveAgainstGitHub exercises the gh path for real. It is opt-in because it
// needs network and a working gh login.
func TestLiveAgainstGitHub(t *testing.T) {
	dir := os.Getenv("ADE_LIVE_REPO")
	if dir == "" {
		t.Skip("set ADE_LIVE_REPO to a repo with a merged PR #7 to run this")
	}
	ctx := context.Background()

	t.Run("checks", func(t *testing.T) {
		checks, err := Checks(ctx, dir, 7)
		require.NoError(t, err)
		require.NotEmpty(t, checks)
		for _, check := range checks {
			assert.NotEmpty(t, check.Name)
			assert.NotEmpty(t, check.Bucket)
		}
	})

	t.Run("a branch with no pull request is absence, not an error", func(t *testing.T) {
		pr, err := ForBranch(ctx, dir, "no-such-branch-here")
		require.NoError(t, err)
		assert.Nil(t, pr)
	})
}
