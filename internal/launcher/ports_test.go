package launcher

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/browse"
	"github.com/owenrumney/ade/internal/httpx"
	"github.com/owenrumney/ade/internal/remote"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
)

// fixture builds a home with its own settings and checkouts, so a test can say
// what the scan finds instead of taking whatever is on the developer's disk.
type fixture struct {
	home  string
	code  string
	alive map[string]bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	code := filepath.Join(home, "code")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".ade"), 0o700))
	require.NoError(t, os.MkdirAll(code, 0o700))
	settings := fmt.Sprintf(`{"scan_dirs":[%q],"hosts":[{"name":"pc","label":"under the desk"}]}`, code)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".ade", "settings.json"), []byte(settings), 0o600))

	return &fixture{home: home, code: code, alive: map[string]bool{}}
}

// checkout adds a repository. A port of zero leaves it with no server record
// at all, which is the ordinary case of somewhere you could start one.
func (m *fixture) checkout(t *testing.T, name string, port int, live bool) string {
	t.Helper()
	dir := filepath.Join(m.code, name)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))
	if port == 0 {
		return dir
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/?t=tok", port)
	require.NoError(t, session.Write(session.Path(sessions.GitDir(dir)), session.Record{
		PID: os.Getpid(), URL: url, Root: dir, StartedAt: time.Now(),
	}))
	m.alive[url] = live
	return dir
}

// prober answers from what the fixture said, so nothing is probed for real.
func (m *fixture) prober() sessions.Prober {
	return func(_ context.Context, rec *session.Record) (session.Health, bool) {
		if !m.alive[rec.URL] {
			return session.Health{}, false
		}
		return session.Health{Ade: true, Root: rec.Root, PID: rec.PID}, true
	}
}

func newLauncherOn(t *testing.T, m *fixture) *Server {
	t.Helper()
	s, err := New(0, "test")
	require.NoError(t, err)
	s.probe = m.prober()
	return s
}

func TestPortsAreTheServersThatAnswer(t *testing.T) {
	m := newFixture(t)
	m.checkout(t, "live", 27431, true)
	m.checkout(t, "stale", 25000, false)
	m.checkout(t, "never-opened", 0, false)

	s := newLauncherOn(t, m)
	s.setPort(29233)

	ports := s.ports(context.Background())

	assert.Contains(t, ports, 29233, "the launcher's own port is what the tunnel comes in on")
	assert.Contains(t, ports, 27431)
	assert.NotContains(t, ports, 25000, "a record whose server is gone is not a port to forward")
	assert.Len(t, ports, 2)
	assert.Equal(t, []int{27431, 29233}, ports, "sorted, so an unchanged set encodes the same twice")
}

func TestPortsComeOffTheRecordNotTheHash(t *testing.T) {
	// httpx.Listen falls back to any free port when its stable one is taken,
	// so what is forwarded has to be where the server actually is.
	m := newFixture(t)
	dir := m.checkout(t, "moved", 41234, true)

	s := newLauncherOn(t, m)
	assert.Equal(t, []int{41234}, s.ports(context.Background()), "recomputing the port for %s would miss it", dir)
}

func TestThePortStreamOpensWithTheWholeSet(t *testing.T) {
	m := newFixture(t)
	m.checkout(t, "live", 27431, true)
	s := newLauncherOn(t, m)

	srv := httptest.NewServer(s.routes())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/ports?t="+s.token, nil)
	require.NoError(t, err)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()

	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "text/event-stream", res.Header.Get("Content-Type"))

	var frame struct {
		Ports []int `json:"ports"`
	}
	require.NoError(t, json.Unmarshal(firstFrame(t, res.Body), &frame))
	assert.Contains(t, frame.Ports, 27431)
}

