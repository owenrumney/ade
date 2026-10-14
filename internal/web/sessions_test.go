package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

func TestCheckoutsAreListedWithTheirSessions(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "GET", "/api/repos", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Repos []struct {
			Path      string `json:"path"`
			Name      string `json:"name"`
			Here      bool   `json:"here"`
			Sessions  int    `json:"sessions"`
			Checkouts []struct {
				Path     string `json:"path"`
				Name     string `json:"name"`
				Main     string `json:"main"`
				Worktree bool   `json:"worktree"`
				Sessions []struct {
					Dir      string `json:"dir"`
					Archived bool   `json:"archived"`
				} `json:"sessions"`
			} `json:"checkouts"`
		} `json:"repos"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	for _, repo := range body.Repos {
		assert.NotEmpty(t, repo.Name, "a repository needs something to click on")
		assert.True(t, filepath.IsAbs(repo.Path), repo.Path)
		require.NotEmpty(t, repo.Checkouts, "%s has no checkouts", repo.Path)

		total, main := 0, 0
		for _, dir := range repo.Checkouts {
			// A worktree belongs one level down from the repository it is of,
			// which is the whole point of this list's shape.
			assert.Equal(t, repo.Path, dir.Main, dir.Path)
			assert.Equal(t, dir.Path != repo.Path, dir.Worktree, dir.Path)
			if !dir.Worktree {
				main++
			}
			// The sessions under a checkout have to be that checkout's, since
			// that is the only thing that says they may be opened there.
			for _, found := range dir.Sessions {
				assert.Equal(t, dir.Path, found.Dir)
				// The count is of what is going on, and an archived session is
				// listed but not counted.
				if !found.Archived {
					total++
				}
			}
		}
		assert.Equal(t, 1, main, "%s should have exactly one main checkout", repo.Path)
		assert.Equal(t, total, repo.Sessions, "%s counted its sessions wrong", repo.Path)

		// The test server's root is a scratch repository outside the scan
		// roots, so nothing on this list is where it already is.
		assert.False(t, repo.Here)
	}
}

func TestOpeningIsLimitedToCheckoutsThatWereFound(t *testing.T) {
	s := newTestServer(t)

	// The request names a directory to start a process in. Anything but a
	// directory the scan actually found has to be refused.
	for _, body := range []string{
		`{"dir":"/etc","id":"x"}`,
		`{"dir":"/","id":""}`,
		`{"dir":"` + t.TempDir() + `"}`,
		`{}`,
	} {
		rec := do(t, s, "POST", "/api/sessions/open", body)
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "accepted %s", body)
		assert.Contains(t, rec.Body.String(), "not one of the checkouts")
	}
}

func TestDeletingSessionsTakesThemOutOfTheLists(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.SetArchived("old-two", true))

	rec := do(t, s, "POST", "/api/sessions/delete", `{"ids":["old-one","old-two"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var res struct{ Removed int }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Equal(t, 2, res.Removed)

	hidden := settings.Hidden()
	assert.True(t, hidden["old-one"])
	assert.True(t, hidden["old-two"])
	assert.Empty(t, settings.Archived(), "a deleted session is not left in the archive")
}

func TestDeletingNoSessionsIsAMistake(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	assert.Equal(t, http.StatusBadRequest, do(t, s, "POST", "/api/sessions/delete", `{"ids":[]}`).Code)
}

func TestSessionGroupActionsRejectUnknownGroupsAndSuggestions(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())

	assert.Equal(t, http.StatusBadRequest,
		do(t, s, "POST", "/api/sessions/uncombine", `{"id":"nope"}`).Code)
	assert.Equal(t, http.StatusBadRequest,
		do(t, s, "POST", "/api/sessions/suggestion/dismiss", `{"dir":"/nope","fingerprint":"old"}`).Code)
}

func TestExitCanDeleteTheSessionOnTheWayOut(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.session = "this-one"
	s.agent.current = "this-one"

	rec := do(t, s, "POST", "/api/exit", `{"session":"delete"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, settings.Hidden()["this-one"])
}

func TestExitCanArchiveTheSessionInstead(t *testing.T) {
	// Leaving is not the same as being done with it. Archiving is the middle
	// answer, and the only one that can be undone from the launcher.
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.session = "this-one"
	s.agent.current = "this-one"

	rec := do(t, s, "POST", "/api/exit", `{"session":"archive"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, settings.Archived()["this-one"])
	assert.Empty(t, settings.Hidden(), "archived, not deleted")
}

func TestExitWithoutASessionRefusesToPutOneAway(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.session = ""
	s.agent.current = ""

	for _, what := range []string{"archive", "delete"} {
		rec := do(t, s, "POST", "/api/exit", `{"session":"`+what+`"}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code, what)
	}
	assert.Empty(t, settings.Hidden())
	assert.Empty(t, settings.Archived())
}

func TestExitLeavesTheSessionAloneUnlessAsked(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.session = "this-one"
	s.agent.current = "this-one"

	// Empty is keep, so an older page that sends nothing does no harm.
	for _, body := range []string{`{"session":"keep"}`, `{}`} {
		rec := do(t, s, "POST", "/api/exit", body)
		require.Equal(t, http.StatusOK, rec.Code, body)
	}
	assert.Empty(t, settings.Hidden(), "leaving is not deleting")
	assert.Empty(t, settings.Archived())
}

func TestExitRefusesADisposalItDoesNotKnow(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.session = "this-one"
	s.agent.current = "this-one"

	rec := do(t, s, "POST", "/api/exit", `{"session":"burn"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, settings.Hidden())
	assert.Empty(t, settings.Archived())
}
