package launcher

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
)

// upstream stands in for the ade serving a checkout, and records what the
// proxy asked it for.
type upstream struct {
	server *httptest.Server
	seen   chan *http.Request
}

func newUpstream(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	up := &upstream{seen: make(chan *http.Request, 8)}
	up.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up.seen <- r
		handler(w, r)
	}))
	t.Cleanup(up.server.Close)
	return up
}

func (u *upstream) port(t *testing.T) int {
	t.Helper()
	parsed, err := url.Parse(u.server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	return port
}

// proxied stands the launcher up in front of one upstream, reachable as the
// session "abc" with the token "upstream-token".
func proxied(t *testing.T, handler http.HandlerFunc) (*Server, *upstream, *httptest.Server) {
	t.Helper()
	up := newUpstream(t, handler)
	s := newTestLauncher(t)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))
	require.NoError(t, session.Write(session.Path(sessions.GitDir(dir)), session.Record{
		PID: os.Getpid(), Root: dir, StartedAt: time.Now(),
		URL: up.server.URL + "/?t=upstream-token",
	}))
	s.locate = func(_ context.Context, id string) (string, int, bool) {
		if id != "abc" {
			return "", 0, false
		}
		return dir, up.port(t), true
	}

	front := httptest.NewServer(s.routes())
	t.Cleanup(front.Close)
	return s, up, front
}

func TestTheProxyReachesASessionByItsID(t *testing.T) {
	s, up, front := proxied(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	res, err := http.Get(front.URL + "/s/abc/api/state?t=" + s.token)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusOK, res.StatusCode)

	got := <-up.seen
	// The prefix is the launcher's: the far ade knows nothing about it.
	assert.Equal(t, "/api/state", got.URL.Path)
	// The browser holds the launcher's token; the session's is put on here.
	assert.Equal(t, "upstream-token", got.Header.Get("X-Ade-Token"))
	assert.Equal(t, "upstream-token", got.URL.Query().Get("t"))
}

func TestTheProxyRefusesASessionNothingIsServing(t *testing.T) {
	s, _, front := proxied(t, func(http.ResponseWriter, *http.Request) {})

	res, err := http.Get(front.URL + "/s/nobody/api/state?t=" + s.token)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestTheProxyIsBehindTheToken(t *testing.T) {
	_, _, front := proxied(t, func(http.ResponseWriter, *http.Request) {})

	res, err := http.Get(front.URL + "/s/abc/api/state")
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusForbidden, res.StatusCode)
}

// An unflushed proxy holds the stream until its buffer fills, which is the
// whole page waiting on a session that has already spoken.
func TestAProxiedEventArrivesBeforeTheStreamCloses(t *testing.T) {
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	s, _, front := proxied(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: state\ndata: {}\n\n"))
		w.(http.Flusher).Flush()
		select {
		case <-hold:
		case <-r.Context().Done():
		}
	})

	req, err := http.NewRequest(http.MethodGet, front.URL+"/s/abc/api/events?t="+s.token, nil)
	require.NoError(t, err)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()

	frame := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(res.Body).ReadString('\n')
		frame <- line
	}()
	select {
	case line := <-frame:
		assert.Equal(t, "event: state\n", line)
	case <-time.After(3 * time.Second):
		t.Fatal("the event was buffered rather than flushed")
	}
}

