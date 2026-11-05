package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/term"
	"github.com/owenrumney/ade/internal/transcripts"
)

// ade drives a pipe, not a TUI, so the agent's own interface cannot be a view
// onto the running process. It replaces it, and only one may hold the
// transcript at a time.
func TestATakenOverConversationRefusesATurnHere(t *testing.T) {
	a := &agentRunner{}
	a.takenOver = true

	_, ok := a.begin(t.Context(), "chat")
	assert.False(t, ok, "a turn would be a second process on the transcript")
	assert.True(t, a.TakenOver())
	assert.True(t, a.State().TakenOver, "the page is told why it cannot type")
}

func TestGivingTheConversationBackLetsTurnsRunAgain(t *testing.T) {
	a := &agentRunner{}
	a.takenOver = true
	a.endTakeover()

	ctx, ok := a.begin(t.Context(), "chat")
	require.True(t, ok)
	require.NotNil(t, ctx)
	assert.False(t, a.State().TakenOver)
}

// The TUI opens on the conversation ade was in, not beside it.
func TestTheTakeoverResumesWhatAdeWasIn(t *testing.T) {
	a := &agentRunner{provider: "claude", model: "opus", current: "sess-1"}

	argv, _, _, err := a.takeoverArgv("", false)
	if err != nil {
		t.Skip("claude is not on this machine's PATH")
	}
	assert.Equal(t, "claude", argv[0])
	assert.Contains(t, argv, "--resume")
	assert.Contains(t, argv, "sess-1")
	assert.Contains(t, argv, "opus")
}

func TestTheTakeoverFallsBackToTheRememberedSession(t *testing.T) {
	a := &agentRunner{provider: "claude", session: "remembered"}

	argv, _, _, err := a.takeoverArgv("", false)
	if err != nil {
		t.Skip("claude is not on this machine's PATH")
	}
	assert.Contains(t, argv, "remembered")
}

func TestATurnInFlightIsNotInterrupted(t *testing.T) {
	s := &Server{agent: &agentRunner{}}
	s.agent.running = true

	rec := httptest.NewRecorder()
	s.handleAgentTerminal(rec, httptest.NewRequest(http.MethodPost, "/api/agent/terminal", nil))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "mid-turn")
	assert.False(t, s.agent.TakenOver(), "a refused takeover changes nothing")
}

// A worker has no claude of its own: the sandboxed agent's is in its box.
func TestASandboxedTakeoverNeedsNoAgentOnThePath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	a := &agentRunner{provider: "claude", current: "sess-1", sandbox: true}

	argv, _, boxed, err := a.takeoverArgv("", false)
	require.NoError(t, err)
	assert.True(t, boxed)
	assert.Equal(t, []string{"claude", "--resume", "sess-1"}, argv)

	a.sandbox = false
	_, _, _, err = a.takeoverArgv("", false)
	assert.ErrorContains(t, err, "not on the PATH")
}

func TestTheBoxedTakeoverIsATerminalOnTheBoxsServers(t *testing.T) {
	spec := sandbox.Spec{Name: "x", Image: "img", Env: map[string]string{"HOME": "/h"},
		Args: []string{"--mcp-config", "/h/.claude/ade-mcp.json"}}
	box := bridge.BoxedTUI(spec, []string{"claude", "--resume", "s1"}, []string{"ADE_DELEGATE_URL=u"})

	args := strings.Join(sandbox.Docker{Network: "container:ade-hub"}.Args(box), " ")
	assert.Contains(t, args, "run -i -t --rm")
	assert.Contains(t, args, "--network container:ade-hub")
	assert.Contains(t, args, "-e ADE_DELEGATE_URL=u")
	assert.Contains(t, args, "claude --resume s1 --mcp-config /h/.claude/ade-mcp.json --strict-mcp-config")
	assert.NotContains(t, strings.Join(sandbox.Docker{}.Args(spec), " "), " -t ", "the agent's stream stays a pipe")
}

type seenRuntime struct{ spec *sandbox.Spec }

func (seenRuntime) Name() string            { return "seen" }
func (seenRuntime) Bin() string             { return "/no/such/runtime" }
func (seenRuntime) HasArgs(string) []string { return []string{"true"} }
func (r seenRuntime) Args(spec sandbox.Spec) []string {
	*r.spec = spec
	return []string{"run", spec.Name}
}

func TestASandboxedTakeoverRunsTheTUIInItsOwnBox(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	was := sandboxLogin
	sandboxLogin = func(_ context.Context, home string) (string, error) { return filepath.Join(home, "creds.json"), nil }
	t.Cleanup(func() { sandboxLogin = was })
	dir := t.TempDir()
	seen := &sandbox.Spec{}
	a := &agentRunner{provider: "claude", current: "sess-1", sandbox: true, dir: dir, runtime: seenRuntime{seen}}

	argv, err := a.takeoverCommand(t.Context(), "", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"/no/such/runtime", "run", sandbox.TUIContainerName(dir)}, argv)
	assert.True(t, seen.TTY)
	assert.Contains(t, seen.Args, "sess-1")
}

// A conversation the terminal starts is named up front, so ade knows which
// transcript is its own before the first message is sent.
func TestAFreshClaudeIsToldWhatToCallItsConversation(t *testing.T) {
	a := &agentRunner{provider: "claude", sandbox: true}

	argv, _, _, err := a.takeoverArgv("", false)
	require.NoError(t, err)
	require.Len(t, argv, 3)
	assert.Equal(t, "--session-id", argv[1])
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, argv[2])
	id := argv[2]

	again, _, _, err := a.takeoverArgv("", false)
	require.NoError(t, err)
	assert.Equal(t, argv, again, "a pane made twice before it ran is the same conversation")

	later, _, _, err := a.takeoverArgv("", true)
	require.NoError(t, err)
	assert.Equal(t, []string{"claude", "--resume", id}, later, "once it has run there is something to resume")
}

