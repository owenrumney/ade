package launcher

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

func spaceList(t *testing.T, s *Server) (open []Space, offered []map[string]any) {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/spaces", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Spaces  []Space          `json:"spaces"`
		Offered []map[string]any `json:"offered"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return got.Spaces, got.Offered
}

func TestOpeningASpaceWritesItAndClosingTakesItBackOut(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	rec := do(t, s, http.MethodPost, "/api/spaces", `{"repo":"infracost/coast"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = do(t, s, http.MethodPost, "/api/spaces", `{"repo":"owenrumney/ade"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"infracost/coast", "owenrumney/ade"}, cfg.Spaces)

	rec = do(t, s, http.MethodDelete, "/api/spaces", `{"repo":"infracost/coast"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	cfg, err = settings.Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"owenrumney/ade"}, cfg.Spaces)
}

func TestASpaceHasToBeOwnerAndName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	for _, body := range []string{`{"repo":""}`, `{"repo":"coast"}`, `{"repo":"a/b/c"}`, `{}`} {
		rec := do(t, s, http.MethodPost, "/api/spaces", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
}

// A space whose clone is gone is listed with nothing under it, so it can be
// cloned back rather than silently dropped from the person's own list.
func TestASpaceThisMachineHasNoCloneOfIsStillListed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	require.NoError(t, settings.Save(settings.Settings{Spaces: []string{"nobody/nowhere"}}))

	open, _ := spaceList(t, s)
	require.Len(t, open, 1)
	assert.Equal(t, "nobody/nowhere", open[0].Repo)
	assert.False(t, open[0].Here)
	assert.Empty(t, open[0].Designs)
}

// Opening none is not asking to see less, but it is also not 107 spaces.
func TestNoSpacesOpenedListsNoneRatherThanEveryRepository(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	open, _ := spaceList(t, s)
	assert.Empty(t, open)
}

// Being in a checkout is having its repository open. A chat in ade with no
// space opened read as "no spaces", which is not what the person sees.
func TestTheCheckoutYouAreInIsASpaceWithoutOpeningOne(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s := newTestLauncher(t)

	dir := filepath.Join(home, "code", "owenrumney", "ade")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	gitRun(t, dir, "init", "--initial-branch", "main")
	gitRun(t, dir, "remote", "add", "origin", "https://github.com/owenrumney/ade.git")
	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{filepath.Join(home, "code")}}))

	open, _ := spaceList(t, s)
	require.Empty(t, open, "nothing is open before the window says where it is")

	rec := do(t, s, http.MethodGet, "/api/spaces?dir="+url.QueryEscape(dir), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Spaces []Space `json:"spaces"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Spaces, 1)
	assert.Equal(t, "owenrumney/ade", got.Spaces[0].Repo)
	assert.True(t, got.Spaces[0].Current)

	// Shown, not written: a repository you passed through is not one to
	// remember for every window from now on.
	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.Spaces)
}

// A review you take on is work you have opened, so its repository becomes a
// space: a checkout no open space holds is not listed anywhere.
func TestTakingOnAReviewOpensItsSpace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	require.NoError(t, settings.Save(settings.Settings{Spaces: []string{"owenrumney/ade"}}))
	s.openSpaceFor("infracost/infra")

	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"owenrumney/ade", "infracost/infra"}, cfg.Spaces)

	// Twice is once: the list is ordered and a repeat would move nothing.
	s.openSpaceFor("infracost/infra")
	cfg, err = settings.Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"owenrumney/ade", "infracost/infra"}, cfg.Spaces)
}

// Nobody has opened a space, so everything the scan found is listed. Adding
// one here would hide every other repository on the machine.
func TestAReviewDoesNotOpenTheFirstSpaceForYou(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	s.openSpaceFor("infracost/infra")

	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.Spaces)
}
