package oneshot

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
