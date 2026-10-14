package web

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/wiki"
)

// newTestServer builds a server over a repo whose feature branch changes a.go.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	write := func(name, body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}

	git("init", "-b", "main", "-q")
	write("a.go", "package main\n\nvar x = 1\n")
	git("add", ".")
	git("commit", "-qm", "base")
	git("checkout", "-qb", "feature")
	write("a.go", "package main\n\nvar x = 2\n")
	git("add", ".")
	git("commit", "-qm", "change")

	store, err := comments.Open(filepath.Join(dir, ".git"), "feature")
	require.NoError(t, err)

	s, err := New(Config{
		Root:   dir,
		Branch: "feature",
		GitDir: filepath.Join(dir, ".git"),
		// The same directory in an ordinary checkout, and the one designs live
		// under so a worktree can find them too.
		CommonGitDir: filepath.Join(dir, ".git"),
		Diff:         gitdiff.Options{Repo: dir},
	}, store)
	require.NoError(t, err)
	require.NoError(t, s.refresh(context.Background()))
	// Registered after TempDir's own cleanup, so it runs first: a turn's
	// goroutines are still writing under .git as the directory goes.
	t.Cleanup(s.Close)
	return s
}

func do(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("X-Ade-Token", s.token)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	return rec
}

func createThread(t *testing.T, s *Server, body string) comments.Thread {
	t.Helper()
	rec := do(t, s, "POST", "/api/threads", body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var thread comments.Thread
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &thread))
	return thread
}

func TestTokenGuard(t *testing.T) {
	s := newTestServer(t)

	t.Run("rejects a missing token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/state", nil))
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("rejects a wrong token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/state", nil)
		req.Header.Set("X-Ade-Token", "nope")
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("accepts the header", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do(t, s, "GET", "/api/state", "").Code)
	})

	t.Run("accepts a query token for EventSource", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/state?t="+s.token, nil)
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestStateEndpoint(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "GET", "/api/state", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var got stateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	diff := getDiff(t, s)
	require.Len(t, diff.Diff.Files, 1)
	assert.Equal(t, "a.go", diff.Diff.Files[0].Path)
	assert.Equal(t, gitdiff.ModeWorktree, got.Meta.Mode)
	assert.Equal(t, "feature", got.Meta.Branch)
	assert.Empty(t, got.Threads)

	// Threads are anchored to diff lines, so the client must be able to tell
	// which diff the threads it was handed belong to.
	assert.NotEmpty(t, got.DiffVersion)
	assert.Equal(t, got.DiffVersion, diff.Version, "state named a version /api/diff does not serve")
}

// getDiff reads the diff from the endpoint that now owns it.
func getDiff(t *testing.T, s *Server) diffResponse {
	t.Helper()
	rec := do(t, s, "GET", "/api/diff", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var got diffResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotNil(t, got.Diff, "the browser needs a diff object even when there is no change")
	return got
}

func TestTheDiffEndpointIsGuarded(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/diff", nil)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, "the diff is repository content")
}

func TestCreateThread(t *testing.T) {
	s := newTestServer(t)

	t.Run("anchors to a line in the diff", func(t *testing.T) {
		thread := createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"why 2?"}`)
		assert.Equal(t, 3, thread.Line)
		assert.Equal(t, comments.AuthorYou, thread.Comments[0].Author)
		assert.Equal(t, "var x = 2", thread.Anchor.Content)
		assert.NotEmpty(t, thread.Anchor.ContextHash)
	})

	t.Run("comments on the base side too", func(t *testing.T) {
		thread := createThread(t, s, `{"file":"a.go","side":"left","line":3,"body":"was fine"}`)
		assert.Equal(t, "var x = 1", thread.Anchor.Content)
	})

	tests := []struct {
		name string
		body string
		want string
	}{
		{"line not in diff", `{"file":"a.go","side":"right","line":9999,"body":"x"}`, "not in the current diff"},
		{"file not in diff", `{"file":"nope.go","side":"right","line":1,"body":"x"}`, "not in the current diff"},
		{"context line has no anchor", `{"file":"a.go","side":"right","line":1,"body":"x"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, s, "POST", "/api/threads", tt.body)
			if tt.want == "" {
				// Context lines are anchorable, so this one is allowed.
				assert.Equal(t, http.StatusOK, rec.Code)
				return
			}
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.want)
		})
	}

	t.Run("rejects invalid JSON", func(t *testing.T) {
		rec := do(t, s, "POST", "/api/threads", `{not json`)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "invalid JSON")
	})

	t.Run("rejects a blank body", func(t *testing.T) {
		rec := do(t, s, "POST", "/api/threads", `{"file":"a.go","side":"right","line":3,"body":"   "}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestThreadLifecycle(t *testing.T) {
	s := newTestServer(t)
	thread := createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"why 2?"}`)

	t.Run("reply appends", func(t *testing.T) {
		rec := do(t, s, "POST", "/api/threads/"+thread.ID+"/reply", `{"body":"because"}`)
		require.Equal(t, http.StatusOK, rec.Code)
		var got comments.Thread
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Len(t, got.Comments, 2)
	})

	t.Run("resolve then reopen", func(t *testing.T) {
		rec := do(t, s, "POST", "/api/threads/"+thread.ID+"/resolve", `{"resolved":true}`)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"resolved":true`)

		rec = do(t, s, "POST", "/api/threads/"+thread.ID+"/resolve", `{"resolved":false}`)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"resolved":false`)
	})

	t.Run("delete", func(t *testing.T) {
		rec := do(t, s, "DELETE", "/api/threads/"+thread.ID, "")
		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.Empty(t, s.ws.store.List())
	})

	t.Run("unknown ids are 404", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, do(t, s, "POST", "/api/threads/nope/reply", `{"body":"x"}`).Code)
		assert.Equal(t, http.StatusNotFound, do(t, s, "POST", "/api/threads/nope/resolve", `{"resolved":true}`).Code)
		assert.Equal(t, http.StatusNotFound, do(t, s, "DELETE", "/api/threads/nope", "").Code)
	})
}

