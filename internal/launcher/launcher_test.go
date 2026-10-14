package launcher

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/ui"
)

// pages stands in for the built frontend: the launcher page and one asset.
func newTestLauncher(t *testing.T) *Server {
	t.Helper()
	s, err := New(0, "test")
	require.NoError(t, err)
	return s
}

func do(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if !strings.Contains(path, "t=") {
		req.Header.Set("X-Ade-Token", s.token)
	}
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	return rec
}

func TestTheTokenIsRequired(t *testing.T) {
	s := newTestLauncher(t)

	// The launcher starts processes, so an unauthenticated page on this machine
	// must not be able to drive it.
	for _, route := range [][2]string{
		{http.MethodGet, "/api/repos"},
		{http.MethodPost, "/api/open"},
		{http.MethodPost, "/api/archive"},
		{http.MethodPost, "/api/sessions/delete"},
		{http.MethodPost, "/api/sessions/combine"},
		{http.MethodPost, "/api/sessions/uncombine"},
		{http.MethodPost, "/api/sessions/suggestion/dismiss"},
		{http.MethodGet, "/api/worktrees"},
		{http.MethodPost, "/api/prune"},
		{http.MethodPost, "/api/scratch"},
		{http.MethodGet, "/api/stats"},
		{http.MethodPost, "/api/provision"},
	} {
		req := httptest.NewRequest(route[0], route[1], strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, req)
		assert.Equalf(t, http.StatusForbidden, rec.Code, "%s was open", route[1])
	}

	req := httptest.NewRequest(http.MethodGet, "/api/repos?t=wrong", nil)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

type listedDir struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Main     string `json:"main"`
	Worktree bool   `json:"worktree"`
	Sessions []struct {
		ID       string `json:"id"`
		Dir      string `json:"dir"`
		Here     bool   `json:"here"`
		Archived bool   `json:"archived"`
	} `json:"sessions"`
}

type listedRepo struct {
	Path      string      `json:"path"`
	Name      string      `json:"name"`
	Here      bool        `json:"here"`
	Sessions  int         `json:"sessions"`
	Checkouts []listedDir `json:"checkouts"`
}

func listRepos(t *testing.T, s *Server) []listedRepo {
	t.Helper()
	rec := do(t, s, "GET", "/api/repos", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Repos []listedRepo `json:"repos"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Repos
}

// checkouts flattens the repositories, which is what the open handler will
// accept and so what the tests reason about.
func checkoutsOf(repos []listedRepo) []listedDir {
	var out []listedDir
	for _, repo := range repos {
		out = append(out, repo.Checkouts...)
	}
	return out
}

func TestRepositoriesCarryTheirWorktrees(t *testing.T) {
	for _, repo := range listRepos(t, newTestLauncher(t)) {
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
			for _, found := range dir.Sessions {
				// The sessions under a checkout have to be that checkout's,
				// since that is the only thing that says they may be opened
				// there.
				assert.Equal(t, dir.Path, found.Dir)
				// The launcher belongs to no checkout, so nothing is where you
				// already are and every row has to be clickable.
				assert.False(t, found.Here)
				// The repository's count is of what is going on, so what
				// you put away is listed under its checkout but not counted.
				if !found.Archived {
					total++
				}
			}
		}
		assert.Equal(t, 1, main, "%s should have exactly one main checkout", repo.Path)
		assert.Equal(t, total, repo.Sessions, "%s counted its sessions wrong", repo.Path)
		assert.False(t, repo.Here)
	}
}

func TestAScratchIsMadeAndOpened(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestLauncher(t)
	var opened string
	s.open = func(_ context.Context, dir, agent, _, id string) (string, error) {
		opened = dir
		assert.Empty(t, agent, "a scratch has no agent to resume")
		assert.Empty(t, id, "a scratch has no session to resume")
		return "http://127.0.0.1:1234/", nil
	}

	rec := do(t, s, "POST", "/api/scratch", `{"name":"no home for this yet"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got struct{ URL, Path string }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "http://127.0.0.1:1234/", got.URL)
	assert.Equal(t, got.Path, opened, "the ade is started in the checkout that was made")
	assert.Equal(t, "no-home-for-this-yet", filepath.Base(got.Path))
	assert.DirExists(t, filepath.Join(got.Path, ".git"))
}

// The picker sits above the scratch button, so what it says applies to the
// checkout that button makes.
func TestAScratchStartsOnThePickedAgentAndModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestLauncher(t)
	var gotAgent, gotModel string
	s.open = func(_ context.Context, _, agent, model, _ string) (string, error) {
		gotAgent, gotModel = agent, model
		return "http://127.0.0.1:1234/", nil
	}

	rec := do(t, s, "POST", "/api/scratch", `{"name":"picked","agent":"codex","model":"gpt-5.6-terra"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "codex", gotAgent)
	assert.Equal(t, "gpt-5.6-terra", gotModel)

	bad := do(t, s, "POST", "/api/scratch", `{"name":"picked","agent":"claude","model":"gpt-5.6-terra"}`)
	assert.Equal(t, http.StatusBadRequest, bad.Code)
}

func TestOpeningIsLimitedToCheckoutsThatWereFound(t *testing.T) {
	s := newTestLauncher(t)

	// The request names a directory to start a process in. Anything but a
	// directory the scan actually found has to be refused.
	for _, body := range []string{
		`{"dir":"/etc","id":"x"}`,
		`{"dir":"/","id":""}`,
		`{"dir":"` + t.TempDir() + `"}`,
		`{}`,
	} {
		rec := do(t, s, "POST", "/api/open", body)
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "accepted %s", body)
		assert.Contains(t, rec.Body.String(), "not one of the checkouts")
	}
}

func TestASessionMustBelongToTheCheckoutItIsOpenedIn(t *testing.T) {
	s := newTestLauncher(t)

	var withSessions, other listedDir
	for _, dir := range checkoutsOf(listRepos(t, s)) {
		if len(dir.Sessions) > 0 && withSessions.Path == "" {
			withSessions = dir
			continue
		}
		if len(dir.Sessions) == 0 && other.Path == "" {
			other = dir
		}
	}
	if withSessions.Path == "" || other.Path == "" {
		t.Skip("needs a checkout with sessions and one without")
	}

	// Naming somebody else's conversation would resume it in the wrong place.
	rec := do(t, s, "POST", "/api/open",
		`{"dir":"`+other.Path+`","id":"`+withSessions.Sessions[0].ID+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "not one of that checkout's sessions")
}

func TestHealthNamesThisProcess(t *testing.T) {
	s := newTestLauncher(t)

	// A second launch proves the port is ours by this answer, so it has to name
	// the same home the record does.
	rec := do(t, s, "GET", "/healthz", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"ade":true`)
	assert.Contains(t, rec.Body.String(), s.home)
}

// The editor is the only front end: the launcher serves an API and a proxy,
// and nothing that a browser could land on.
func TestTheLauncherServesNoPage(t *testing.T) {
	s := newTestLauncher(t)

	for _, path := range []string{"/", "/assets/launcher-abc.js"} {
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusNotFound, rec.Code, path)
	}
}

func TestTheSessionEndpointHandsBackTheLiveToken(t *testing.T) {
	s := newTestLauncher(t)

	// Outside the guard, because a tab reaching for this has no working token
	// by definition.
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/session", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"token":"`+s.token+`"}`, rec.Body.String())

	// But only down the loopback listener: elsewhere it is the whole API.
	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, arrivedOn(t, "100.84.12.9:29233", "/api/session"))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.NotContains(t, rec.Body.String(), s.token)
}

// arrivedOn is a request with the listener that took it recorded on it.
func arrivedOn(t *testing.T, local, path string) *http.Request {
	t.Helper()
	addr, err := net.ResolveTCPAddr("tcp", local)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	return req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, net.Addr(addr)))
}

