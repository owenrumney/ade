package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
)

// The bar is one bar across every machine, so a connected host's sessions have
// to arrive in this machine's list, marked with where they are.
func TestAConnectedHostsSessionsArriveMarkedWithTheMachine(t *testing.T) {
	var asked string
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.Header.Get("X-Ade-Token")
		_ = json.NewEncoder(w).Encode([]ActiveSession{
			{ID: "a1", Dir: "/home/owen/work", Name: "work"},
			// Already relayed from somewhere else: not this host's to pass on.
			{ID: "b2", Dir: "/elsewhere", Name: "elsewhere", Machine: "third"},
		})
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}

	rows := s.hostSessions(context.Background())
	require.Len(t, rows, 1)
	assert.Equal(t, "a1", rows[0].ID)
	assert.Equal(t, "linux", rows[0].Machine)
	assert.Equal(t, "faraway", asked)
}

func TestRemoteOpenCarriesThePickedAgentAndModel(t *testing.T) {
	var got struct {
		Dir, ID, Agent, Model string
		Direct                bool
	}
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/open", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_ = json.NewEncoder(w).Encode(map[string]string{"url": farURL(r)})
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}
	rec := do(t, s, http.MethodPost, "/api/open", `{"machine":"linux","dir":"/home/ade/code","id":"","agent":"codex","model":"gpt-5.6-terra"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "/home/ade/code", got.Dir)
	assert.Equal(t, "codex", got.Agent)
	assert.Equal(t, "gpt-5.6-terra", got.Model)
	// The path that machine opened on, which is the folder a window needs.
	assert.Contains(t, rec.Body.String(), `"path":"/home/ade/code"`)
}

func farURL(r *http.Request) string {
	return "http://" + r.Host + "/?t=backend"
}

func TestTargetReadsOnlyTheSelectedHostsChoices(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Save(settings.Settings{Hosts: []settings.Host{{Name: "linux", Label: "backend"}}}))

	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos":
			_ = json.NewEncoder(w).Encode(map[string]any{"repos": []map[string]string{{"path": "/home/ade/code/api", "name": "api"}}})
		case "/api/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"id": "opus", "label": "Opus", "agent": "claude"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}
	rec := do(t, s, http.MethodPost, "/api/target", `{"machine":"linux"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"path":"/home/ade/code/api"`)
	assert.Contains(t, rec.Body.String(), `"id":"opus"`)
}

func TestLaptopCatalogueOffersOriginsRatherThanLaptopPaths(t *testing.T) {
	was := readOrigin
	readOrigin = func(_ context.Context, dir string) (string, error) {
		assert.Equal(t, "/Users/owen/code/infracost/cli", dir)
		return "git@github.com:infracost/infracost.git", nil
	}
	t.Cleanup(func() { readOrigin = was })

	got := laptopCatalogue(context.Background(), []sessions.Repo{{
		Path: "/Users/owen/code/infracost/cli", Owner: "infracost", Name: "cli", Branch: "main",
	}}, nil)
	require.Len(t, got, 1)
	assert.Equal(t, "git@github.com:infracost/infracost.git", got[0].Path)
	assert.Empty(t, got[0].Checkouts)
}

func TestLaptopCatalogueLeavesOutWhatTheWorkerAlreadyHas(t *testing.T) {
	was := readOrigin
	readOrigin = func(context.Context, string) (string, error) {
		return "git@github.com:infracost/cli.git", nil
	}
	t.Cleanup(func() { readOrigin = was })

	got := laptopCatalogue(context.Background(),
		[]sessions.Repo{{Path: "/Users/owen/code/infracost/cli", Name: "cli"}},
		[]sessions.Repo{{Path: "/srv/ade/abc/cli", Name: "cli"}})
	assert.Empty(t, got, "the worker's own checkout is the one to offer")
}

func TestTargetFailureNeverStartsALocalSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Save(settings.Settings{Hosts: []settings.Host{{Name: "linux"}}}))

	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "tailnet unavailable", http.StatusBadGateway)
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}
	opened := false
	s.open = func(context.Context, string, string, string, string) (string, error) {
		opened = true
		return "", nil
	}
	rec := do(t, s, http.MethodPost, "/api/target", `{"machine":"linux"}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.False(t, opened, "a failed remote target must not fall back locally")
}

func TestNoHostsMeansNoRemoteRows(t *testing.T) {
	s := newTestLauncher(t)
	assert.Empty(t, s.hostSessions(context.Background()))
}

// A host that will not answer must not hold up or empty the list of sessions
// on this machine.
func TestAHostThatWillNotAnswerIsSkipped(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer dead.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: dead.URL + "/?t=faraway"}
	assert.Empty(t, s.hostSessions(context.Background()))
}