// The page is served from the launcher's origin, so a link the session server
// anchored at the root would miss everything the session serves.
func TestAProxiedPageKeepsItsLinksUnderThePrefix(t *testing.T) {
	s, _, front := proxied(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<link href="/design-assets/markdown.css"><script src="/assets/app.js">`))
	})

	res, err := http.Get(front.URL + "/s/abc/?t=" + s.token)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()

	body := make([]byte, 512)
	n, _ := res.Body.Read(body)
	page := string(body[:n])
	assert.Contains(t, page, `href="/s/abc/design-assets/markdown.css"`)
	assert.Contains(t, page, `src="/s/abc/assets/app.js"`)
}

func TestTheProxyRootRedirectsToItsPage(t *testing.T) {
	s, _, front := proxied(t, func(http.ResponseWriter, *http.Request) {})

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Get(front.URL + "/s/abc?t=" + s.token)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusMovedPermanently, res.StatusCode)
	assert.True(t, strings.HasPrefix(res.Header.Get("Location"), "/s/abc/"), res.Header.Get("Location"))
}

// Opening a session hands the page this launcher's address for it, so the tab
// that lands there can switch to another session without moving origin.
func TestOpeningASessionHandsBackItsProxiedAddress(t *testing.T) {
	s := newTestLauncher(t)
	var found listedDir
	for _, dir := range checkoutsOf(listRepos(t, s)) {
		if len(dir.Sessions) > 0 {
			found = dir
			break
		}
	}
	if found.Path == "" {
		t.Skip("no checkout on this machine has a session to open")
	}
	id := found.Sessions[0].ID

	s.setPort(4321)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		return "http://127.0.0.1:1234/?t=direct", nil
	}
	s.locate = func(_ context.Context, want string) (string, int, bool) {
		return found.Path, 1234, want == id
	}

	req := httptest.NewRequest(http.MethodPost, "/api/open",
		strings.NewReader(`{"dir":"`+found.Path+`","id":"`+id+`"}`))
	// A name this launcher bound: the same-host guard turns anything else away.
	req.Host = "127.0.0.1:4321"
	req.Header.Set("X-Ade-Token", s.token)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct{ URL string }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Contains(t, got.URL, "/s/"+id+"/")
	assert.Contains(t, got.URL, "t="+s.token)
}

// The page is public on the checkout's own server and has to stay public
// here: a stylesheet the browser fetches carries no token.
func TestAProxiedPageLoadsWhatItAsksForWithoutAToken(t *testing.T) {
	_, up, front := proxied(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("body {}"))
	})

	res, err := http.Get(front.URL + "/s/abc/assets/style.css")
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusOK, res.StatusCode)

	// And the session's own token is not handed to a caller that had none.
	assert.Empty(t, (<-up.seen).Header.Get("X-Ade-Token"))
}

// One tab for every session is the point, so a second ade asks whether a page
// is already open here before opening another over it.
func TestTheLauncherSaysWhetherAPageIsWatching(t *testing.T) {
	s := newTestLauncher(t)

	rec := do(t, s, http.MethodGet, "/api/watching", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"watching":false}`, rec.Body.String())

	// Behind the token like the rest: it says what is on this machine.
	req := httptest.NewRequest(http.MethodGet, "/api/watching", nil)
	bare := httptest.NewRecorder()
	s.routes().ServeHTTP(bare, req)
	assert.Equal(t, http.StatusForbidden, bare.Code)
}