func TestArchivingASession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	rec := do(t, s, "POST", "/api/archive", `{"id":"sess-1","archived":true}`)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"changed":1`)
	rec = do(t, s, "POST", "/api/archive", `{"id":"","archived":true}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// Clearing a pile-up is the whole point of archiving rather than deleting, so
// the list form has to work and has to be reversible.
func TestArchivingAPileOfSessionsAtOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	rec := do(t, s, "POST", "/api/archive", `{"ids":["a","b","c"],"archived":true}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"changed":3`)

	// Already archived: nothing to change, and no complaint.
	rec = do(t, s, "POST", "/api/archive", `{"ids":["a","b"],"archived":true}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"changed":0`)

	// Recovering one is the same call the other way.
	rec = do(t, s, "POST", "/api/archive", `{"ids":["b"],"archived":false}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"changed":1`)
	assert.False(t, settings.Archived()["b"], "b came back")
	assert.True(t, settings.Archived()["a"], "a stayed put away")

	rec = do(t, s, "POST", "/api/archive", `{"ids":[],"archived":true}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDeletingSessionsFromTheLauncher(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	rec := do(t, s, "POST", "/api/sessions/delete", `{"ids":["sess-1","sess-2"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"removed":2}`, rec.Body.String())
	assert.True(t, settings.Hidden()["sess-1"])

	rec = do(t, s, "POST", "/api/sessions/delete", `{"ids":[]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTouchKeepsAnIdleLauncherUp(t *testing.T) {
	s := newTestLauncher(t)
	stale := time.Now().Add(-time.Hour)
	s.mu.Lock()
	s.used = stale
	s.mu.Unlock()

	// Every guarded request counts as use, or a launcher you are still reading
	// would give the port back under you.
	do(t, s, "GET", "/api/repos", "")

	s.mu.Lock()
	used := s.used
	s.mu.Unlock()
	// Moved forward, rather than within some window of now: scanning every
	// checkout on the fixture is not instant, least of all under -race.
	assert.True(t, used.After(stale), "%s did not move past %s", used, stale)
}

func TestTokensDiffer(t *testing.T) {
	assert.NotEqual(t, newToken(), newToken())
	assert.Len(t, newToken(), 32)
}

func TestPruningIsLimitedToRepositoriesThatWereFound(t *testing.T) {
	s := newTestLauncher(t)

	// Pruning removes directories, so the repository named has to be one the
	// scan offered rather than any path on the fixture.
	for _, body := range []string{`{"repo":"/etc","paths":["/etc/hosts"]}`, `{"repo":"","paths":[]}`} {
		rec := do(t, s, "POST", "/api/prune", body)
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "accepted %s", body)
		assert.Contains(t, rec.Body.String(), "not one of the repositories")
	}
	rec := do(t, s, "GET", "/api/worktrees?repo=/etc", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// A browser fetches a favicon with no token and no header, so the mark is
// outside the guard like the page it sits on.
func TestTheLauncherWearsTheMark(t *testing.T) {
	s := newTestLauncher(t)

	req := httptest.NewRequest(http.MethodGet, "/icon.svg", nil)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/svg+xml", rec.Header().Get("Content-Type"))
	assert.Equal(t, ui.Icon, rec.Body.String(), "the same mark every surface wears")
}

func TestATypedPathMayCarryATilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	checkout := filepath.Join(home, "code", "thing")
	require.NoError(t, os.MkdirAll(filepath.Join(checkout, ".git"), 0o755))

	dir, ok := browsed("~/code/thing")
	require.True(t, ok)
	assert.Equal(t, checkout, dir.Path)
	assert.Equal(t, "thing", dir.Name)

	// Still only a real checkout: expansion is not permission.
	_, ok = browsed("~/code")
	assert.False(t, ok)
}

// The rail draws what announced itself, so what it draws has to be openable.
// A session started just now has no transcript for the scan to find, and a
// checkout outside the scan roots has no listing at all.
func TestAnAnnouncedSessionOpensWithoutTheScan(t *testing.T) {
	s := newTestLauncher(t)
	dir, port := liveAde(t, session.Health{})
	s.served["fresh"] = Serving{ID: "fresh", Dir: dir, Port: port}

	var gotDir, gotID string
	s.open = func(_ context.Context, dir, _, _, id string) (string, error) {
		gotDir, gotID = dir, id
		return "http://127.0.0.1:1234/", nil
	}

	rec := do(t, s, "POST", "/api/open", `{"dir":"`+dir+`","id":"fresh","direct":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, dir, gotDir)
	assert.Equal(t, "fresh", gotID, "the session was opened without being resumed")

	// Still only that checkout's: an announcement elsewhere does not let a
	// session be opened here.
	other, _ := liveAde(t, session.Health{})
	refused := do(t, s, "POST", "/api/open", `{"dir":"`+other+`","id":"fresh"}`)
	assert.Equal(t, http.StatusBadRequest, refused.Code)
	assert.Contains(t, refused.Body.String(), "not one of that checkout's sessions")
}

func TestAPickedAgentAndModelHaveToBeOnTheList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	for _, bad := range [][2]string{
		{"gemini", ""},
		{"claude", "gpt-5.6-terra"},
		{"codex", "opus"},
		{"claude", "; rm -rf /"},
	} {
		_, _, err := picked(bad[0], bad[1])
		assert.Errorf(t, err, "accepted agent %q model %q", bad[0], bad[1])
	}

	// Both empty is the configured default, which is what every other door
	// into a session asks for.
	agent, model, err := picked("", "")
	require.NoError(t, err)
	assert.Empty(t, agent)
	assert.Empty(t, model)

	agent, model, err = picked("codex", "gpt-5.6-terra")
	require.NoError(t, err)
	assert.Equal(t, "codex", agent)
	assert.Equal(t, "gpt-5.6-terra", model)

	// A model with no agent beside it names its own; claude on a gpt id is a
	// turn that fails at the first request rather than at the door.
	agent, model, err = picked("", "gpt-5.6-terra")
	require.NoError(t, err)
	assert.Equal(t, "codex", agent)
	assert.Equal(t, "gpt-5.6-terra", model)
}

func TestAPickedAgentOnlyAppliesToANewSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	dir, port := liveAde(t, session.Health{})
	s.served["fresh"] = Serving{ID: "fresh", Dir: dir, Port: port}

	var gotAgent, gotModel string
	s.open = func(_ context.Context, _, agent, model, _ string) (string, error) {
		gotAgent, gotModel = agent, model
		return "http://127.0.0.1:1234/", nil
	}

	// Resuming carries the agent the session already has: a page cannot move
	// a running conversation onto another model by asking to open it.
	rec := do(t, s, "POST", "/api/open",
		`{"dir":"`+dir+`","id":"fresh","agent":"codex","model":"gpt-5.6-terra"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, gotAgent)
	assert.Empty(t, gotModel)
}

func TestRenamingASessionMakesTheNameYours(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	rec := do(t, s, http.MethodPost, "/api/sessions/rename", `{"id":"s1","title":"the worker dashboard"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "the worker dashboard", settings.Titles()["s1"])
	assert.True(t, settings.NamedByYou("s1"), "ade must not rename it again")

	rec = do(t, s, http.MethodPost, "/api/sessions/rename", `{"title":"no id"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