func TestCodexResumesByItsOwnSubcommand(t *testing.T) {
	for name, tc := range map[string]struct {
		current string
		started bool
		want    []string
	}{
		"a new one":                 {"", false, []string{"codex"}},
		"one ade was in":            {"thread-1", false, []string{"codex", "resume", "thread-1"}},
		"one started here, unnamed": {"", true, []string{"codex", "resume", "--last"}},
	} {
		t.Run(name, func(t *testing.T) {
			a := &agentRunner{provider: "codex", current: tc.current, sandbox: true}
			argv, _, _, err := a.takeoverArgv("", tc.started)
			require.NoError(t, err)
			assert.Equal(t, tc.want, argv)
		})
	}
}

func TestTheFirstMessageIsTheLastArgument(t *testing.T) {
	a := &agentRunner{provider: "codex", sandbox: true}

	argv, _, _, err := a.takeoverArgv("  fix the build  ", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"codex", "fix the build"}, argv)

	argv, _, _, err = a.takeoverArgv("--help me", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"codex", "--", "--help me"}, argv, "a message is never read as a flag")
}

// fakeAgent puts a claude on the PATH that prints its arguments and then
// echoes what is typed at it, which is all a pane test needs of one.
func fakeAgent(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho \"ARGS:$*\"\nexec cat\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/bin:/usr/bin")
}

// seen reads a pane until want shows up in it.
func seen(t *testing.T, sess *term.Session, want string) string {
	t.Helper()
	_, replay, out, err := sess.Attach()
	require.NoError(t, err)
	var got strings.Builder
	got.Write(replay)
	deadline := time.After(10 * time.Second)
	for !strings.Contains(got.String(), want) {
		select {
		case chunk, ok := <-out:
			require.True(t, ok, "the pane closed before %q, saw %q", want, got.String())
			got.Write(chunk)
		case <-deadline:
			t.Fatalf("waited for %q, saw %q", want, got.String())
		}
	}
	return got.String()
}

func TestTheAgentPaneStartsOnceAndIsTypedIntoAfterThat(t *testing.T) {
	fakeAgent(t)
	s := newTestServer(t)
	t.Cleanup(s.closeTerminal)

	first, err := s.agentPane(t.Context(), "the first message")
	require.NoError(t, err)
	assert.True(t, s.agent.TakenOver(), "ade's own agent steps aside")
	assert.Contains(t, seen(t, first, "the first message"), "ARGS:--session-id")

	second, err := s.agentPane(t.Context(), "typed \x1bafterwards")
	require.NoError(t, err)
	assert.Same(t, first, second, "one process on a transcript")
	assert.Contains(t, seen(t, second, "typed afterwards"), "typed afterwards", "an escape cannot end the paste early")
}

// The window only opens a socket on the pane called agent. That alone has to
// start the agent: a login shell under that name would look like a dead one.
func TestOpeningTheAgentPaneStartsTheAgent(t *testing.T) {
	fakeAgent(t)
	s := newTestServer(t)
	srv := live(t, s)

	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(t.Context(), ws+"/api/terminal?pane=agent&t="+url.QueryEscape(s.token), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })

	assert.Contains(t, awaitOutput(t, conn, "ARGS:"), "--session-id")
	assert.True(t, s.agent.TakenOver())
}

// quit ends the fake as leaving the TUI ends the real one.
func quit(t *testing.T, sess *term.Session) {
	t.Helper()
	require.NoError(t, sess.Write([]byte{4}))
	require.Eventually(t, func() bool { return !sess.Alive() }, 10*time.Second, 20*time.Millisecond)
}

func TestAnAgentPaneThatExitedComesBackOnItsConversation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fakeAgent(t)
	s := newTestServer(t)
	t.Cleanup(s.closeTerminal)

	first, err := s.agentPane(t.Context(), "")
	require.NoError(t, err)
	seen(t, first, "ARGS:--session-id")
	id := s.agent.Current()
	require.NotEmpty(t, id, "the conversation is ade's from the moment it is named")
	quit(t, first)

	// Quit before a word was said, there is no transcript, and a resume of
	// nothing fails every time. The same conversation is started instead.
	again, err := s.agentPane(t.Context(), "")
	require.NoError(t, err)
	assert.Contains(t, seen(t, again, "ARGS:"), "--session-id "+id)
	quit(t, again)

	path := transcripts.ClaudeTranscript(s.agent.dir, id)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o600))

	back, err := s.agentPane(t.Context(), "")
	require.NoError(t, err)
	assert.NotSame(t, first, back)
	assert.Contains(t, seen(t, back, "ARGS:"), "--resume "+id)
}

// Picking another conversation in the checkout used to move ade onto it and
// leave the terminal running the old one. A running agent is left alone.
func TestARunningAgentTerminalIsNotMovedOffItsConversation(t *testing.T) {
	fakeAgent(t)
	s := newTestServer(t)
	t.Cleanup(s.closeTerminal)

	pane, err := s.agentPane(t.Context(), "")
	require.NoError(t, err)
	seen(t, pane, "ARGS:")

	rec := httptest.NewRecorder()
	assert.False(t, s.yieldAgentPane(rec))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.True(t, pane.Alive(), "an agent does not die because another row was picked")

	quit(t, pane)
	assert.True(t, s.yieldAgentPane(httptest.NewRecorder()), "one that has exited is only in the way")
	assert.False(t, s.agent.TakenOver())
}
