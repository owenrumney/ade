package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/sandbox"
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

	argv, _, _, err := a.takeoverArgv()
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

	argv, _, _, err := a.takeoverArgv()
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

	argv, _, boxed, err := a.takeoverArgv()
	require.NoError(t, err)
	assert.True(t, boxed)
	assert.Equal(t, []string{"claude", "--resume", "sess-1"}, argv)

	a.sandbox = false
	_, _, _, err = a.takeoverArgv()
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

	argv, err := a.takeoverCommand(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"/no/such/runtime", "run", sandbox.TUIContainerName(dir)}, argv)
	assert.True(t, seen.TTY)
	assert.Contains(t, seen.Args, "sess-1")
}