func TestOpeningACheckoutPublishesBeforeItAnswers(t *testing.T) {
	// A ade watching from another fixture forwards the new port off this
	// stream, and has to have it up before the page navigates. The page cannot
	// navigate until it has read the response, so the wake must come first.
	m := newFixture(t)
	dir := m.checkout(t, "repo", 0, false)

	s := newLauncherOn(t, m)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		return "http://127.0.0.1:27431/?t=tok", nil
	}

	wake, unsubscribe := s.subscribePorts()
	defer unsubscribe()

	spy := &orderingWriter{ResponseRecorder: httptest.NewRecorder(), wake: wake}
	req := httptest.NewRequest(http.MethodPost, "/api/open",
		jsonBody(t, map[string]string{"dir": dir, "id": ""}))
	req.Header.Set("X-Ade-Token", s.token)
	s.routes().ServeHTTP(spy, req)

	require.Equal(t, http.StatusOK, spy.Code)
	assert.True(t, spy.notifiedFirst, "the stream heard about the port only after the page did")
}

func TestOpeningACodexSessionKeepsItsAgent(t *testing.T) {
	m := newFixture(t)
	dir := m.checkout(t, "repo", 0, false)
	sessionDir := filepath.Join(m.home, ".codex", "sessions", "2026", "08", "03")
	require.NoError(t, os.MkdirAll(sessionDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "rollout-2026-08-03T08-00-00-codex-session.jsonl"),
		[]byte(`{"timestamp":"2026-08-03T08:00:00Z","type":"session_meta","payload":{"id":"codex-session","cwd":"`+dir+`"}}`+"\n"), 0o600))

	s := newLauncherOn(t, m)
	var agent, sessionID string
	s.open = func(_ context.Context, _, gotAgent, _, gotSession string) (string, error) {
		agent, sessionID = gotAgent, gotSession
		return "http://127.0.0.1:27431/?t=tok", nil
	}

	rec := do(t, s, "POST", "/api/open", `{"dir":"`+dir+`","id":"codex-session"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "codex", agent)
	assert.Equal(t, "codex-session", sessionID)
}

// A pi thread id is not a Codex thread id, so pi sessions are history the
// launcher shows and opens the checkout on, without resuming them.
func TestAPiSessionIsReadButNotResumed(t *testing.T) {
	m := newFixture(t)
	dir := m.checkout(t, "repo", 0, false)
	sessionDir := filepath.Join(m.home, ".pi", "agent", "sessions", "repo")
	require.NoError(t, os.MkdirAll(sessionDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "pi.jsonl"),
		[]byte(`{"type":"session","id":"pi-session","cwd":"`+dir+`"}`+"\n"), 0o600))

	s := newLauncherOn(t, m)
	var agent, sessionID string
	s.open = func(_ context.Context, _, gotAgent, _, gotSession string) (string, error) {
		agent, sessionID = gotAgent, gotSession
		return "http://127.0.0.1:27431/?t=tok", nil
	}

	rec := do(t, s, "POST", "/api/open", `{"dir":"`+dir+`","id":"pi-session"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, sessionID, "a pi thread cannot be resumed by the codex driver")
	assert.Empty(t, agent, "with nothing to resume the checkout opens on the CLI's default")
}

func TestAnIdleLauncherStaysUpForAConnectedHost(t *testing.T) {
	s := newTestLauncher(t)
	s.idle, s.every = time.Millisecond, 5*time.Millisecond
	s.mu.Lock()
	s.used = time.Now().Add(-time.Hour)
	s.hosts["pc"] = &conn{stop: func() {}}
	s.mu.Unlock()

	// The browser is off on the far side's launcher, so nothing touches this
	// one. Giving the port back now would take the tunnel with it.
	assert.False(t, idles(t, s), "shut down while a host was connected")

	s.mu.Lock()
	delete(s.hosts, "pc")
	s.mu.Unlock()
	assert.True(t, idles(t, s), "stayed up with nothing connected")
}

