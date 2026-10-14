package sessions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/session"
)

func TestCancelAgentAsksTheRecordedServerWithItsToken(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ade")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	assert.ErrorContains(t, CancelAgent(context.Background(), dir), "no ade is running")

	var got, token string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, token = r.Method+" "+r.URL.Path, r.Header.Get("X-Ade-Token")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	require.NoError(t, session.Write(session.Path(filepath.Join(dir, ".git")),
		session.Record{PID: os.Getpid(), URL: server.URL + "/?t=secret", Root: dir}))

	require.NoError(t, CancelAgent(context.Background(), dir))
	assert.Equal(t, "POST /api/cancel", got)
	assert.Equal(t, "secret", token)
}

func TestTheGraphShowsLiveServersAndTheirAgents(t *testing.T) {
	main := filepath.Join(t.TempDir(), "ade")
	wt := filepath.Join(t.TempDir(), "ade-fix")
	for _, dir := range []string{main, wt} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	}
	// A record for the main checkout; the worktree has none.
	require.NoError(t, session.Write(session.Path(filepath.Join(main, ".git")),
		session.Record{PID: 4242, URL: "http://127.0.0.1:1/?t=x", Root: main, StartedAt: time.Unix(1_700_000_000, 0)}))

	repos := []Repo{{
		Path: main, Name: "ade", Owner: "owenrumney",
		Checkouts: []Dir{
			{Path: main, Name: "ade", Branch: "main", Here: true, Sessions: []Session{{ID: "a"}, {ID: "b"}}},
			{Path: wt, Name: "ade-fix", Branch: "fix", Worktree: true},
		},
	}}

	probed := 0
	live := func(_ context.Context, rec *session.Record) (session.Health, bool) {
		probed++
		assert.Equal(t, 4242, rec.PID)
		return session.Health{Ade: true, Branch: "feat/x", Agent: &session.AgentHealth{
			Running: true, Kind: "chat", Model: "opus", SideRuns: []string{"security", "tester"},
		}}, true
	}
	graph := Graph(context.Background(), repos, live)
	require.Len(t, graph, 1)
	require.Len(t, graph[0].Checkouts, 2)
	assert.Equal(t, 1, probed, "only the checkout with a record is asked")

	first := graph[0].Checkouts[0]
	require.NotNil(t, first.Server)
	assert.Equal(t, 4242, first.Server.PID)
	assert.Equal(t, "feat/x", first.Branch, "the server knows its branch better than HEAD does")
	assert.Equal(t, 2, first.Sessions)
	assert.True(t, first.Here)
	require.NotNil(t, first.Server.Agent)
	assert.True(t, first.Server.Agent.Running)
	assert.Equal(t, []string{"security", "tester"}, first.Server.Agent.SideRuns)

	second := graph[0].Checkouts[1]
	assert.Nil(t, second.Server, "no record, no server, still drawn")
	assert.True(t, second.Worktree)

	// A record whose server is gone is a checkout with no server, not a ghost.
	dead := func(context.Context, *session.Record) (session.Health, bool) { return session.Health{}, false }
	graph = Graph(context.Background(), repos, dead)
	assert.Nil(t, graph[0].Checkouts[0].Server)
	assert.Equal(t, "main", graph[0].Checkouts[0].Branch)
}

func TestGraphProbesWithTheRecordedToken(t *testing.T) {
	main := filepath.Join(t.TempDir(), "ade")
	require.NoError(t, os.MkdirAll(filepath.Join(main, ".git"), 0o750))

	// A ade that, like the real one, says what its agent is doing only to a
	// caller holding its token.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		health := session.Health{Ade: true, Root: main, PID: os.Getpid()}
		if r.URL.Path == "/healthz" && r.URL.Query().Get("t") == "tok" {
			health.Branch = "feat/x"
			health.Agent = &session.AgentHealth{Running: true, Kind: "chat"}
		}
		require.NoError(t, json.NewEncoder(w).Encode(health))
	}))
	defer srv.Close()
	require.NoError(t, session.Write(session.Path(filepath.Join(main, ".git")),
		session.Record{PID: os.Getpid(), URL: srv.URL + "/?t=tok", Root: main, StartedAt: time.Now()}))

	repos := []Repo{{Path: main, Name: "ade", Checkouts: []Dir{{Path: main, Name: "ade", Branch: "main"}}}}
	graph := Graph(context.Background(), repos, nil)
	require.Len(t, graph, 1)
	first := graph[0].Checkouts[0]
	require.NotNil(t, first.Server)
	assert.Equal(t, "feat/x", first.Branch)
	require.NotNil(t, first.Server.Agent)
	assert.True(t, first.Server.Agent.Running)
}
