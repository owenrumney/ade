package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/permissions"
)

func TestARuleDecidesTheCallWithoutDrawingACard(t *testing.T) {
	asked := 0
	c := gated(t, &asked)

	response := c.Hook(context.Background(), bash("go test ./... 2>&1 | tail -20"))
	assert.Equal(t, "allow", response.Output.PermissionDecision)
	assert.Contains(t, response.Output.PermissionDecisionReason, "Bash(go test:*)")
	assert.Zero(t, asked, "a rule that decided the call still woke the page")
	assert.Empty(t, c.Asking())
}

func TestADenyRuleStopsTheCallAndSaysWhich(t *testing.T) {
	asked := 0
	c := gated(t, &asked)
	require.NoError(t, c.opts.Rules.(*permissions.Store).Remember("Bash(terraform destroy:*)", true, false))

	response := c.Hook(context.Background(), bash("terraform destroy -auto-approve"))
	assert.Equal(t, "deny", response.Output.PermissionDecision)
	assert.Contains(t, response.Output.PermissionDecisionReason, "Bash(terraform destroy:*)")
	assert.Zero(t, asked)
}

func TestACallNoRuleCoversIsHeldUntilItIsDecided(t *testing.T) {
	asked := 0
	c := gated(t, &asked)

	done := make(chan HookResponse, 1)
	go func() { done <- c.Hook(context.Background(), bash("terraform apply")) }()

	var ask Ask
	require.Eventually(t, func() bool {
		if asking := c.Asking(); len(asking) == 1 {
			ask = asking[0]
			return true
		}
		return false
	}, time.Second, 5*time.Millisecond, "the call was never held")

	assert.Equal(t, "Bash", ask.Tool)
	assert.Equal(t, "terraform apply", ask.Target)
	assert.Equal(t, "Bash(terraform apply:*)", ask.Rule, "the card is offered the shape, not the command")

	require.NoError(t, c.Decide(ask.ToolID, true, "", ""))
	assert.Equal(t, "allow", (<-done).Output.PermissionDecision)
	assert.Empty(t, c.Asking())
}

func TestTheCardProposesARuleForWhatTheRulesLeftAndNamesWhatTheyCovered(t *testing.T) {
	asked := 0
	c := gated(t, &asked)

	go c.Hook(context.Background(), bash("go test ./... 2>&1 && curl -s https://x.dev"))
	require.Eventually(t, func() bool { return len(c.Asking()) == 1 }, time.Second, 5*time.Millisecond)

	ask := c.Asking()[0]
	assert.Equal(t, "curl -s https://x.dev", ask.Target, "the chips are for the part being asked about")
	assert.Equal(t, "Bash(curl:*)", ask.Rule, "not Bash(go test:*), which is already a rule")
	assert.Equal(t, []string{"Bash(go test:*)"}, ask.Allowed)
	require.NoError(t, c.Decide(ask.ToolID, false, "", ""))
}

func TestADeniedCallCarriesTheReasonBackToTheModel(t *testing.T) {
	asked := 0
	c := gated(t, &asked)

	done := make(chan HookResponse, 1)
	go func() { done <- c.Hook(context.Background(), bash("rm -rf vendor")) }()
	require.Eventually(t, func() bool { return len(c.Asking()) == 1 }, time.Second, 5*time.Millisecond)

	require.NoError(t, c.Decide(c.Asking()[0].ToolID, false, "", "vendor is checked in"))
	response := <-done
	assert.Equal(t, "deny", response.Output.PermissionDecision)
	assert.Equal(t, "vendor is checked in", response.Output.PermissionDecisionReason)
}

func TestAQuestionIsAnsweredThroughTheSameHook(t *testing.T) {
	asked := 0
	c := gated(t, &asked)
	request := HookRequest{
		HookEventName: preToolUse,
		ToolName:      askTool,
		ToolUseID:     "ask-1",
		ToolInput:     map[string]any{"questions": []any{"which?"}},
	}

	done := make(chan HookResponse, 1)
	go func() { done <- c.Hook(context.Background(), request) }()
	require.Eventually(t, func() bool { return len(c.Asking()) == 1 }, time.Second, 5*time.Millisecond)

	require.NoError(t, c.Answer("ask-1", map[string]string{"which?": "the second"}))
	response := <-done
	assert.Equal(t, "allow", response.Output.PermissionDecision)
	assert.Equal(t, map[string]string{"which?": "the second"},
		response.Output.UpdatedInput["answers"], "the answers go back as the tool's own input")
	assert.Equal(t, []any{"which?"}, response.Output.UpdatedInput["questions"])
}