// The scan only knows sessions that have left a transcript, so a checkout
// tells the launcher what it is holding rather than waiting to be found.
func TestACheckoutAnnouncesWhatItServes(t *testing.T) {
	s := newTestLauncher(t)

	// A claim has to name a live ade on that port, or anything holding the
	// launcher's token could point a session id at any loopback port.
	rec := do(t, s, http.MethodPost, "/api/serving",
		`{"id":"abc","dir":"`+t.TempDir()+`","port":1234}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	// Named, not summarised: the three ways this fails are fixed in three
	// different places, and the claimant only ever sees this sentence.
	assert.Contains(t, rec.Body.String(), "no ade is recorded at ")
	assert.Contains(t, rec.Body.String(), "ade/server.json")

	for _, body := range []string{
		`{"id":"","dir":"/repo","port":1234}`,
		`{"id":"abc","dir":"relative","port":1234}`,
		`{"id":"abc","dir":"/repo","port":0}`,
	} {
		refused := do(t, s, http.MethodPost, "/api/serving", body)
		assert.Equalf(t, http.StatusBadRequest, refused.Code, "accepted %s", body)
	}

	// And an announcement nothing backs is not resolved either: the record is
	// read again on the way out, so a dead ade falls back to the scan.
	s.served["abc"] = Serving{ID: "abc", Dir: t.TempDir(), Port: 1234}
	_, found := s.announcedServer("abc")
	assert.False(t, found)
}

func TestAnAnnouncementIsBroadcastSoAnOpenPageNeedNotPoll(t *testing.T) {
	s := newTestLauncher(t)

	req := httptest.NewRequest(http.MethodGet, "/api/events?t="+s.token, nil)
	ctx, hangUp := context.WithCancel(req.Context())
	defer hangUp()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.routes().ServeHTTP(httptest.NewRecorder(), req.WithContext(ctx))
	}()
	require.Eventually(t, func() bool { return s.hub.Clients() > 0 }, time.Second, 5*time.Millisecond)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))
	require.NoError(t, session.Write(session.Path(sessions.GitDir(dir)), session.Record{
		PID: os.Getpid(), Root: dir, StartedAt: time.Now(),
		URL: "http://127.0.0.1:4321/?t=x",
	}))
	rec := do(t, s, http.MethodPost, "/api/serving",
		fmt.Sprintf(`{"id":"abc","dir":%q,"port":4321}`, dir))
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, 1, s.hub.Broadcast("serving", nil), "nobody was listening")

	hangUp()
	<-done
}

func TestTheActiveListIsAnnouncedNotScanned(t *testing.T) {
	// The rail asks this. A session in a checkout outside the scan roots is
	// still one you are sitting in, and a dead announcement is not.
	s := newTestLauncher(t)
	s.served["gone"] = Serving{ID: "gone", Dir: t.TempDir(), Port: 1234}

	rec := do(t, s, http.MethodGet, "/api/serving", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var got []ActiveSession
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Empty(t, got, "an announcement nothing serves was listed")
	assert.NotContains(t, s.served, "gone", "a dead announcement was kept")
}

// liveAde stands a checkout up as the real thing does: a record in its git dir
// and a server answering /healthz, so nothing here is faked but the agent.
func liveAde(t *testing.T, health session.Health) (dir string, port int) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "coast")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	health.Ade, health.PID, health.Root = true, os.Getpid(), dir
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(health)
	}))
	t.Cleanup(up.Close)
	require.NoError(t, session.Write(session.Path(filepath.Join(dir, ".git")),
		session.Record{PID: os.Getpid(), URL: up.URL + "/?t=x", Root: dir}))
	return dir, portOf(up.URL)
}

func TestTheActiveListSaysWhatALiveSessionIsDoing(t *testing.T) {
	s := newTestLauncher(t)
	dir, port := liveAde(t, session.Health{
		Branch: "feat/x",
		Agent:  &session.AgentHealth{Running: true, Model: "opus", Session: "abc"},
	})
	s.served["abc"] = Serving{ID: "abc", Dir: dir, Port: port}

	got := s.activeSessions(context.Background())
	require.Len(t, got, 1)
	assert.Equal(t, "abc", got[0].ID)
	assert.Equal(t, dir, got[0].Dir)
	assert.Equal(t, "coast", got[0].Name)
	assert.Equal(t, "coast", got[0].Repo)
	assert.Equal(t, "feat/x", got[0].Branch)
	require.NotNil(t, got[0].Agent)
	assert.True(t, got[0].Agent.Running)
	assert.Equal(t, "opus", got[0].Agent.Model)
}

// The rail addresses a session by id, so the id the agent left behind has to
// go: the ade announced the new one and said nothing about the old.
func TestAnAnnouncementTheAgentHasMovedOnFromIsDropped(t *testing.T) {
	s := newTestLauncher(t)
	dir, port := liveAde(t, session.Health{Agent: &session.AgentHealth{Session: "new"}})
	s.served["old"] = Serving{ID: "old", Dir: dir, Port: port}

	assert.Empty(t, s.activeSessions(context.Background()))
	assert.NotContains(t, s.served, "old")
}

// A probe that did not answer is a busy ade, not a gone one: forgetting it
// takes the session off every rail and the proxy loses its only route.
func TestASweepThatWasCancelledForgetsNothing(t *testing.T) {
	s := newTestLauncher(t)
	dir, port := liveAde(t, session.Health{Agent: &session.AgentHealth{Session: "abc"}})
	s.served["abc"] = Serving{ID: "abc", Dir: dir, Port: port}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := s.activeSessions(ctx)
	assert.Contains(t, s.served, "abc", "a session was dropped because the page went away")
	require.Len(t, got, 1)
	assert.Nil(t, got[0].Agent, "nothing answered, so nothing is claimed about the agent")
}

// The launcher gives its port back when it is left alone, and the next session
// starts it again. Every ade announces once, so what it knew has to survive.
func TestARestartedLauncherStillKnowsWhatWasAnnounced(t *testing.T) {
	// A home of this test's own: the set is kept in it, and the second launcher
	// must read what the first wrote and nothing else.
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	dir, port := liveAde(t, session.Health{})
	rec := do(t, s, http.MethodPost, "/api/serving",
		`{"id":"abc","dir":"`+dir+`","port":`+strconv.Itoa(port)+`}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	next, err := New(0, "test")
	require.NoError(t, err)
	assert.Contains(t, next.served, "abc")
}

func TestTheActiveListNeedsTheToken(t *testing.T) {
	s := newTestLauncher(t)
	rec := do(t, s, http.MethodGet, "/api/serving?t=wrong", "")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// A restart announces a new id for the same checkout. Without dropping the old
// row the rail draws two tiles for one ade, the stale one dimmed for ever.
func TestOneCheckoutIsOneRowHoweverOftenItRestarts(t *testing.T) {
	s := newTestLauncher(t)
	dir, port := liveAde(t, session.Health{Agent: &session.AgentHealth{Session: "second"}})

	for _, id := range []string{"first", "second"} {
		rec := do(t, s, http.MethodPost, "/api/serving",
			`{"id":"`+id+`","dir":"`+dir+`","port":`+strconv.Itoa(port)+`}`)
		require.Equal(t, http.StatusNoContent, rec.Code)
	}

	require.Len(t, s.served, 1, "the checkout is served by one ade")
	assert.Contains(t, s.served, "second")

	got := s.activeSessions(context.Background())
	require.Len(t, got, 1, "one tile on the rail")
	assert.Equal(t, "second", got[0].ID)
}

// The same invariant on the way back from disk: a remembered list written
// before the fix would otherwise put the duplicate back on every restart.
func TestARememberedListIsOneRowPerCheckout(t *testing.T) {
	s := newTestLauncher(t)
	dir, port := liveAde(t, session.Health{Agent: &session.AgentHealth{Session: "second"}})

	rows := []Serving{{ID: "first", Dir: dir, Port: port}, {ID: "second", Dir: dir, Port: port}}
	data, err := json.Marshal(rows)
	require.NoError(t, err)
	path := filepath.Join(s.home, servingFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	s.served = map[string]Serving{}
	s.recall()
	assert.Len(t, s.served, 1)
	assert.Contains(t, s.served, "second")
}

// The shell points a view straight at the session's own port, so it asks for
// the address the proxy would otherwise have replaced.
func TestDirectOpeningSkipsTheProxy(t *testing.T) {
	s := newTestLauncher(t)
	var found listedDir
	for _, dir := range checkoutsOf(listRepos(t, s)) {
		if len(dir.Sessions) > 0 {
			found = dir
			break
		}
	}
	if found.Path == "" {
		t.Skip("no checkout on this machine has a session to open")
	}
	id := found.Sessions[0].ID

	s.setPort(4321)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		return "http://127.0.0.1:1234/?t=itsown", nil
	}
	s.locate = func(_ context.Context, want string) (string, int, bool) {
		return found.Path, 1234, want == id
	}

	req := httptest.NewRequest(http.MethodPost, "/api/open",
		strings.NewReader(`{"dir":"`+found.Path+`","id":"`+id+`","direct":true}`))
	req.Host = "127.0.0.1:4321"
	req.Header.Set("X-Ade-Token", s.token)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct{ URL string }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "http://127.0.0.1:1234/?t=itsown", got.URL)
	assert.NotContains(t, got.URL, "/s/")
}

// A machine nothing is connected to is a bad request, not a silent fall back
// to opening something on this one.
func TestOpeningOnAMachineThatIsNotConnectedIsRefused(t *testing.T) {
	s := newTestLauncher(t)
	s.setPort(4321)
	req := httptest.NewRequest(http.MethodPost, "/api/open",
		strings.NewReader(`{"dir":"/anywhere","id":"","direct":true,"machine":"linux"}`))
	req.Host = "127.0.0.1:4321"
	req.Header.Set("X-Ade-Token", s.token)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "not connected to linux")
}