// The dashboard holds an SSE stream open. guard touches once, when the request
// starts, so a page someone is watching looks exactly like an empty fixture
// unless the stream itself counts.
func TestAnIdleLauncherStaysUpForAnOpenPage(t *testing.T) {
	s := newTestLauncher(t)
	s.idle, s.every = time.Millisecond, 5*time.Millisecond
	s.mu.Lock()
	s.used = time.Now().Add(-time.Hour)
	s.mu.Unlock()

	// The stream is closed by hand, after the check, rather than on a timer:
	// idles waits two seconds to be sure, and a stream that ended first would
	// be indistinguishable from the bug.
	req := httptest.NewRequest(http.MethodGet, "/api/events?t="+s.token, nil)
	ctx, hangUp := context.WithCancel(req.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.routes().ServeHTTP(httptest.NewRecorder(), req.WithContext(ctx))
	}()

	require.Eventually(t, func() bool { return s.hub.Clients() > 0 }, time.Second, 5*time.Millisecond)
	assert.False(t, idles(t, s), "shut down while a page was watching")

	hangUp()
	<-done
	require.Eventually(t, func() bool { return s.hub.Clients() == 0 }, time.Second, 5*time.Millisecond)
	assert.True(t, idles(t, s), "went when the last page did")
}

// idles runs one round of the idle check and reports whether it stopped.
func idles(t *testing.T, s *Server) bool {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stopped := make(chan struct{})
	go s.watchIdle(ctx, func() { close(stopped) })
	select {
	case <-stopped:
		return true
	case <-time.After(2 * time.Second):
		return false
	}
}

// firstFrame reads up to the first SSE data line and returns its payload.
func firstFrame(t *testing.T, body io.Reader) []byte {
	t.Helper()
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		if data, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
			return []byte(data)
		}
	}
	t.Fatal("the stream said nothing")
	return nil
}

func jsonBody(t *testing.T, body any) io.Reader {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	return bytes.NewReader(encoded)
}

// orderingWriter notes whether the ports stream had already been woken by the
// time the first byte of the response went out.
type orderingWriter struct {
	*httptest.ResponseRecorder
	wake          <-chan struct{}
	notifiedFirst bool
}

func (w *orderingWriter) Write(b []byte) (int, error) {
	select {
	case <-w.wake:
		w.notifiedFirst = true
	default:
	}
	return w.ResponseRecorder.Write(b)
}

func TestBrowsingIsDirectoriesOnly(t *testing.T) {
	m := newFixture(t)
	m.checkout(t, "repo", 0, false)
	require.NoError(t, os.WriteFile(filepath.Join(m.code, "a-file.txt"), []byte("x"), 0o600))
	s := newLauncherOn(t, m)

	rec := do(t, s, "GET", "/api/browse?path="+url.QueryEscape(m.code), "")
	require.Equal(t, http.StatusOK, rec.Code)

	var view browse.View
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	require.Len(t, view.Dirs, 1, "a file is not somewhere to open")
	assert.Equal(t, "repo", view.Dirs[0].Name)
	assert.True(t, view.Dirs[0].Repo, "a checkout says so, which is what the picker highlights")
}

func TestBrowsingNeedsTheTokenAndAnAbsolutePath(t *testing.T) {
	s := newTestLauncher(t)

	req := httptest.NewRequest(http.MethodGet, "/api/browse?path=/tmp", nil)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	assert.Equal(t, http.StatusBadRequest, do(t, s, "GET", "/api/browse?path=code", "").Code)
}

