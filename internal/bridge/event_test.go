package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closeBuffer stands in for the subprocess's stdin. Locked, because a refused
// ask is written from the timer's goroutine while the test reads.
type closeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *closeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *closeBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}

func (b *closeBuffer) String() string { return string(b.Bytes()) }

func (b *closeBuffer) Close() error { return nil }

type controlResponse struct {
	Type     string `json:"type"`
	Response struct {
		Subtype   string `json:"subtype"`
		RequestID string `json:"request_id"`
		Response  struct {
			Behavior           string           `json:"behavior"`
			ToolName           string           `json:"toolName"`
			Message            string           `json:"message"`
			UpdatedInput       map[string]any   `json:"updatedInput"`
			UpdatedPermissions []map[string]any `json:"updatedPermissions"`
		} `json:"response"`
	} `json:"response"`
}

func TestReadStdoutHoldsAQuestionUntilItIsAnswered(t *testing.T) {
	input := `{"type":"control_request","request_id":"req-1","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","tool_use_id":"toolu_1","input":{"questions":[{"question":"Tea or coffee?","options":[{"label":"Tea"},{"label":"Coffee"}]}]}}}`
	var output closeBuffer
	c := &Claude{stdin: &output, events: make(chan Event, 1)}
	c.readStdout(strings.NewReader(input + "\n"))

	assert.Empty(t, output.Bytes())
	assert.Equal(t, []Ask{{ToolID: "toolu_1", Tool: "AskUserQuestion"}}, c.Asking())
	assert.Empty(t, c.events)

	require.NoError(t, c.Answer("toolu_1", map[string]string{"Tea or coffee?": "Coffee"}))
	assert.Empty(t, c.Asking())

	var response controlResponse
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response))
	assert.Equal(t, "control_response", response.Type)
	assert.Equal(t, "success", response.Response.Subtype)
	assert.Equal(t, "req-1", response.Response.RequestID)
	assert.Equal(t, "allow", response.Response.Response.Behavior)
	assert.Equal(t, "AskUserQuestion", response.Response.Response.ToolName)
	assert.Len(t, response.Response.Response.UpdatedInput["questions"], 1)
	assert.Equal(t, map[string]any{"Tea or coffee?": "Coffee"}, response.Response.Response.UpdatedInput["answers"])

	assert.Error(t, c.Answer("toolu_1", nil), "a question is answered once")
}

func TestReadStdoutHoldsAPermissionAskForTheUser(t *testing.T) {
	input := `{"type":"control_request","request_id":"req-2","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"toolu_2","input":{"command":"rm -rf /"}}}`
	var output closeBuffer
	c := &Claude{stdin: &output, events: make(chan Event, 1)}
	c.readStdout(strings.NewReader(input + "\n"))

	// Held, not answered: the turn stops here until somebody says yes or no.
	assert.Empty(t, output.Bytes())
	assert.Equal(t, []Ask{{ToolID: "toolu_2", Tool: "Bash"}}, c.Asking())
	tool, ok := c.Pending("toolu_2")
	assert.True(t, ok)
	assert.Equal(t, "Bash", tool)

	require.NoError(t, c.Decide("toolu_2", false, "", "not on this branch"))
	var response controlResponse
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response))
	assert.Equal(t, "req-2", response.Response.RequestID)
	assert.Equal(t, "deny", response.Response.Response.Behavior)
	assert.Equal(t, "Bash", response.Response.Response.ToolName)
	assert.Equal(t, "not on this branch", response.Response.Response.Message)
	assert.Empty(t, c.Asking())
	assert.Error(t, c.Decide("toolu_2", true, "", ""), "decided once")
}

func TestADenialAlwaysCarriesSomethingTheModelCanRead(t *testing.T) {
	input := `{"type":"control_request","request_id":"req-3","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"toolu_3","input":{}}}`
	var output closeBuffer
	c := &Claude{stdin: &output, events: make(chan Event, 1)}
	c.readStdout(strings.NewReader(input + "\n"))

	require.NoError(t, c.Decide("toolu_3", false, "", "   "))
	var response controlResponse
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response))
	assert.NotEmpty(t, response.Response.Response.Message)
}

