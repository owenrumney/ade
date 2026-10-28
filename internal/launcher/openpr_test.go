package launcher

import (
	"net/http"
	"testing"

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

// A settings file with a blank or a repeat in it is shorter once cleaned, and
// the save was skipped because the lengths matched. The review's repository
// then never became a space, so its checkout was listed nowhere.
func TestOpeningAReviewAddsItsSpaceEvenWhenTheListNeedsCleaning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	require.NoError(t, settings.Save(settings.Settings{Spaces: []string{"infracost/coast", "", "infracost/coast"}}))

	s.openSpaceFor("infracost/tokenomics")

	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"infracost/coast", "infracost/tokenomics"}, cfg.Spaces)
}

// No spaces open is every repository listed, so there is nothing to add to.
func TestOpeningAReviewLeavesAnEmptySpaceListEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	s.openSpaceFor("infracost/tokenomics")

	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.Spaces)
}
