package dash

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/sessions"
)

func TestStartingATicketRunsCreateInTheChosenRepository(t *testing.T) {
	var ranIn, ranFor string
	repos := []sessions.Repo{{Path: "/srv/lsp", Name: "lsp", Owner: "owenrumney"}}
	b := New(Deps{
		Repos: func() ([]sessions.Repo, error) { return repos, nil },
		Create: func(_ context.Context, dir, ticket string) (string, error) {
			ranIn, ranFor = dir, ticket
			return "http://127.0.0.1:1/?t=x", nil
		},
	})

	url, path, err := b.StartTicket(context.Background(), "/srv/lsp", "fix-623")
	require.NoError(t, err)
	assert.Equal(t, "/srv/lsp", ranIn)
	assert.Equal(t, "FIX-623", ranFor, "as Linear writes it")
	assert.Equal(t, "http://127.0.0.1:1/?t=x", url)
	assert.Equal(t, gitdiff.WorktreePath("/srv/lsp", "fix-623"), path)

	// Only a repository the scan found, and only a ticket-shaped ticket:
	// both go onto a command line.
	_, _, err = b.StartTicket(context.Background(), "/etc", "FIX-1")
	assert.ErrorContains(t, err, "not one of the repositories")
	for _, bad := range []string{"", "FIX", "FIX-", "-1", "FIX-1; rm -rf /", "../FIX-1", "FIX-1 --foo"} {
		_, _, err = b.StartTicket(context.Background(), "/srv/lsp", bad)
		assert.Error(t, err, bad)
	}
}