// A frame with no tool call id cannot be tracked, so it is refused rather
// than held: holding one would pause the turn with nothing able to clear it.
func TestAnUntrackableAskIsRefusedRatherThanHeld(t *testing.T) {
	input := `{"type":"control_request","request_id":"req-4","request":{"subtype":"can_use_tool","tool_name":"Bash"}}`
	var output closeBuffer
	c := &Claude{stdin: &output, events: make(chan Event, 1)}
	c.readStdout(strings.NewReader(input + "\n"))

	var response controlResponse
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response))
	assert.Equal(t, "deny", response.Response.Response.Behavior)
	assert.Empty(t, c.Asking())
}

func TestDecodeCanUseToolRequestRejectsOtherFrames(t *testing.T) {
	for _, line := range []string{
		`{"type":"assistant"}`,
		`{"type":"control_request","request_id":"req-1","request":{"subtype":"interrupt"}}`,
		`{"type":"control_request","request":{"subtype":"can_use_tool","tool_name":"Bash"}}`,
	} {
		_, ok := decodeCanUseToolRequest([]byte(line))
		assert.False(t, ok, line)
	}
}

func TestDecodeEvent(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		ok    bool
		check func(t *testing.T, ev Event)
	}{
		{
			name: "system init",
			line: `{"type":"system","subtype":"init","session_id":"abc"}`,
			ok:   true,
			check: func(t *testing.T, ev Event) {
				assert.Equal(t, "system", ev.Type)
				assert.Equal(t, "init", ev.Subtype)
				assert.Equal(t, "abc", ev.SessionID)
				assert.Empty(t, ev.ToolUses)
			},
		},
		{
			name: "assistant text and tool use",
			line: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Looking"},{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"a.go"}}]}}`,
			ok:   true,
			check: func(t *testing.T, ev Event) {
				assert.Equal(t, "Looking", ev.Text)
				require.Len(t, ev.ToolUses, 1)
				assert.Equal(t, "Read", ev.ToolUses[0].Name)
				assert.Equal(t, "toolu_1", ev.ToolUses[0].ID)
				assert.JSONEq(t, `{"file_path":"a.go"}`, string(ev.ToolUses[0].Input))
			},
		},
		{
			name: "multiple text blocks join",
			line: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}}`,
			ok:   true,
			check: func(t *testing.T, ev Event) {
				assert.Equal(t, "ab", ev.Text)
			},
		},
		{
			name: "successful result",
			line: `{"type":"result","subtype":"success","is_error":false,"result":"done","total_cost_usd":0.012}`,
			ok:   true,
			check: func(t *testing.T, ev Event) {
				assert.Equal(t, "result", ev.Type)
				assert.False(t, ev.IsError)
				assert.Equal(t, "done", ev.Result)
				assert.InDelta(t, 0.012, ev.CostUSD, 1e-9)
			},
		},
		{
			name: "error result",
			line: `{"type":"result","subtype":"error_max_turns","is_error":true,"result":"hit limit"}`,
			ok:   true,
			check: func(t *testing.T, ev Event) {
				assert.True(t, ev.IsError)
				assert.Equal(t, "error_max_turns", ev.Subtype)
			},
		},
		{
			name: "malformed json is skipped",
			line: `{"type":"assistant"`,
			ok:   false,
		},
		{
			name: "unknown block types are ignored",
			line: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","text":"hmm"}]}}`,
			ok:   true,
			check: func(t *testing.T, ev Event) {
				assert.Empty(t, ev.Text)
				assert.Empty(t, ev.ToolUses)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, ok := DecodeEvent([]byte(tt.line))
			require.Equal(t, tt.ok, ok)
			if tt.check != nil {
				tt.check(t, ev)
			}
		})
	}
}

