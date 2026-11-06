package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/agentbus"
	"github.com/owenrumney/ade/internal/claudelog"
	"github.com/owenrumney/ade/internal/term"
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
	t.Setenv("HOME", t.TempDir())
	spoke(t, "sess-1")
	a := &agentRunner{provider: "claude", model: "opus", current: "sess-1"}

	argv, err := a.takeoverArgv("", false)
	if err != nil {
		t.Skip("claude is not on this machine's PATH")
	}
	assert.Equal(t, "claude", argv[0])
	assert.Contains(t, argv, "--resume")
	assert.Contains(t, argv, "sess-1")
	assert.Contains(t, argv, "opus")
}

func TestTheTakeoverFallsBackToTheRememberedSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	spoke(t, "remembered")
	a := &agentRunner{provider: "claude", session: "remembered"}

	argv, err := a.takeoverArgv("", false)
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

// No agent CLI on the PATH is said, not started.
func TestATakeoverNeedsTheAgentOnThePath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	a := &agentRunner{provider: "claude", current: "sess-1"}

	_, err := a.takeoverArgv("", false)
	assert.ErrorContains(t, err, "not on the PATH")
}

// A conversation the terminal starts is named up front, so ade knows which
// transcript is its own before the first message is sent.
func TestAFreshClaudeIsToldWhatToCallItsConversation(t *testing.T) {
	fakeAgent(t)
	a := &agentRunner{provider: "claude"}

	argv, err := a.takeoverArgv("", false)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(argv), 3)
	assert.Equal(t, "--session-id", argv[1])
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, argv[2])
	id := argv[2]

	again, err := a.takeoverArgv("", false)
	require.NoError(t, err)
	assert.Equal(t, argv, again, "a pane made twice before it ran is the same conversation")

	later, err := a.takeoverArgv("", true)
	require.NoError(t, err)
	assert.Equal(t, []string{"claude", "--resume", id}, later[:3], "once it has run there is something to resume")
}

// A conversation on record that claude has since dropped is begun again:
// a resume of nothing fails every time.
func TestARecordedConversationClaudeDroppedIsStartedAgain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fakeAgent(t)
	const id = "fce48a23-8b72-498e-ad48-ba115c0b7491"
	a := &agentRunner{provider: "claude", current: id}

	argv, err := a.takeoverArgv("", true)
	require.NoError(t, err)
	assert.Equal(t, []string{"claude", "--session-id", id}, argv[:3])

	spoke(t, id)
	later, err := a.takeoverArgv("", true)
	require.NoError(t, err)
	assert.Equal(t, []string{"claude", "--resume", id}, later[:3])

	old := &agentRunner{provider: "claude", current: "sess-1"}
	argv, err = old.takeoverArgv("", true)
	require.NoError(t, err)
	assert.Equal(t, "--session-id", argv[1])
	assert.True(t, claudelog.IsID(argv[2]), "an id claude would refuse is replaced")
}

// spoke leaves the transcript claude writes once a conversation has a message.
func spoke(t *testing.T, id string) {
	t.Helper()
	path := filepath.Join(os.Getenv("HOME"), ".claude", "projects", "here", id+".jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o600))
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
			fakeAgent(t)
			a := &agentRunner{provider: "codex", current: tc.current}
			argv, err := a.takeoverArgv("", tc.started)
			require.NoError(t, err)
			assert.Equal(t, tc.want, withoutConfig(argv))
		})
	}
}

func TestTheFirstMessageIsTheLastArgument(t *testing.T) {
	fakeAgent(t)
	a := &agentRunner{provider: "codex"}

	argv, err := a.takeoverArgv("  fix the build  ", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"codex", "fix the build"}, withoutConfig(argv))

	argv, err = a.takeoverArgv("--help me", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"codex", "--", "--help me"}, withoutConfig(argv), "a message is never read as a flag")
}

// fakeAgent puts a claude on the PATH that prints its arguments and then
// echoes what is typed at it, which is all a pane test needs of one.
func fakeAgent(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho \"ARGS:$*\"\necho \"BADGE:$ADE_BADGE_URL\"\nexec cat\n"
	for _, cli := range []string{"claude", "codex"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, cli), []byte(script), 0o700))
	}
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

	path := filepath.Join(os.Getenv("HOME"), ".claude", "projects", "here", id+".jsonl")
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

func TestAnAgentOnThisMachineIsToldToReportWhatItIsDoing(t *testing.T) {
	a := &agentRunner{provider: "claude"}
	argv := a.claudeArgv("", false)
	require.Equal(t, "--settings", argv[len(argv)-2])
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal([]byte(argv[len(argv)-1]), &settings))
	for _, event := range badgeEvents {
		require.Len(t, settings.Hooks[event], 1, event)
		assert.Equal(t, "command", settings.Hooks[event][0].Hooks[0].Type)
		assert.Regexp(t, `^'.+' badge$`, settings.Hooks[event][0].Hooks[0].Command)
	}

	codex := codexArgv("thread-1", false, codexNotify())
	require.Len(t, codex, 5)
	assert.Equal(t, "-c", codex[1])
	assert.Regexp(t, `^notify=\[".+","badge"\]$`, codex[2])
	assert.Equal(t, []string{"resume", "thread-1"}, codex[3:], "the override goes before the subcommand")
}