// A worker's session ports are its container's, so the shell's direct ask
// would name a port nothing outside can reach.
func TestAWorkerProxiesEvenADirectOpen(t *testing.T) {
	t.Setenv("ADE_BIND", "0.0.0.0")
	repo := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", repo).Run())
	repo, err := filepath.EvalSymlinks(repo)
	require.NoError(t, err)

	s := newTestLauncher(t)
	s.setPort(4321)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		return "http://127.0.0.1:1234/?t=direct", nil
	}
	s.served["pending-abc"] = Serving{ID: "pending-abc", Dir: repo, Port: 1234}

	req := httptest.NewRequest(http.MethodPost, "/api/open",
		strings.NewReader(`{"dir":"`+repo+`","direct":true}`))
	req.Host = "127.0.0.1:4321"
	req.Header.Set("X-Ade-Token", s.token)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct{ URL string }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.True(t, strings.HasPrefix(got.URL, "http://127.0.0.1:4321/s/pending-abc/"),
		"absolute, so the shell can parse it: %s", got.URL)
}

func TestAWorkerSessionThatNeverAnnouncesIsABadGatewayAfterTheLongerWait(t *testing.T) {
	t.Setenv("ADE_BIND", "0.0.0.0")
	was := announcedWait
	announcedWait = 20 * time.Millisecond
	t.Cleanup(func() { announcedWait = was })
	repo := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", repo).Run())
	repo, err := filepath.EvalSymlinks(repo)
	require.NoError(t, err)

	s := newTestLauncher(t)
	s.setPort(4321)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		return "http://127.0.0.1:1234/?t=direct", nil
	}

	req := httptest.NewRequest(http.MethodPost, "/api/open",
		strings.NewReader(`{"dir":"`+repo+`","direct":true}`))
	req.Host = "127.0.0.1:4321"
	req.Header.Set("X-Ade-Token", s.token)
	rec := httptest.NewRecorder()
	start := time.Now()
	s.routes().ServeHTTP(rec, req)
	assert.GreaterOrEqual(t, time.Since(start), 10*announcedWait)
	assert.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "never announced itself")
	assert.NotContains(t, rec.Body.String(), "127.0.0.1:1234")
}