func TestDecodeEventReadsUsage(t *testing.T) {
	ev, ok := DecodeEvent([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":12,"cache_read_input_tokens":40000,"cache_creation_input_tokens":500,"output_tokens":9}}}`))
	require.True(t, ok)
	require.NotNil(t, ev.Usage)
	assert.Equal(t, 40512, ev.Usage.Context(), "the window holds the prompt, cached or not")
	assert.Equal(t, 9, ev.Usage.Output)

	// A result carries the turn's usage at the top level.
	ev, ok = DecodeEvent([]byte(`{"type":"result","result":"done","usage":{"input_tokens":3,"output_tokens":4}}`))
	require.True(t, ok)
	require.NotNil(t, ev.Usage)
	assert.Equal(t, 3, ev.Usage.Context())

	// None is nil, not zero.
	ev, _ = DecodeEvent([]byte(`{"type":"system","subtype":"init"}`))
	assert.Nil(t, ev.Usage)
}

func TestFindToolUse(t *testing.T) {
	events := []Event{
		{Type: "assistant", ToolUses: []ToolUse{{Name: "Read", Input: []byte(`{"n":1}`)}}},
		{Type: "assistant", ToolUses: []ToolUse{{Name: "ReportFindings", Input: []byte(`{"findings":[]}`)}}},
		{Type: "assistant", ToolUses: []ToolUse{{Name: "ReportFindings", Input: []byte(`{"findings":[{"file":"a.go"}]}`)}}},
	}

	t.Run("returns the last call", func(t *testing.T) {
		got, ok := FindToolUse(events, "ReportFindings")
		require.True(t, ok)
		assert.JSONEq(t, `{"findings":[{"file":"a.go"}]}`, string(got))
	})

	t.Run("reports absence", func(t *testing.T) {
		_, ok := FindToolUse(events, "Edit")
		assert.False(t, ok)
	})

	t.Run("empty events", func(t *testing.T) {
		_, ok := FindToolUse(nil, "ReportFindings")
		assert.False(t, ok)
	})
}

func TestWithoutAnthropicKeys(t *testing.T) {
	in := []string{"PATH=/bin", "ANTHROPIC_API_KEY=sk-1", "HOME=/root", "ANTHROPIC_AUTH_TOKEN=tok"}
	got := withoutAnthropicKeys(in)
	assert.Equal(t, []string{"PATH=/bin", "HOME=/root"}, got)
}

func TestStderrTail(t *testing.T) {
	assert.Empty(t, stderrTail("   "))
	assert.Equal(t, "\nstderr:\nboom", stderrTail("boom\n"))

	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'x'
	}
	assert.Len(t, stderrTail(string(long)), len("\nstderr:\n")+4000)
}

func TestUserMessageCarriesImagesAsBlocks(t *testing.T) {
	// Text alone stays a plain string, which is the shape every existing
	// transcript was written with.
	plain := userMessage("hello", nil)
	assert.Equal(t, "hello", plain["message"].(map[string]any)["content"])

	// With a picture it becomes content blocks, the picture first so the text
	// reads as its caption.
	with := userMessage("what is this?", []Image{{MediaType: "image/png", Data: "aGk="}})
	blocks, ok := with["message"].(map[string]any)["content"].([]map[string]any)
	require.True(t, ok, "images need content blocks, not a string")
	require.Len(t, blocks, 2)
	assert.Equal(t, "image", blocks[0]["type"])
	source := blocks[0]["source"].(map[string]any)
	assert.Equal(t, "base64", source["type"])
	assert.Equal(t, "image/png", source["media_type"])
	assert.Equal(t, "aGk=", source["data"])
	assert.Equal(t, map[string]any{"type": "text", "text": "what is this?"}, blocks[1])
}

// newWith starts something other than claude, for driving Send without one.
func newWith(ctx context.Context, bin string, args []string, dir string) (*Claude, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	return start(cmd, Opts{Dir: dir})
}

// TestALocalCommandIsNudgedIntoAnswering drives Send through a stand-in for
// claude: a shell that says nothing to the first line and answers the second,
// which is what a built-in like /usage looks like from the outside — the
// command runs locally, and its output rides on the next prompt.
func TestALocalCommandIsNudgedIntoAnswering(t *testing.T) {
	was := commandGrace
	commandGrace = 50 * time.Millisecond
	defer func() { commandGrace = was }()

	script := `read first; read second; printf '%s\n' '{"type":"result","subtype":"success","result":"stats"}'`
	c, err := newWith(context.Background(), "sh", []string{"-c", script}, t.TempDir())
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	turn, err := c.SendCommand(context.Background(), "/usage")
	require.NoError(t, err)
	assert.Equal(t, "stats", turn.Text)
}

// TestAPlainTurnIsNotNudged is the other half: a message that gets an answer
// in time is left alone, so a skill that expands into a turn is not talked
// over.
func TestAPlainTurnIsNotNudged(t *testing.T) {
	was := commandGrace
	commandGrace = 50 * time.Millisecond
	defer func() { commandGrace = was }()

	// Answers the first line at once; a nudge would then be a second line
	// nobody answers, and the turn would already be over.
	script := `read first; printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}'; printf '%s\n' '{"type":"result","subtype":"success","result":"hi"}'`
	c, err := newWith(context.Background(), "sh", []string{"-c", script}, t.TempDir())
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	turn, err := c.SendCommand(context.Background(), "/code-review")
	require.NoError(t, err)
	assert.Equal(t, "hi", turn.Text)
	assert.Len(t, turn.Events, 2)
}

// TestATurnTheAgentStartsItselfIsShownAndNotTakenAsTheNextReply is a wake on
// a background task: it streams with no send open, and the next send gets
// its own result rather than the wake's.
func TestATurnTheAgentStartsItselfIsShownAndNotTakenAsTheNextReply(t *testing.T) {
	script := `printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"woke"}]}}'
printf '%s\n' '{"type":"result","subtype":"success","result":"woke"}'
read first
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"reply"}]}}'
printf '%s\n' '{"type":"result","subtype":"success","result":"reply"}'
read hold`
	woke := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var got []string
	cmd := exec.CommandContext(context.Background(), "sh", "-c", script)
	c, err := start(cmd, Opts{OnEvent: func(ev Event) {
		mu.Lock()
		got = append(got, ev.Type+":"+ev.Text+ev.Result)
		mu.Unlock()
		if ev.Type == "result" {
			once.Do(func() { close(woke) })
		}
	}})
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	select {
	case <-woke:
	case <-time.After(5 * time.Second):
		t.Fatal("a turn nobody sent never reached the chat")
	}
	mu.Lock()
	assert.Equal(t, []string{"assistant:woke", "result:woke"}, got, "the wake's result is how its run is closed")
	mu.Unlock()

	turn, err := c.Send(context.Background(), "go")
	require.NoError(t, err)
	assert.Equal(t, "reply", turn.Text)
	mu.Lock()
	assert.Equal(t, []string{"assistant:woke", "result:woke", "assistant:reply"}, got, "a sent turn's result is the sender's")
	mu.Unlock()
}

// TestALongTurnNobodySentDoesNotStallTheReader is past the channel's buffer:
// with nothing reading, the reader used to block and the agent with it.
func TestALongTurnNobodySentDoesNotStallTheReader(t *testing.T) {
	script := `i=0
while [ $i -lt 200 ]; do
  printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"line"}]}}'
  i=$((i+1))
done
printf '%s\n' '{"type":"result","subtype":"success","result":"woke"}'
read first
printf '%s\n' '{"type":"result","subtype":"success","result":"reply"}'
read hold`
	var mu sync.Mutex
	seen := 0
	cmd := exec.CommandContext(context.Background(), "sh", "-c", script)
	c, err := start(cmd, Opts{OnEvent: func(ev Event) {
		if ev.Type != "assistant" {
			return
		}
		mu.Lock()
		seen++
		mu.Unlock()
	}})
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return seen == 200
	}, 5*time.Second, 10*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	turn, err := c.Send(ctx, "go")
	require.NoError(t, err)
	assert.Equal(t, "reply", turn.Text)
}

