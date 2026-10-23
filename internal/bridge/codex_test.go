package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/sandbox"
)

func TestCodexArgsAlwaysCheckTheConfig(t *testing.T) {
	assert.Equal(t, []string{"app-server", "--strict-config"}, codexArgs(CodexOpts{}))
	assert.Equal(t,
		[]string{"app-server", "--strict-config", "-c", "mcp_servers={}"},
		codexArgs(CodexOpts{NoMCP: true}),
	)
	// read-only is a sandbox mode, not a tool allowlist: a reviewer that keeps
	// web search and every MCP server can still take the checkout off the box.
	assert.Equal(t,
		[]string{"app-server", "--strict-config", "-c", "tools.web_search=false", "-c", "mcp_servers={}"},
		codexArgs(CodexOpts{ReadOnly: true}),
	)
}

func TestCodexSandboxModeDecidesWhoOwnsTheBoundary(t *testing.T) {
	assert.Equal(t, "workspace-write", codexSandboxMode(CodexOpts{}))
	assert.Equal(t, "read-only", codexSandboxMode(CodexOpts{ReadOnly: true}))
	// ADE's container is the boundary; Codex's own would block writes to the
	// checkout ADE mounted so the turn could make them.
	assert.Equal(t, "danger-full-access", codexSandboxMode(CodexOpts{Sandbox: &sandbox.Spec{}}))
}

func TestCodexModelDropsThePiPrefix(t *testing.T) {
	assert.Equal(t, "gpt-5.6-terra", codexModel("openai-codex/gpt-5.6-terra"))
	assert.Equal(t, "gpt-5.6-terra", codexModel(" gpt-5.6-terra "))
	assert.Empty(t, codexModel(""))
}

func TestCodexInputPutsImagesAheadOfTheText(t *testing.T) {
	got := codexInput("look", []Image{{MediaType: "image/png", Data: "AAAA"}})
	require.Len(t, got, 2)
	assert.Equal(t, "image", got[0]["type"])
	assert.Equal(t, "data:image/png;base64,AAAA", got[0]["url"])
	assert.Equal(t, map[string]any{"type": "text", "text": "look"}, got[1])
}

func TestOtelEndpointOverride(t *testing.T) {
	for _, test := range []struct {
		name   string
		config string
		want   string
	}{
		{
			name:   "inline table under otel",
			config: "[otel]\nexporter = { otlp-grpc = { endpoint = \"http://localhost:4317\" } }\n",
			want:   `otel.exporter.otlp-grpc.endpoint="http://host.docker.internal:4317"`,
		},
		{
			name:   "its own table",
			config: "[otel.exporter.otlp-http]\nendpoint = \"http://127.0.0.1:4318/v1/traces\"\nprotocol = \"binary\"\n",
			want:   `otel.exporter.otlp-http.endpoint="http://host.docker.internal:4318/v1/traces"`,
		},
		{
			name:   "a comment after the value is not part of it",
			config: "[otel.exporter.otlp-http]\nendpoint = \"http://127.0.0.1:4318/v1/traces\" # local\n",
			want:   `otel.exporter.otlp-http.endpoint="http://host.docker.internal:4318/v1/traces"`,
		},
		{
			name:   "a collector that is already off the host is left alone",
			config: "[otel]\nexporter = { otlp-grpc = { endpoint = \"https://otlp.example.com:4317\" } }\n",
		},
		{name: "no otel section", config: "model = \"gpt-5.6-terra\"\n"},
		{name: "exporter off", config: "[otel]\nexporter = \"none\"\n"},
		{name: "nothing at all"},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, otelEndpointOverride([]byte(test.config), "host.docker.internal"))
		})
	}
}

// Without a host name there is no container to reach out of, and rewriting the
// user's endpoint would send their telemetry somewhere they did not ask for.
func TestOtelEndpointIsLeftAloneWithoutAHost(t *testing.T) {
	config := []byte("[otel]\nexporter = { otlp-grpc = { endpoint = \"http://localhost:4317\" } }\n")
	assert.Empty(t, otelEndpointOverride(config, ""))
}