func TestRefreshReanchorsThreads(t *testing.T) {
	s := newTestServer(t)
	thread := createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"why 2?"}`)

	// Insert a line above the anchored one, as a Resolve run would.
	require.NoError(t, os.WriteFile(filepath.Join(s.ws.homeRoot, "a.go"),
		[]byte("package main\n\nconst c = 1\n\nvar x = 2\n"), 0o600))
	require.NoError(t, s.refresh(context.Background()))

	got, err := s.ws.store.Get(thread.ID)
	require.NoError(t, err)
	assert.Equal(t, 5, got.Line, "thread should follow the shifted line")
	assert.False(t, got.Orphaned)
}

func TestRefreshOrphansRewrittenLines(t *testing.T) {
	s := newTestServer(t)
	thread := createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"why 2?"}`)

	require.NoError(t, os.WriteFile(filepath.Join(s.ws.homeRoot, "a.go"),
		[]byte("package main\n\nvar renamed = 42\n"), 0o600))
	require.NoError(t, s.refresh(context.Background()))

	got, err := s.ws.store.Get(thread.ID)
	require.NoError(t, err)
	assert.True(t, got.Orphaned, "the anchored content is gone")
	assert.Equal(t, "why 2?", got.Comments[0].Body, "the comment itself must survive")
}

func TestTheSessionEndpointHandsBackTheLiveToken(t *testing.T) {
	s := newTestServer(t)

	// Outside the guard, because a tab reaching for this has no working token by
	// definition: its port was reused and its token was not.
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/session", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"token":"`+s.token+`"}`, rec.Body.String())

	// And only down the loopback listener: the token there is the terminal.
	addr, err := net.ResolveTCPAddr("tcp", "192.168.1.50:25352")
	require.NoError(t, err)
	req := httptest.NewRequest("GET", "/api/session", nil)
	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, net.Addr(addr))))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.NotContains(t, rec.Body.String(), s.token)
}

func TestAnotherHostIsRefused(t *testing.T) {
	// Loopback stops the network. This stops a name that has been rebound to
	// 127.0.0.1 reaching us from a page already in your browser.
	assert.True(t, localHost("127.0.0.1:8080", "8080"))
	assert.True(t, localHost("localhost:8080", "8080"))
	assert.True(t, localHost("[::1]:8080", "8080"))

	assert.False(t, localHost("evil.com:8080", "8080"), "a rebound name must fail at the door")
	assert.False(t, localHost("127.0.0.1:9999", "8080"), "another port is another server")
	assert.False(t, localHost("127.0.0.1", "8080"), "no port means port 80, which is never ours")
	assert.False(t, localHost("", "8080"))

	s := newTestServer(t)
	s.listenPort = "8080"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/session", nil)
	req.Host = "evil.com:8080"
	s.routes().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAContainerNameIsOurs(t *testing.T) {
	t.Setenv("ADE_WORKER_NAME", "ade-worker")
	assert.True(t, localHost("host.docker.internal:8080", "8080"))
	assert.True(t, localHost("ade-worker:8080", "8080"))
	assert.False(t, localHost("ade-worker:9999", "8080"))

	t.Setenv("ADE_WORKER_NAME", "")
	assert.False(t, localHost("ade-worker:8080", "8080"), "off a worker the name means nothing")
}

func TestTheWikiToolPassesTheHostCheck(t *testing.T) {
	s := newTestServer(t)
	s.listenPort = "8080"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", wiki.MCPPath, strings.NewReader("{}"))
	req.Host = "hub-namespace:8080"
	s.routes().ServeHTTP(rec, req)
	assert.NotEqual(t, http.StatusForbidden, rec.Code, "the bearer draft id is the check here")

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", wiki.MCPPath, nil)
	req.Host = "hub-namespace:8080"
	s.routes().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, "only the tool's own POST is exempt")
}