func TestHookReportsBecomeTheBadge(t *testing.T) {
	for name, tc := range map[string]struct {
		said string
		want string
	}{
		"a message sent":           {`{"hook_event_name":"UserPromptSubmit"}`, badgeWorking},
		"a tool finished":          {`{"hook_event_name":"PostToolUse"}`, badgeWorking},
		"a turn finished":          {`{"hook_event_name":"Stop"}`, badgeNeeds},
		"a permission asked for":   {`{"hook_event_name":"PermissionRequest"}`, badgeBlocked},
		"a permission notice":      {`{"hook_event_name":"Notification","notification_type":"permission_prompt"}`, badgeBlocked},
		"an idle notice":           {`{"hook_event_name":"Notification","notification_type":"idle_prompt"}`, ""},
		"a session starting":       {`{"hook_event_name":"SessionStart","session_id":"abc"}`, ""},
		"codex finishing its turn": {`{"type":"agent-turn-complete","thread-id":"0199-abcd"}`, badgeNeeds},
	} {
		t.Run(name, func(t *testing.T) {
			var said hookSaid
			require.NoError(t, json.Unmarshal([]byte(tc.said), &said))
			assert.Equal(t, tc.want, said.badge())
		})
	}
	assert.Equal(t, "0199-abcd", hookSaid{Thread: "0199-abcd"}.session())
	assert.Empty(t, hookSaid{Session: "--dangerously-skip"}.session(), "an id is never a flag")
}

func TestABadgeClearsWhenLookedAtUnlessTheAgentIsBlocked(t *testing.T) {
	a := &agentRunner{}
	assert.Equal(t, badgeIdle, a.badgeAfter(0), "nothing said yet is idle")

	require.True(t, a.setBadge(badgeNeeds))
	assert.False(t, a.setBadge(badgeNeeds), "said twice is not a change")
	assert.True(t, a.seen())
	assert.Equal(t, badgeIdle, a.badgeAfter(0))

	require.True(t, a.setBadge(badgeBlocked))
	assert.False(t, a.seen(), "a prompt still open is still yours")
	assert.Equal(t, badgeNeeds, a.badgeAfter(0))

	require.True(t, a.setBadge(badgeWorking))
	assert.Equal(t, badgeWorking, a.badgeAfter(quietAfter-time.Second))
	assert.Equal(t, badgeIdle, a.badgeAfter(quietAfter+time.Second), "an interrupted turn says nothing, so silence ends it")
}

func TestTheAgentsOwnHooksMoveItsBadge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fakeAgent(t)
	s := newTestServer(t)
	srv := live(t, s)
	s.hookToken = "hook-token"
	s.agent.setHook(srv.URL, s.hookToken)

	pane, err := s.agentPane(t.Context(), "")
	require.NoError(t, err)
	assert.Empty(t, s.agentBadge(), "nothing runs until its pane is opened")
	assert.Contains(t, seen(t, pane, "BADGE:http"), "BADGE:"+srv.URL+BadgePath, "the CLI's hooks know where to report")
	assert.Equal(t, badgeIdle, s.agentBadge())
	assert.True(t, s.agentAlive(), "which is what keeps the server up with no window")

	post := func(path, bearer, body string) int {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("X-Ade-Token", s.token)
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = res.Body.Close() }()
		return res.StatusCode
	}
	assert.Equal(t, http.StatusForbidden, post(BadgePath, "wrong", `{"hook_event_name":"Stop"}`))
	assert.Equal(t, badgeIdle, s.agentBadge())

	assert.Equal(t, http.StatusNoContent,
		post(BadgePath, "hook-token", `{"hook_event_name":"Stop","session_id":"0199aaaa-bbbb"}`))
	assert.Equal(t, badgeNeeds, s.agentBadge())
	assert.Equal(t, "0199aaaa-bbbb", s.agent.Current(), "the CLI's own name for the conversation is what resumes it")

	assert.Equal(t, http.StatusNoContent, post("/api/agent/seen", "", ""))
	assert.Equal(t, badgeIdle, s.agentBadge(), "looked at")

	quit(t, pane)
	assert.Equal(t, badgeEnded, s.agentBadge())
	assert.False(t, s.agentAlive())
}

// A socket coming back is not a person asking: only Resume restarts an agent.
func TestAnEndedAgentIsNotRestartedByItsSocketAlone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fakeAgent(t)
	s := newTestServer(t)
	srv := live(t, s)

	pane, err := s.agentPane(t.Context(), "")
	require.NoError(t, err)
	seen(t, pane, "ARGS:")
	quit(t, pane)

	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(t.Context(), ws+"/api/terminal?pane=agent&t="+url.QueryEscape(s.token), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	_, _, err = conn.Read(t.Context())
	assert.Equal(t, websocket.StatusNormalClosure, websocket.CloseStatus(err))
	assert.Equal(t, badgeEnded, s.agentBadge(), "still ended")

	back, err := s.agentPane(t.Context(), "")
	require.NoError(t, err)
	seen(t, back, "ARGS:")
	assert.Equal(t, badgeIdle, s.agentBadge(), "asked for, it comes back")
}

