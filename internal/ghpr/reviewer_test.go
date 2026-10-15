package ghpr

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two lookups behind a handle read, and must pass the read-only guard.
func TestTheHandleLookupsAreReads(t *testing.T) {
	for _, args := range [][]string{
		{"api", "repos/infracost/cli/commits/abc123", "--jq", ".author.login // \"\""},
		{"api", "--paginate", "repos/infracost/cli/assignees", "--jq", ".[].login"},
	} {
		assert.Truef(t, readOnly(args), "gh %v should be allowed", args)
	}
}

// Setting the reviewer writes, and pr edit is already on the write allowlist.
func TestAddingAReviewerIsAnAllowedWrite(t *testing.T) {
	assert.True(t, writable([]string{
		"pr", "edit", "https://github.com/infracost/cli/pull/1", "--add-reviewer", "liamg",
	}))
}

func TestAddReviewerNeedsAPullRequestAndAHandle(t *testing.T) {
	ctx := context.Background()
	require.Error(t, AddReviewer(ctx, ".", "", "liamg"))
	require.Error(t, AddReviewer(ctx, ".", "https://github.com/x/y/pull/1", "  "))
}

// Nothing is asked of GitHub without a repository and a commit to ask about.
func TestTheLookupsSayNothingWithNothingToAsk(t *testing.T) {
	ctx := context.Background()

	login, err := LoginOfCommit(ctx, ".", "", "abc123")
	require.NoError(t, err)
	assert.Empty(t, login)

	login, err = LoginOfCommit(ctx, ".", "infracost/cli", "")
	require.NoError(t, err)
	assert.Empty(t, login)

	logins, err := Assignable(ctx, ".", "")
	require.NoError(t, err)
	assert.Empty(t, logins)
}