func TestAWorkerBoxReachesUsByTheWorkerName(t *testing.T) {
	t.Setenv("ADE_WORKER_NAME", "ade-worker")
	hub := sandbox.Docker{Network: "container:ade-hub"}
	assert.Equal(t, "http://ade-worker:4000", workerBase(hub, "http://127.0.0.1:4000"))
	assert.Equal(t, "http://127.0.0.1:4000", workerBase(sandbox.Docker{}, "http://127.0.0.1:4000"),
		"on a laptop the box's own host rewrite applies")
}
func TestUnknownPathIs404(t *testing.T) {
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestStateCarriesSyntaxTokens(t *testing.T) {
	s := newTestServer(t)

	body := getDiff(t, s)

	var checked int
	for _, file := range body.Diff.Files {
		for _, hunk := range file.Hunks {
			for _, row := range hunk.Rows {
				for _, cell := range []*gitdiff.Cell{row.Left, row.Right} {
					if cell == nil || len(cell.Tokens) == 0 {
						continue
					}
					var rebuilt string
					for _, token := range cell.Tokens {
						rebuilt += token.Text
					}
					assert.Equal(t, cell.Content, rebuilt, "tokens must rebuild the line exactly")
					checked++
				}
			}
		}
	}
	assert.NotZero(t, checked, "the test repo is Go, so some lines should be highlighted")
}

func TestTickSurvivesAPanickingRefresh(t *testing.T) {
	s := newTestServer(t)
	s.ws.source = func(context.Context) (*gitdiff.Diff, error) { panic("lexer went wrong") }

	// An unrecovered panic here would end the process, not just this refresh.
	assert.NotPanics(t, func() { s.tick(context.Background()) })

	// And the server keeps working afterwards.
	s.ws.source = func(context.Context) (*gitdiff.Diff, error) { return &gitdiff.Diff{}, nil }
	assert.NotPanics(t, func() { s.tick(context.Background()) })
	assert.Equal(t, http.StatusOK, do(t, s, "GET", "/api/state", "").Code)
}

func TestRecoverRunKeepsTheServerUp(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)

	assert.NotPanics(t, func() {
		defer s.recoverRun("review")
		panic("something in the run went wrong")
	})

	// The run is reported as failed and the lock is released, so the next one
	// is not blocked forever by a run that died.
	agent := s.agent.State()
	assert.False(t, agent.Running)
	assert.Contains(t, agent.Error, "panicked")
	assert.Equal(t, http.StatusOK, do(t, s, "GET", "/api/state", "").Code)
}

func TestTheStateNamesTheBranchForTheTitle(t *testing.T) {
	s := newTestServer(t)

	// The window title used to be templated into the page. It is now set from
	// here, so this is what has to carry the branch.
	rec := do(t, s, "GET", "/api/state", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"branch":"feature"`)
}

func TestTheStateSaysWhichProjectThisIs(t *testing.T) {
	s := newTestServer(t)

	// The test repository has no remote, so there is nothing to say — and
	// saying nothing is better than guessing from the directory name.
	rec := do(t, s, "GET", "/api/state", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), `"project"`)

	git := exec.Command("git", "remote", "add", "origin", "git@github.com:owenrumney/ade.git")
	git.Dir = s.space().homeRoot
	out, err := git.CombinedOutput()
	require.NoError(t, err, string(out))

	// Worked out once per checkout, so the cache has to be primed by the first
	// ask rather than by construction.
	assert.Equal(t, "owenrumney/ade", s.project())

	rec = do(t, s, "GET", "/api/state", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"project":"owenrumney/ade"`)
}

func TestResettingTheCommentsClearsWhatAdeFiled(t *testing.T) {
	s := newTestServer(t)
	createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"mine"}`)
	createThread(t, s, `{"scope":"repo","body":"about the change as a whole"}`)

	imported, err := s.space().store.Add(comments.Thread{
		File: "a.go", Side: "right", Line: 4, ExternalID: "gh-7",
		Comments: []comments.Comment{{Body: "from the pull request"}},
	})
	require.NoError(t, err)

	rec := do(t, s, "DELETE", "/api/threads", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var res struct{ Removed int }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Equal(t, 2, res.Removed)

	left := s.space().store.List()
	require.Len(t, left, 1)
	assert.Equal(t, imported.ID, left[0].ID, "a pull request's own comment is not ade's to delete")
}