func TestDecodeCodexEvent(t *testing.T) {
	t.Run("agent message", func(t *testing.T) {
		ev, ok := decodeCodexEvent("item/completed", json.RawMessage(
			`{"item":{"id":"item-1","type":"agentMessage","text":"done"}}`))
		require.True(t, ok)
		assert.Equal(t, "assistant", ev.Type)
		assert.Equal(t, "done", ev.Text)
	})

	t.Run("a delta is not a row", func(t *testing.T) {
		_, ok := decodeCodexEvent("item/agentMessage/delta", json.RawMessage(
			`{"itemId":"item-1","delta":"do"}`))
		assert.False(t, ok, "the chat appends a row per event; a delta would be a row per token")
	})

	t.Run("command execution", func(t *testing.T) {
		started, ok := decodeCodexEvent("item/started", json.RawMessage(
			`{"item":{"id":"call-1","type":"commandExecution","command":"pwd","status":"inProgress"}}`))
		require.True(t, ok)
		require.Len(t, started.ToolUses, 1)
		assert.Equal(t, "Bash", started.ToolUses[0].Name)
		assert.JSONEq(t, `{"command":"pwd"}`, string(started.ToolUses[0].Input))

		done, ok := decodeCodexEvent("item/completed", json.RawMessage(
			`{"item":{"id":"call-1","type":"commandExecution","command":"pwd","aggregatedOutput":"/repo","exitCode":0,"status":"completed"}}`))
		require.True(t, ok)
		require.Len(t, done.ToolResults, 1)
		assert.Equal(t, "/repo", done.ToolResults[0].Content)
		assert.False(t, done.ToolResults[0].IsError)
	})

	t.Run("a non-zero exit is a failed tool result", func(t *testing.T) {
		ev, ok := decodeCodexEvent("item/completed", json.RawMessage(
			`{"item":{"id":"call-2","type":"commandExecution","command":"false","aggregatedOutput":"","exitCode":1,"status":"completed"}}`))
		require.True(t, ok)
		require.Len(t, ev.ToolResults, 1)
		assert.True(t, ev.ToolResults[0].IsError)
	})

	t.Run("usage", func(t *testing.T) {
		ev, ok := decodeCodexEvent("thread/tokenUsage/updated", json.RawMessage(
			`{"threadId":"t","turnId":"u","tokenUsage":{"last":{"inputTokens":1000,"cachedInputTokens":600,"cacheWriteInputTokens":40,"outputTokens":50,"reasoningOutputTokens":10,"totalTokens":1050},"total":{"inputTokens":1,"cachedInputTokens":0,"outputTokens":0,"reasoningOutputTokens":0,"totalTokens":1}}}`))
		require.True(t, ok)
		require.NotNil(t, ev.Usage)
		// Codex counts cached tokens inside input; Context must not double them.
		assert.Equal(t, 1040, ev.Usage.Context())
		assert.Equal(t, 50, ev.Usage.Output)
	})

	t.Run("a failed turn is the turn's error, not its prose", func(t *testing.T) {
		ev, ok := decodeCodexEvent("turn/completed", json.RawMessage(
			`{"threadId":"t","turn":{"id":"u","items":[],"status":"failed","error":{"message":"usage limit reached"}}}`))
		require.True(t, ok)
		assert.True(t, ev.IsError)
		assert.Empty(t, ev.Text, "an error must not land in the transcript as something the agent said")
		assert.Equal(t, "usage limit reached", ev.Result)
	})

	t.Run("a completed turn is the result", func(t *testing.T) {
		ev, ok := decodeCodexEvent("turn/completed", json.RawMessage(
			`{"threadId":"t","turn":{"id":"u","items":[],"status":"completed"}}`))
		require.True(t, ok)
		assert.Equal(t, "result", ev.Type)
		assert.False(t, ev.IsError)
	})

	t.Run("an error it will retry is not the turn's error", func(t *testing.T) {
		_, ok := decodeCodexEvent("error", json.RawMessage(
			`{"threadId":"t","turnId":"u","willRetry":true,"error":{"message":"stream reset"}}`))
		assert.False(t, ok)
	})

	t.Run("an error it will not retry is", func(t *testing.T) {
		ev, ok := decodeCodexEvent("error", json.RawMessage(
			`{"threadId":"t","turnId":"u","willRetry":false,"error":{"message":"unauthorized"}}`))
		require.True(t, ok)
		assert.True(t, ev.IsError)
		assert.Equal(t, "unauthorized", ev.Result)
	})
}

func TestCodexRateLimits(t *testing.T) {
	got, ok := codexRateLimits(json.RawMessage(
		`{"rateLimits":{"primary":{"usedPercent":12,"windowDurationMins":300,"resetsAt":1790000000},"secondary":{"usedPercent":40,"windowDurationMins":10080}}}`))
	require.True(t, ok)
	assert.Equal(t, "codex", got.Provider)
	require.Len(t, got.Windows, 2)
	assert.Equal(t, "5h", got.Windows[0].Name)
	assert.InDelta(t, 12, got.Windows[0].Percent, 0.001)
	assert.False(t, got.Windows[0].ResetsAt.IsZero())
	assert.Equal(t, "7d", got.Windows[1].Name)

	_, ok = codexRateLimits(json.RawMessage(`{"rateLimits":{}}`))
	assert.False(t, ok, "nothing there is nothing, not zero")
}

