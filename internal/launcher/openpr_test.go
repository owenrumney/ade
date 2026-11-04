package launcher

import (
	"context"
	"net/http"
	"testing"

	"github.com/owenrumney/ade/internal/dash"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Read before anything is checked out: a link that is not a pull request
// should not reach a clone, and the message should say what one looks like.
func TestAPastedLinkIsReadBeforeAnythingIsCheckedOut(t *testing.T) {
	s := newTestLauncher(t)

	for _, bad := range []string{
		`{"link":"https://github.com/infracost/cli"}`,
		`{"link":"https://github.com/infracost/cli/issues/12"}`,
		`{"link":"not a link"}`,
	} {
		rec := do(t, s, http.MethodPost, "/api/reviews/open", bad)
		require.Equalf(t, http.StatusBadRequest, rec.Code, "took %s", bad)
		assert.Contains(t, rec.Body.String(), "pull request link")
	}

	// No link at all is the old request, which names the two fields and
	// answers from the review list.
	rec := do(t, s, http.MethodPost, "/api/reviews/open", `{"link":"   ","repo":"a/b","number":1}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// A pull request opened from a link lands in a worktree of a repository this
// machine already has. If that repository is not an open space, the checkout
// is listed nowhere, so opening it is opening its space.
func TestOpeningAPastedLinkOpensTheSpaceForARepositoryYouHave(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	require.NoError(t, settings.Save(settings.Settings{Spaces: []string{"owenrumney/ade"}}))

	here := t.TempDir()
	s.board = dash.New(dash.Deps{
		Repos: func() ([]sessions.Repo, error) {
			return []sessions.Repo{{Owner: "infracost", Name: "coast", Path: here}}, nil
		},
		CheckOut: func(context.Context, string, int) (string, error) { return here + "-pr7", nil },
		Open: func(context.Context, string, string) (string, error) {
			return "http://127.0.0.1:1/#s", nil
		},
	})

	rec := do(t, s, http.MethodPost, "/api/reviews/open",
		`{"link":"https://github.com/infracost/coast/pull/7"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"owenrumney/ade", "infracost/coast"}, cfg.Spaces)
}