// A terminal agent holds the conversation for as long as its pane lives, so a
// run that waited for it would wait for good.
func TestAColdRunDoesNotWaitForTheAgentsTerminal(t *testing.T) {
	a := &agentRunner{}
	a.releaseForTakeover()

	for _, kind := range []string{kindReview, kindCommitMessage} {
		_, ok := a.begin(t.Context(), kind)
		require.True(t, ok, kind)
		a.finish(nil)
	}
	assert.True(t, a.TakenOver(), "the terminal still holds the conversation afterwards")
}

func TestATerminalAgentIsHandedTheBusTools(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fakeAgent(t)
	a := &agentRunner{provider: "claude", delegateURL: "http://127.0.0.1:4100/", delegateToken: "tok-1"}

	argv, err := a.takeoverArgv("", false)
	require.NoError(t, err)
	at := slices.Index(argv, "--mcp-config")
	require.Positive(t, at, "claude is given a config file")
	raw, err := os.ReadFile(argv[at+1])
	require.NoError(t, err)
	var config struct {
		Servers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(raw, &config))
	require.Len(t, config.Servers, 1, "the bus and nothing else of ade's")
	assert.Equal(t, "http://127.0.0.1:4100"+BusMCPPath, config.Servers[busServerName].URL)
	assert.Equal(t, "tok-1", config.Servers[busServerName].Headers["X-Ade-Delegate-Token"])

	env := a.busEnv()
	assert.Contains(t, env, busTokenVar+"=tok-1")
	assert.Regexp(t, "^"+agentbus.EnvInstance+"=ade-", env[0])

	codex := codexArgv("", true, (&agentRunner{delegateURL: "http://127.0.0.1:4100", delegateToken: "tok-1"}).codexBus()...)
	assert.Equal(t, []string{
		"codex",
		"-c", `mcp_servers.agentbus.url="http://127.0.0.1:4100/mcp/agentbus"`,
		"-c", `mcp_servers.agentbus.bearer_token_env_var="ADE_AGENTBUS_TOKEN"`,
		"resume", "--last",
	}, codex, "the token is in the environment, not on the command line")

}

func TestAnAnswerFromTheBusIsTypedAtATerminalAgent(t *testing.T) {
	fakeAgent(t)
	s := newTestServer(t)
	t.Cleanup(s.closeTerminal)

	s.answered(t.Context(), "kim answered: before any terminal")
	assert.Nil(t, s.agentTerm(), "an answer alone starts no agent")

	pane, err := s.agentPane(t.Context(), "the first message")
	require.NoError(t, err)
	seen(t, pane, "the first message")

	s.answered(t.Context(), "kim answered \"ship it?\": yes\x03, go\x1b[201~")
	assert.Contains(t, seen(t, pane, "go[201~"), `From agentbus: kim answered "ship it?": yes, go[201~`, "nothing in an answer ends the paste or the agent")
}

func TestATerminalAgentsBadgeIsItsStandingOnTheBus(t *testing.T) {
	for name, tc := range map[string]struct {
		run     RunState
		running bool
		asking  int
	}{
		"a turn of ade's own":   {RunState{Running: true}, true, 0},
		"working in a terminal": {RunState{Terminal: badgeWorking}, true, 0},
		"waiting on its human":  {RunState{Terminal: badgeNeeds}, false, 1},
		"idle":                  {RunState{Terminal: badgeIdle}, false, 0},
		"ended":                 {RunState{Terminal: badgeEnded}, false, 0},
	} {
		t.Run(name, func(t *testing.T) {
			running, asking := busStanding(tc.run)
			assert.Equal(t, tc.running, running)
			assert.Equal(t, tc.asking, asking)
		})
	}
}

// withoutConfig drops codex's -c overrides, which name this test binary.
func withoutConfig(argv []string) []string {
	var out []string
	for i := 0; i < len(argv); i++ {
		if argv[i] == "-c" {
			i++
			continue
		}
		out = append(out, argv[i])
	}
	return out
}

// The rule travels with the binary: no file on the machine carries it, so a
// second machine needs nothing synced to it.
func TestTheTakeoverTellsTheAgentTheTerminalDrawsPictures(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := &agentRunner{provider: "claude"}

	argv, err := a.takeoverArgv("", false)
	if err != nil {
		t.Skip("claude is not on this machine's PATH")
	}
	at := slices.Index(argv, "--append-system-prompt")
	require.GreaterOrEqual(t, at, 0, argv)
	require.Less(t, at+1, len(argv))
	assert.Contains(t, argv[at+1], "ade show <file>")
}

// Codex takes no such flag, so it is told nothing rather than handed one.
func TestCodexIsNotGivenAFlagItDoesNotTake(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := &agentRunner{provider: "codex"}

	argv, err := a.takeoverArgv("", false)
	if err != nil {
		t.Skip("codex is not on this machine's PATH")
	}
	assert.NotContains(t, argv, "--append-system-prompt")
}