// fakeAppServer is a stub `codex` that speaks enough of the protocol for one
// turn. It will not finish that turn until its approval request is answered.
func fakeAppServer(t *testing.T) (bin, replies string) {
	t.Helper()
	dir := t.TempDir()
	bin, replies = filepath.Join(dir, "codex"), filepath.Join(dir, "replies")
	script := fmt.Sprintf(`#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"decision"'*)
      printf '%%s\n' "$line" >> %q
      printf '{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","completedAtMs":0,"item":{"id":"m1","type":"agentMessage","text":"hello"}}}\n'
      printf '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","items":[],"status":"completed"}}}\n' ;;
    *'"method":"initialize"'*)
      printf '{"id":%%s,"result":{"userAgent":"stub","codexHome":"/tmp","platformFamily":"unix","platformOs":"linux"}}\n' "$id" ;;
    *'"method":"thread/start"'*)
      printf '{"id":%%s,"result":{"thread":{"id":"thread-1"}}}\n' "$id"
      printf '{"method":"thread/started","params":{"thread":{"id":"thread-1"}}}\n' ;;
    *'"method":"turn/start"'*)
      printf '{"id":%%s,"result":{"turn":{"id":"turn-1","items":[],"status":"inProgress"}}}\n' "$id"
      printf '{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-1","items":[],"status":"inProgress"}}}\n'
      printf '{"id":9001,"method":"item/commandExecution/requestApproval","params":{}}\n' ;;
    *'"method":"thread/compact/start"'*)
      printf '{"id":%%s,"result":{}}\n' "$id"
      sleep 0.2
      printf '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-2","items":[],"status":"completed"}}}\n' ;;
    *'"method":"initialized"'*) ;;
    *'"method"'*) printf '{"id":%%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id" ;;
  esac
done
`, replies)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o700)) // #nosec G306 -- a test stub that has to run
	return bin, replies
}

func TestCodexRunsATurnOverTheAppServer(t *testing.T) {
	bin, replies := fakeAppServer(t)
	var seen []Event
	c, err := NewCodex(context.Background(), CodexOpts{
		Bin: bin, Dir: t.TempDir(), OnEvent: func(ev Event) { seen = append(seen, ev) },
	})
	require.NoError(t, err)
	defer c.Close()
	assert.Equal(t, "thread-1", c.Thread())

	turn, err := c.Send(context.Background(), "say hello")
	require.NoError(t, err)
	assert.Equal(t, "thread-1", turn.SessionID)

	var said string
	for _, ev := range seen {
		said += ev.Text
	}
	assert.Equal(t, "hello", said)
	assert.False(t, c.Busy())

	// An unanswered approval stalls the turn forever, and approvalPolicy is
	// "never", so anything that still asks gets a no.
	data, err := os.ReadFile(replies) // #nosec G304 -- written by the stub above
	require.NoError(t, err)
	assert.Contains(t, string(data), `"decision":"decline"`)
}

// Compaction is a turn. Returning on the acknowledgement would let the caller
// re-send the user's prompt while it is still open, which the server refuses.
func TestCodexCompactWaitsForTheTurnToClose(t *testing.T) {
	bin, _ := fakeAppServer(t)
	c, err := NewCodex(context.Background(), CodexOpts{Bin: bin, Dir: t.TempDir()})
	require.NoError(t, err)
	defer c.Close()

	started := time.Now()
	turn, err := c.SendCommand(context.Background(), "/compact")
	require.NoError(t, err)
	assert.Equal(t, "thread-1", turn.SessionID)
	assert.GreaterOrEqual(t, time.Since(started), 150*time.Millisecond, "returned before turn/completed")
	assert.False(t, c.Busy())

	_, err = c.SendCommand(context.Background(), "/clear")
	assert.Error(t, err, "the rest of Claude Code's slash set has no Codex analogue")
}

func TestCodexResumesTheThreadItWasGiven(t *testing.T) {
	bin, _ := fakeAppServer(t)
	// The stub answers no thread/resume, so the handshake must fail rather
	// than quietly start a fresh thread under a name the caller did not pick.
	_, err := NewCodex(context.Background(), CodexOpts{Bin: bin, Dir: t.TempDir(), Resume: "thread-9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "thread/resume")
}

func TestCodexGetsAdesToolsWithTheirTokensInTheEnvironment(t *testing.T) {
	opts := CodexOpts{Env: []string{"A=1"}, MCPs: []MCP{
		{Name: "ade_wiki", URL: "http://127.0.0.1:9/mcp/wiki", Token: "draft-1"},
		{Name: "checks", URL: "http://127.0.0.1:9/mcp/checks", Token: "del-2"},
	}}
	args := strings.Join(codexArgs(opts), " ")
	assert.Contains(t, args, `mcp_servers.ade_wiki.url="http://127.0.0.1:9/mcp/wiki"`)
	assert.Contains(t, args, `mcp_servers.ade_wiki.bearer_token_env_var="ADE_WIKI_TOKEN"`)
	assert.Contains(t, args, `mcp_servers.checks.url="http://127.0.0.1:9/mcp/checks"`)
	assert.Contains(t, args, `mcp_servers.checks.bearer_token_env_var="ADE_CHECKS_TOKEN"`)
	assert.NotContains(t, args, "draft-1", "the token is not in argv")
	assert.NotContains(t, args, "del-2", "the token is not in argv")
	assert.Equal(t, []string{"A=1", "ADE_WIKI_TOKEN=draft-1", "ADE_CHECKS_TOKEN=del-2"}, codexEnv(opts))

	opts.NoMCP = true
	bare := strings.Join(codexArgs(opts), " ")
	assert.NotContains(t, bare, "ade_wiki", "no MCP means no wiki either")
	assert.NotContains(t, bare, "mcp_servers.checks")
	assert.Equal(t, []string{"A=1"}, codexEnv(opts))
}