func TestAPickedDirectoryOpensIfItIsReallyACheckout(t *testing.T) {
	// The picker reaches outside the scan roots on purpose, so this cannot be
	// limited to what the scan found. It is still limited to a repository.
	m := newFixture(t)
	outside := filepath.Join(m.home, "elsewhere", "thing")
	require.NoError(t, os.MkdirAll(filepath.Join(outside, ".git"), 0o700))

	s := newLauncherOn(t, m)
	opened := ""
	s.open = func(_ context.Context, dir, _, _, _ string) (string, error) {
		opened = dir
		return "http://127.0.0.1:27431/?t=tok", nil
	}

	rec := do(t, s, "POST", "/api/open", `{"dir":"`+outside+`","id":""}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, outside, opened)

	// A directory that is not a checkout still is not somewhere to start one,
	// and neither is a relative path.
	for _, body := range []string{
		`{"dir":"` + filepath.Join(m.home, "elsewhere") + `"}`,
		`{"dir":"code/repo"}`,
		`{"dir":"/etc"}`,
	} {
		refused := do(t, s, "POST", "/api/open", body)
		assert.Equalf(t, http.StatusBadRequest, refused.Code, "accepted %s", body)
	}
}

func TestAPickedDirectoryCannotNameASession(t *testing.T) {
	// Sessions are matched against the ones listed under a checkout, and a
	// directory the scan never offered has none to match.
	m := newFixture(t)
	outside := filepath.Join(m.home, "elsewhere", "thing")
	require.NoError(t, os.MkdirAll(filepath.Join(outside, ".git"), 0o700))

	s := newLauncherOn(t, m)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		return "http://127.0.0.1:27431/?t=tok", nil
	}

	rec := do(t, s, "POST", "/api/open", `{"dir":"`+outside+`","id":"some-session"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTheRemoteLauncherIsNotIdentityMapped(t *testing.T) {
	// Every launcher hashes "launcher" to the same port, so the one here is
	// always holding the one over there. It is also the only forward whose URL
	// this side hands out, so it is the only one that can be moved.
	launcher := remote.Mapping{Local: 31000, Remote: 29233}

	got := mappings([]int{29233, 27431}, launcher)

	assert.Equal(t, []remote.Mapping{launcher, remote.Same(27431)}, got)
	assert.NotContains(t, got, remote.Same(29233), "the launcher must not be claimed twice")
}

func TestTheLauncherForwardSurvivesAFrameThatOmitsIt(t *testing.T) {
	// Dropping it would take away the tunnel the frame that said so arrived on.
	launcher := remote.Mapping{Local: 31000, Remote: 29233}
	assert.Equal(t, []remote.Mapping{launcher}, mappings(nil, launcher))
}

func TestALocalPortForARemoteLauncherIsStablePerHost(t *testing.T) {
	assert.Equal(t, localLauncherPort("pc"), localLauncherPort("pc"))
	assert.NotEqual(t, localLauncherPort("pc"), localLauncherPort("other"))
	// Never the port a launcher of our own would take.
	assert.NotEqual(t, httpx.StablePort("launcher"), localLauncherPort("pc"))
}

func TestTheUrlIsRewrittenToThePortItAnswersOnHere(t *testing.T) {
	assert.Equal(t,
		"http://127.0.0.1:31000/?t=abc",
		reachableAt("http://127.0.0.1:29233/?t=abc", 31000),
		"the token has to survive, or the page cannot ask that launcher anything")
}

func TestANamedPortIsTakenInsteadOfAdesOwn(t *testing.T) {
	// make shots runs a second launcher beside the real one, which is holding
	// the stable port and should not have to be closed for a screenshot.
	held, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	want := held.Addr().(*net.TCPAddr).Port
	require.NoError(t, held.Close())

	s := &Server{wantPort: want}
	ln, base, err := s.listen()
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	assert.Equal(t, want, ln.Addr().(*net.TCPAddr).Port)
	assert.Contains(t, base, strconv.Itoa(want))
	assert.NotEqual(t, httpx.StablePort("launcher"), want)
}

func TestANamedPortAlreadyTakenIsAnError(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = held.Close() }()

	s := &Server{wantPort: held.Addr().(*net.TCPAddr).Port}
	_, _, err = s.listen()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "taken by something else")
}

// A dev server that stopped must stop being forwarded: the set is reconciled
// each frame, so a stale entry is a forward to nothing.
func TestAnExposedPortThatStoppedServingIsDropped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := New(0, "test")
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	live := ln.Addr().(*net.TCPAddr).Port

	dead, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	deadPort := dead.Addr().(*net.TCPAddr).Port
	require.NoError(t, dead.Close())

	s.mu.Lock()
	s.exposed[live] = true
	s.exposed[deadPort] = true
	s.mu.Unlock()

	s.dropDeadExposed(context.Background())

	s.mu.Lock()
	defer s.mu.Unlock()
	assert.True(t, s.exposed[live], "something is still serving here")
	assert.False(t, s.exposed[deadPort], "nothing answers, so it is not forwarded")
	_ = ln.Close()
}