func TestAnAskNobodyIsWaitingOnAnyMoreIsNotLeftHeld(t *testing.T) {
	asked := 0
	c := gated(t, &asked)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan HookResponse, 1)
	go func() { done <- c.Hook(ctx, bash("sleep 600")) }()
	require.Eventually(t, func() bool { return len(c.Asking()) == 1 }, time.Second, 5*time.Millisecond)

	cancel()
	assert.Equal(t, "deny", (<-done).Output.PermissionDecision)
	assert.Empty(t, c.Asking(), "the card was left on screen with nothing behind it")
}

func TestTheGateIsRegisteredOnlyForTheToolsWorthGating(t *testing.T) {
	gated := Opts{
		Hook:      "http://127.0.0.1:1/api/hook/pretooluse",
		HookBin:   "/usr/local/bin/ade",
		HookToken: "tok",
	}
	args := claudeArgs(gated)
	i := indexOf(args, "--settings")
	require.GreaterOrEqual(t, i, 0, "the hook was not configured")

	var settings struct {
		Hooks map[string][]struct {
			Matcher string           `json:"matcher"`
			Hooks   []map[string]any `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal([]byte(args[i+1]), &settings))
	entry := settings.Hooks[preToolUse][0]
	assert.Equal(t, GatedTools, entry.Matcher)

	// A command, not http: Claude Code lets a call through when an http hook
	// is not answered, and `ade hook` answers even with ade unreachable.
	assert.Equal(t, "command", entry.Hooks[0]["type"])
	assert.Equal(t, "/usr/local/bin/ade hook", entry.Hooks[0]["command"])
	assert.NotContains(t, args[i+1], "tok", "the token itself was put in the settings")
	assert.NotContains(t, args[i+1], "http", "the gate reads the url from its environment")

	assert.NotContains(t, claudeArgs(Opts{}), "--settings", "a run with no endpoint configured a hook")
	assert.NotContains(t, claudeArgs(Opts{Hook: gated.Hook}), "--settings",
		"a hook with no ade to run would never answer")
}

func TestTheDenyListReachesClaudeCodeGatedOrNot(t *testing.T) {
	deny := []string{"Bash(git push --force:*)"}
	for name, opts := range map[string]Opts{
		"gated":        {Hook: "http://127.0.0.1:1/x", HookBin: "/usr/local/bin/ade", DenyRules: deny},
		"in a sandbox": {DenyRules: deny},
	} {
		t.Run(name, func(t *testing.T) {
			args := claudeArgs(opts)
			i := indexOf(args, "--settings")
			require.GreaterOrEqual(t, i, 0, "the deny list was not passed")
			var settings struct {
				Permissions struct {
					Deny []string `json:"deny"`
				} `json:"permissions"`
			}
			require.NoError(t, json.Unmarshal([]byte(args[i+1]), &settings))
			assert.Equal(t, deny, settings.Permissions.Deny)
		})
	}
}

func TestTheGatedToolsAreTheOnesAChangeCouldHarm(t *testing.T) {
	for _, tool := range []string{"Bash", "Write", "Edit", "WebFetch", "Task", "AskUserQuestion"} {
		assert.Contains(t, GatedTools, tool)
	}
	assert.NotContains(t, GatedTools, "Read")
	assert.NotContains(t, GatedTools, "Grep")
}

// gated is a Claude with a seeded rule file and nothing running, which is all
// the hook needs. asked counts the times the page was woken.
func gated(t *testing.T, asked *int) *Claude {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	rules, err := permissions.Open()
	require.NoError(t, err)
	return &Claude{
		gone: make(chan struct{}),
		opts: Opts{Rules: rules, OnAsk: func() { *asked++ }},
	}
}

func bash(command string) HookRequest {
	return HookRequest{
		HookEventName: preToolUse,
		ToolName:      "Bash",
		ToolUseID:     "call-" + filepath.Base(command),
		ToolInput:     map[string]any{"command": command},
	}
}

func indexOf(args []string, want string) int {
	for i, arg := range args {
		if arg == want {
			return i
		}
	}
	return -1
}

func TestASessionRunningFreeIsNeverAskedAgain(t *testing.T) {
	asked := 0
	c := gated(t, &asked)
	store := c.opts.Rules.(*permissions.Store)
	require.NoError(t, store.Remember("Bash(terraform destroy:*)", true, false))
	store.SetFree(true)

	response := c.Hook(context.Background(), bash("brew install trivy"))
	assert.Equal(t, "allow", response.Output.PermissionDecision)
	assert.Contains(t, response.Output.PermissionDecisionReason, string(permissions.FreeRule))
	assert.Zero(t, asked, "a free session still drew a card")

	// Free is about the asking. A rule that says never still says never.
	denied := c.Hook(context.Background(), bash("terraform destroy -auto-approve"))
	assert.Equal(t, "deny", denied.Output.PermissionDecision)
}

func TestAQuestionIsStillPutToYouWhenRunningFree(t *testing.T) {
	asked := 0
	c := gated(t, &asked)
	c.opts.Rules.(*permissions.Store).SetFree(true)

	done := make(chan HookResponse, 1)
	go func() {
		done <- c.Hook(context.Background(), HookRequest{
			HookEventName: preToolUse, ToolName: askTool, ToolUseID: "ask-free",
			ToolInput: map[string]any{"questions": []any{"which?"}},
		})
	}()
	require.Eventually(t, func() bool { return len(c.Asking()) == 1 }, time.Second, 5*time.Millisecond,
		"running free swallowed a question only you can answer")

	require.NoError(t, c.Answer("ask-free", map[string]string{"which?": "this one"}))
	assert.Equal(t, "allow", (<-done).Output.PermissionDecision)
}

func TestEveryCallNoRuleReachesIsAsked(t *testing.T) {
	asked := 0
	c := gated(t, &asked)
	// Neither allowed nor denied by the seeds: each must pause the turn.
	for _, command := range []string{
		"brew install trivy",
		"go test ./... && curl -s https://example.com",
		"gofmt -w x.go > /etc/crontab",
		"git log $(rm -rf /)",
	} {
		t.Run(command, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan HookResponse, 1)
			go func() { done <- c.Hook(ctx, bash(command)) }()
			require.Eventually(t, func() bool { return len(c.Asking()) == 1 }, time.Second, 5*time.Millisecond,
				"it decided this on its own instead of asking")
			require.NoError(t, c.Decide(c.Asking()[0].ToolID, true, "", ""))
			assert.Equal(t, "allow", (<-done).Output.PermissionDecision)
		})
	}
}

func TestACallAdeCannotKeyIsRefusedRatherThanAllowed(t *testing.T) {
	asked := 0
	c := gated(t, &asked)
	for _, request := range []HookRequest{
		{HookEventName: preToolUse, ToolName: "Bash"},
		{HookEventName: preToolUse, ToolUseID: "no-tool"},
		{HookEventName: preToolUse},
	} {
		response := c.Hook(context.Background(), request)
		assert.Equal(t, "deny", response.Output.PermissionDecision,
			"a call with nowhere to draw a card was allowed")
	}
	assert.Empty(t, c.Asking())
}

// A boxed run asks over stdio, not the hook. Run free has to reach it too.
func TestRunningFreeAnswersASandboxedAskWithoutACard(t *testing.T) {
	asked := 0
	c := gated(t, &asked)
	ask := func(id, command string) controlResponse {
		output := &closeBuffer{}
		c.stdin, c.events = output, make(chan Event, 4)
		c.readStdout(strings.NewReader(`{"type":"control_request","request_id":"` + id +
			`","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"` + id +
			`","input":{"command":"` + command + `"}}}` + "\n"))
		var response controlResponse
		if len(output.Bytes()) > 0 {
			require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response))
		}
		return response
	}

	ask("one", "terraform apply")
	require.Len(t, c.Asking(), 1, "asking is the default")
	require.NoError(t, c.Decide("one", true, "", ""))

	store := c.opts.Rules.(*permissions.Store)
	store.SetFree(true)
	assert.Equal(t, "allow", ask("two", "terraform apply").Response.Response.Behavior)
	assert.Empty(t, c.Asking())

	require.NoError(t, store.Remember("Bash(terraform destroy:*)", true, false))
	assert.Equal(t, "deny", ask("three", "terraform destroy -auto-approve").Response.Response.Behavior)
	assert.Empty(t, c.Asking())

	c.events = make(chan Event, 4)
	c.readStdout(strings.NewReader(`{"type":"control_request","request_id":"q","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","tool_use_id":"q","input":{"questions":[]}}}` + "\n"))
	assert.Len(t, c.Asking(), 1, "a question is still yours to answer")
}