// A user message with string content is the harness talking — a background
// task's notification — and used to be dropped as undecodable.
func TestANoticeFromTheHarnessIsKept(t *testing.T) {
	ev, ok := DecodeEvent([]byte(`{"type":"user","message":{"role":"user","content":"<task-notification>\n<tool-use-id>toolu_1</tool-use-id>\n</task-notification>"}}`))
	require.True(t, ok)
	assert.Equal(t, "user", ev.Type)
	assert.Contains(t, ev.Notice, "<tool-use-id>toolu_1</tool-use-id>")
	assert.Empty(t, ev.Text, "a notice is not something the agent said")

	_, ok = DecodeEvent([]byte(`{"type":"assistant","message":{"content":"never a string"}}`))
	assert.False(t, ok, "only the user's side carries a string")
}

// TestAResultPrintedAsTheProcessExitsIsStillTheTurn pins the race that made
// one turn in five fail: Wait closes stdout as soon as the process goes, so
// the result had to be read off the pipe before it was reaped.
func TestAResultPrintedAsTheProcessExitsIsStillTheTurn(t *testing.T) {
	script := `read first; printf '%s\n' '{"type":"result","subtype":"success","result":"done"}'; exit 0`

	for i := range 20 {
		c, err := newWith(context.Background(), "sh", []string{"-c", script}, t.TempDir())
		require.NoError(t, err)

		turn, err := c.Send(context.Background(), "go")
		require.NoErrorf(t, err, "run %d lost the result to the exit", i)
		assert.Equal(t, "done", turn.Text)
		_ = c.Close()
	}
}

