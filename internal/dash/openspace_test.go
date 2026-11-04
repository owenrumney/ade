package dash

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
)

// A settings file with a blank or a repeat in it is shorter once cleaned, and
// the save was skipped because the lengths matched. The review's repository
// then never became a space, so its checkout was listed nowhere.
func TestOpeningAReviewAddsItsSpaceEvenWhenTheListNeedsCleaning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Save(settings.Settings{Spaces: []string{"infracost/coast", "", "infracost/coast"}}))

	OpenSpaceFor("infracost/tokenomics")

	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"infracost/coast", "infracost/tokenomics"}, cfg.Spaces)
}

// No spaces open is every repository listed, so there is nothing to add to.
func TestOpeningAReviewLeavesAnEmptySpaceListEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	OpenSpaceFor("infracost/tokenomics")

	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.Spaces)
}

// A review you take on is work you have opened, so its repository becomes a
// space: a checkout no open space holds is not listed anywhere.
func TestTakingOnAReviewOpensItsSpace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	require.NoError(t, settings.Save(settings.Settings{Spaces: []string{"owenrumney/ade"}}))
	OpenSpaceFor("infracost/infra")

	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"owenrumney/ade", "infracost/infra"}, cfg.Spaces)

	// Twice is once: the list is ordered and a repeat would move nothing.
	OpenSpaceFor("infracost/infra")
	cfg, err = settings.Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"owenrumney/ade", "infracost/infra"}, cfg.Spaces)
}

// Nobody has opened a space, so everything the scan found is listed. Adding
// one here would hide every other repository on the machine.
func TestAReviewDoesNotOpenTheFirstSpaceForYou(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	OpenSpaceFor("infracost/infra")

	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.Spaces)
}

// Every way into a review goes through startReview, so the space is opened
// wherever the request came from. Before this only the launcher's own handler
// did it, and the session server's /api/reviews/open opened none.
func TestCheckingOutAReviewOpensItsSpaceWhicheverServerAsked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Save(settings.Settings{Spaces: []string{"owenrumney/ade"}}))

	here := t.TempDir()
	board := New(Deps{
		Repos: func() ([]sessions.Repo, error) {
			return []sessions.Repo{{Owner: "infracost", Name: "coast", Path: here}}, nil
		},
		CheckOut: func(context.Context, string, int) (string, error) { return here + "-pr7", nil },
		Open: func(context.Context, string, string) (string, error) {
			return "http://127.0.0.1:1/#s", nil
		},
	})

	_, path, err := board.OpenPR(context.Background(), ghpr.PRRef{Repo: "infracost/coast", Number: 7})
	require.NoError(t, err)
	assert.Equal(t, here+"-pr7", path)

	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"owenrumney/ade", "infracost/coast"}, cfg.Spaces)
}