// New session means a new conversation. An empty id reopens whatever the
// checkout was last in, which is how a "new" session arrives with history.
func TestRemoteOpenCarriesTheFreshFlag(t *testing.T) {
	var got struct {
		Dir, ID string
		Fresh   bool
	}
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_ = json.NewEncoder(w).Encode(map[string]string{"url": farURL(r)})
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}
	rec := do(t, s, http.MethodPost, "/api/open", `{"machine":"linux","dir":"/home/ade/code","id":"","fresh":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, got.Fresh, "the worker is told to start a conversation")
}

// Recent sessions is a list of your work, not of this machine's: a session on
// a connected worker belongs in it, marked with where it is.
func TestConnectedHostsCheckoutsArriveMarkedWithTheMachine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Save(settings.Settings{Hosts: []settings.Host{{Name: "linux", Label: "backend"}}}))

	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/repos", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"repos": []sessions.Repo{{Path: "/home/ade/code/api", Name: "api"}}})
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}

	rec := do(t, s, http.MethodGet, "/api/hosts/repos", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Hosts []MachineRepos `json:"hosts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Hosts, 1)
	assert.Equal(t, "linux", body.Hosts[0].Machine)
	assert.Equal(t, "backend", body.Hosts[0].Label)
	require.Len(t, body.Hosts[0].Repos, 1)
	assert.Equal(t, "/home/ade/code/api", body.Hosts[0].Repos[0].Path)
}

// A machine that is not connected is not dialled to answer this: the list is
// what is already reachable, and the dash asks for it on every sweep.
func TestCheckoutsAreNotAskedForFromAnUnconnectedHost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Save(settings.Settings{Hosts: []settings.Host{{Name: "linux"}}}))

	s := newTestLauncher(t)
	rec := do(t, s, http.MethodGet, "/api/hosts/repos", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"hosts":[]}`, rec.Body.String())
}

// Closing a session from the rail has to reach the machine it is on. This one
// has no such checkout, so guessing locally would refuse a legitimate close.
func TestStoppingARemoteSessionIsAskedOfThatMachine(t *testing.T) {
	var got struct {
		Dir     string `json:"dir"`
		Machine string `json:"machine"`
	}
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/graph/stop", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}

	rec := do(t, s, http.MethodPost, "/api/graph/stop", `{"machine":"linux","dir":"/home/ade/code/api"}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, "/home/ade/code/api", got.Dir)
	// The far machine answers for itself: relaying the name would make it ask on.
	assert.Empty(t, got.Machine)
}

func TestCancellingARemoteSessionIsAskedOfThatMachine(t *testing.T) {
	var got struct {
		Dir string `json:"dir"`
	}
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/graph/cancel", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}

	rec := do(t, s, http.MethodPost, "/api/graph/cancel", `{"machine":"linux","dir":"/home/ade/code/api"}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, "/home/ade/code/api", got.Dir)
}

func TestTheGraphOfAMachineIsAskedOfThatMachine(t *testing.T) {
	const graph = `{"repos":[{"path":"/home/ade/code/api","name":"api","checkouts":[]}]}`
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET /api/graph", r.Method+" "+r.URL.Path)
		require.Equal(t, "faraway", r.Header.Get("X-Ade-Token"))
		_, _ = w.Write([]byte(graph))
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}

	rec := do(t, s, http.MethodGet, "/api/graph?machine=linux", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, graph, rec.Body.String())

	rec = do(t, s, http.MethodGet, "/api/graph?machine=elsewhere", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