// TestAnExitWithoutAResultKeepsItsStatus is the other end of that drain: the
// reader closes events before the waiter reports the exit, so a crash would
// read as "claude closed" if the closed channel were taken as the whole story.
func TestAnExitWithoutAResultKeepsItsStatus(t *testing.T) {
	for i := range 20 {
		c, err := newWith(context.Background(), "sh", []string{"-c", `read first; exit 3`}, t.TempDir())
		require.NoError(t, err)

		_, err = c.Send(context.Background(), "go")
		require.Errorf(t, err, "run %d", i)
		assert.Containsf(t, err.Error(), "exit status 3", "run %d lost the exit code", i)
		_ = c.Close()
	}
}

// TestAGrandchildHoldingStdoutDoesNotHideTheExit is why the drain is bounded
// and why Wait does not sit behind it: a backgrounded grandchild inherits
// stdout, so the pipe never reaches EOF and only the exit says the agent went.
func TestAGrandchildHoldingStdoutDoesNotHideTheExit(t *testing.T) {
	was := exitDrain
	exitDrain = 200 * time.Millisecond
	defer func() { exitDrain = was }()

	c, err := newWith(context.Background(), "sh", []string{"-c", `read first; (sleep 30 &); exit 3`}, t.TempDir())
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = c.Send(ctx, "go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit status 3")
	assert.True(t, c.Dead(), "the owner replaces a dead bridge, so it has to know")
}

// The rule is Claude Code's, not ade's: ade reads it off the request to show
// it, and hands the whole suggestion back to remember it.
func TestRememberingAnAllowHandsBackTheRuleClaudeCodeSuggested(t *testing.T) {
	input := `{"type":"control_request","request_id":"req-5","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"toolu_5","input":{"command":"curl -s https://example.com"},"permission_suggestions":[{"type":"addRules","behavior":"allow","destination":"localSettings","rules":[{"toolName":"Bash","ruleContent":"curl -s https://example.com"}]}]}}`
	var output closeBuffer
	c := &Claude{stdin: &output, events: make(chan Event, 1)}
	c.readStdout(strings.NewReader(input + "\n"))

	require.Equal(t, []Ask{{ToolID: "toolu_5", Tool: "Bash", Rule: "Bash(curl -s https://example.com)"}}, c.Asking())

	require.NoError(t, c.Decide("toolu_5", true, RememberAlways, ""))
	var response controlResponse
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response))
	assert.Equal(t, "allow", response.Response.Response.Behavior)
	require.Len(t, response.Response.Response.UpdatedPermissions, 1)
	// Where Claude Code said to keep it, untouched.
	assert.Equal(t, "localSettings", response.Response.Response.UpdatedPermissions[0]["destination"])
}