// A conversation on another machine is reached through that machine's own
// launcher, which has the same proxy at the same path. Nothing here knows
// which port serves it there, and nothing here needs to.
func TestTheProxyReachesASessionOnAnotherMachine(t *testing.T) {
	var asked *http.Request
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/repos" {
			_ = json.NewEncoder(w).Encode(map[string]any{"repos": []sessions.Repo{{
				Name: "ade", Path: "/far/ade",
				Checkouts: []sessions.Dir{{
					Name: "ade", Path: "/far/ade",
					Sessions: []sessions.Session{{ID: "away", Dir: "/far/ade", Name: "away"}},
				}},
			}}})
			return
		}
		asked = r
		_, _ = w.Write([]byte(`{"from":"the other machine"}`))
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}
	front := httptest.NewServer(s.routes())
	defer front.Close()

	res, err := http.Get(front.URL + "/s/away/api/state?t=" + s.token)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	require.Equal(t, http.StatusOK, res.StatusCode)

	require.NotNil(t, asked, "the far launcher was never asked")
	// Its own proxy path, so it resolves the session at its end.
	assert.Equal(t, "/s/away/api/state", asked.URL.Path)
	assert.Equal(t, "faraway", asked.Header.Get("X-Ade-Token"))
	// The token in the query is rewritten too, or the far end refuses it.
	assert.Equal(t, "faraway", asked.URL.Query().Get("t"))
}

// One nobody has is still a 404: the machines are asked, and if none of
// them holds it there is nothing to forward to.
func TestASessionNoMachineHasIsStillNotFound(t *testing.T) {
	s := newTestLauncher(t)
	front := httptest.NewServer(s.routes())
	defer front.Close()

	res, err := http.Get(front.URL + "/s/nobody/api/state?t=" + s.token)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

// Recorded, running, and not answering: the default proxy writes a bare 502,
// which reaches a person as "/api/chat: 502" and says nothing they can act on.
func TestAnAdeThatDoesNotAnswerSaysSoRatherThanJustFailing(t *testing.T) {
	up := newUpstream(t, func(http.ResponseWriter, *http.Request) {})
	s := newTestLauncher(t)
	dead := up.port(t)
	up.server.Close()

	dir := t.TempDir()
	s.locate = func(_ context.Context, id string) (string, int, bool) {
		return dir, dead, id == "abc"
	}
	front := httptest.NewServer(s.routes())
	t.Cleanup(front.Close)

	res, err := http.Get(front.URL + "/s/abc/api/chat?t=" + s.token)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusBadGateway, res.StatusCode)
	assert.Contains(t, string(body), "no ade is answering for that session")
	assert.Contains(t, string(body), "stop and put it away")
}

// The claim goes with it, so the next request is the 404 the window already
// knows how to end a session from.
func TestAnAdeThatDoesNotAnswerStopsBeingClaimedAsServing(t *testing.T) {
	up := newUpstream(t, func(http.ResponseWriter, *http.Request) {})
	s := newTestLauncher(t)
	dead := up.port(t)
	up.server.Close()

	// A record that still names the dead port, so the claim is believed
	// right up to the dial. That is the state this is about.
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))
	require.NoError(t, session.Write(session.Path(sessions.GitDir(dir)), session.Record{
		PID: os.Getpid(), Root: dir, StartedAt: time.Now(),
		URL: fmt.Sprintf("http://127.0.0.1:%d/?t=upstream-token", dead),
	}))
	s.served = map[string]Serving{"abc": {ID: "abc", Dir: dir, Port: dead}}
	front := httptest.NewServer(s.routes())
	t.Cleanup(front.Close)

	res, err := http.Get(front.URL + "/s/abc/api/chat?t=" + s.token)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	s.mu.Lock()
	_, still := s.served["abc"]
	s.mu.Unlock()
	assert.False(t, still, "the dead claim was dropped")
}