// Remembering for the session is the same rule aimed somewhere that does not
// outlive the process, so nothing reaches the user's checkout.
func TestRememberingForTheSessionLeavesNothingOnDisk(t *testing.T) {
	input := `{"type":"control_request","request_id":"req-7","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"toolu_7","input":{},"permission_suggestions":[{"type":"addRules","behavior":"allow","destination":"localSettings","rules":[{"toolName":"Bash","ruleContent":"curl -s https://example.com"}]}]}}`
	var output closeBuffer
	c := &Claude{stdin: &output, events: make(chan Event, 1)}
	c.readStdout(strings.NewReader(input + "\n"))

	require.NoError(t, c.Decide("toolu_7", true, RememberSession, ""))
	var response controlResponse
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response))
	got := response.Response.Response.UpdatedPermissions
	require.Len(t, got, 1)
	assert.Equal(t, "session", got[0]["destination"])
	// Only the destination moved: the rule itself is still Claude Code's.
	assert.Equal(t, "addRules", got[0]["type"])
	assert.Equal(t, "allow", got[0]["behavior"])
	assert.Len(t, got[0]["rules"], 1)
}

// Allowing once must not persist anything: that is the whole difference.
func TestAllowingOnceRemembersNothing(t *testing.T) {
	input := `{"type":"control_request","request_id":"req-6","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"toolu_6","input":{},"permission_suggestions":[{"type":"addRules","behavior":"allow","destination":"localSettings","rules":[{"toolName":"Bash","ruleContent":"ls"}]}]}}`
	var output closeBuffer
	c := &Claude{stdin: &output, events: make(chan Event, 1)}
	c.readStdout(strings.NewReader(input + "\n"))

	require.NoError(t, c.Decide("toolu_6", true, "", ""))
	var response controlResponse
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response))
	assert.Equal(t, "allow", response.Response.Response.Behavior)
	assert.Empty(t, response.Response.Response.UpdatedPermissions)
}

func TestHoldingAndAnsweringAnAskBothSaySo(t *testing.T) {
	input := `{"type":"control_request","request_id":"req-8","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"toolu_8","input":{}}}`
	var output closeBuffer
	var said int
	c := &Claude{
		stdin:  &output,
		events: make(chan Event, 1),
		opts:   Opts{OnAsk: func() { said++ }},
	}
	c.readStdout(strings.NewReader(input + "\n"))
	assert.Equal(t, 1, said, "held")

	require.NoError(t, c.Decide("toolu_8", true, "", ""))
	assert.Equal(t, 2, said, "answered")

	assert.Error(t, c.Decide("toolu_8", true, "", ""))
	assert.Equal(t, 2, said)
}

// A held ask with nobody to answer it used to hang the turn until the session
// was restarted. It refuses itself instead, so the model can carry on.
func TestAnAskNobodyAnswersRefusesItself(t *testing.T) {
	old := askGrace
	askGrace = 10 * time.Millisecond
	t.Cleanup(func() { askGrace = old })

	input := `{"type":"control_request","request_id":"req-9","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"toolu_9","input":{}}}`
	var output closeBuffer
	c := &Claude{stdin: &output, events: make(chan Event, 1), gone: make(chan struct{})}
	c.readStdout(strings.NewReader(input + "\n"))

	// The write lands after the ask leaves the map, so wait on the response
	// rather than on the waiting set emptying.
	require.Eventually(t, func() bool { return len(output.Bytes()) > 0 }, time.Second, 5*time.Millisecond)

	var response controlResponse
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response))
	assert.Equal(t, "deny", response.Response.Response.Behavior)
	assert.Contains(t, response.Response.Response.Message, "nobody answered")
}

// An answered ask must not be refused a second time when its timer fires.
func TestAnAnsweredAskIsNotRefusedLater(t *testing.T) {
	old := askGrace
	askGrace = 10 * time.Millisecond
	t.Cleanup(func() { askGrace = old })

	input := `{"type":"control_request","request_id":"req-10","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"toolu_10","input":{}}}`
	var output closeBuffer
	c := &Claude{stdin: &output, events: make(chan Event, 1), gone: make(chan struct{})}
	c.readStdout(strings.NewReader(input + "\n"))

	require.NoError(t, c.Decide("toolu_10", true, "", ""))
	time.Sleep(50 * time.Millisecond)

	// One response, and it is the allow: the timer found nothing to refuse.
	assert.Equal(t, 1, bytes.Count(bytes.TrimSpace(output.Bytes()), []byte("\n"))+1)
	assert.Contains(t, output.String(), `"behavior":"allow"`)
}
