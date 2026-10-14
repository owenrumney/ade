package transcripts

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// claudeFixture covers the easy-to-get-wrong parts: usage repeated across one
// response's lines, machinery in the user turn, and a sidechain prompt.
const claudeFixture = `{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/coast","gitBranch":"fix/pricing","message":{"role":"user","content":"<system-reminder>ignore me</system-reminder>fix the flaky pricing test"}}
{"type":"assistant","timestamp":"2026-08-01T10:00:05Z","requestId":"req-1","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5","content":[{"type":"tool_use","id":"tool-1","name":"Bash"}],"usage":{"input_tokens":100,"output_tokens":200,"cache_read_input_tokens":1000,"cache_creation_input_tokens":500}}}
{"type":"assistant","timestamp":"2026-08-01T10:00:06Z","requestId":"req-1","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"on it"}],"usage":{"input_tokens":100,"output_tokens":200,"cache_read_input_tokens":1000,"cache_creation_input_tokens":500}}}
{"type":"user","timestamp":"2026-08-01T10:00:07Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","is_error":true,"content":"failed"}]}}
{"type":"system","timestamp":"2026-08-01T10:00:30Z","subtype":"compact_boundary"}
{"type":"user","timestamp":"2026-08-01T10:00:08Z","isSidechain":true,"message":{"role":"user","content":"subagent instructions"}}
{"type":"user","timestamp":"2026-08-01T10:01:00Z","message":{"role":"user","content":"now run the whole suite"}}
{"type":"assistant","timestamp":"2026-08-01T10:01:05Z","requestId":"req-2","message":{"id":"msg-2","role":"assistant","model":"claude-opus-5","content":[{"type":"tool_use","name":"Bash"}],"usage":{"input_tokens":50,"output_tokens":25,"cache_read_input_tokens":2000,"cache_creation_input_tokens":0}}}
{"type":"result","timestamp":"2026-08-01T10:01:06Z","duration_ms":66000,"is_error":false}
{"type":"ai-title","aiTitle":"Fix flaky pricing test"}
`

const piFixture = `{"type":"session","version":3,"id":"019f-pi","timestamp":"2026-08-02T09:00:00Z","cwd":"/work/mdview"}
{"type":"model_change","timestamp":"2026-08-02T09:00:01Z","provider":"openai-codex","modelId":"gpt-5.6-terra"}
{"type":"message","timestamp":"2026-08-02T09:00:02Z","message":{"role":"user","content":[{"type":"text","text":"review PR 870"}]}}
{"type":"message","timestamp":"2026-08-02T09:00:10Z","message":{"role":"assistant","model":"gpt-5.6-terra","content":[{"type":"thinking","text":"hmm"},{"type":"toolCall","id":"c1","name":"read"}],"usage":{"input":100,"output":50,"cacheRead":900,"cacheWrite":0,"reasoning":30,"totalTokens":1050,"cost":{"total":0.25}}}}
{"type":"message","timestamp":"2026-08-02T09:00:11Z","message":{"role":"toolResult","toolName":"read","toolCallId":"c1"}}
{"type":"message","timestamp":"2026-08-02T09:00:20Z","message":{"role":"assistant","model":"gpt-5.6-terra","content":[{"type":"toolCall","id":"c2","name":"bash"}],"usage":{"input":40,"output":10,"cacheRead":1000,"reasoning":5,"totalTokens":1055,"cost":{"total":0.10}}}}
{"type":"message","timestamp":"2026-08-02T09:00:21Z","message":{"role":"toolResult","toolName":"bash","toolCallId":"c2"}}
`

const codexFixture = `{"timestamp":"2026-08-03T08:00:00Z","type":"session_meta","payload":{"id":"019f-codex","cwd":"/work/cli","originator":"Codex CLI"}}
{"timestamp":"2026-08-03T08:00:00Z","type":"turn_context","payload":{"cwd":"/work/cli","model":"gpt-5.5","effort":"high"}}
{"timestamp":"2026-08-03T08:00:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-1","root_turn_id":"root-1"}}
{"timestamp":"2026-08-03T08:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions\n<INSTRUCTIONS>be good</INSTRUCTIONS>"}]}}
{"timestamp":"2026-08-03T08:00:02Z","type":"event_msg","payload":{"type":"user_message","message":"cross-reference the pricing data"}}
{"timestamp":"2026-08-03T08:00:10Z","type":"response_item","payload":{"type":"function_call","name":"exec_command"}}
{"timestamp":"2026-08-03T08:00:11Z","type":"response_item","payload":{"type":"function_call","name":"exec_command"}}
{"timestamp":"2026-08-03T08:00:11Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","status":"failed"}}}
{"timestamp":"2026-08-03T08:00:12Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":600,"output_tokens":300,"reasoning_output_tokens":120,"total_tokens":1300},"last_token_usage":{"input_tokens":800,"cached_input_tokens":600,"output_tokens":50},"model_context_window":258400}}}
{"timestamp":"2026-08-03T08:00:20Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":2000,"cached_input_tokens":1200,"output_tokens":500,"reasoning_output_tokens":200,"total_tokens":2500},"last_token_usage":{"input_tokens":1500,"cached_input_tokens":1200,"output_tokens":80},"model_context_window":258400}}}
{"timestamp":"2026-08-03T08:00:21Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1","root_turn_id":"root-1","duration_ms":21000}}
{"timestamp":"2026-08-03T08:00:22Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-2","root_turn_id":"root-1"}}
{"timestamp":"2026-08-03T08:00:23Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-2","root_turn_id":"root-1","duration_ms":1000,"error":{"message":"retry failed"}}}
{"timestamp":"2026-08-03T08:00:24Z","type":"compacted","payload":{}}
`

const wtManifestFixture = `{
  "id": "wt-20260804-100000-abc123",
  "name": "docs 2026-08-04 11:00",
  "status": "active",
  "agent": "watchtower",
  "session_dir": "%s",
  "primary_workspace": "/work/docs",
  "created_at": "2026-08-04T10:00:00Z",
  "updated_at": "2026-08-04T11:00:00Z",
  "metadata": {"description": "fix the docs build"},
  "workspaces": [
    {"path": "/work/docs", "branch": "chore/bump-deps", "is_primary": true},
    {"path": "/work/wiki", "branch": "master", "is_primary": false}
  ]
}`

const wtTranscriptFixture = `{"ts":"2026-08-04T10:00:05Z","type":"user","content":"npm start crashes"}
{"ts":"2026-08-04T10:00:10Z","type":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{}"}}]}
{"ts":"2026-08-04T10:00:11Z","type":"tool_result","tool_name":"read_file"}
{"ts":"2026-08-04T10:00:12Z","type":"usage","model":"gpt-5.5","usage":{"prompt_tokens":5000,"completion_tokens":300},"context":true,"context_estimate":5200}
{"ts":"2026-08-04T10:00:20Z","type":"user","content":"try the other provider"}
{"ts":"2026-08-04T10:00:25Z","type":"tool_result","tool_name":"bash"}
{"ts":"2026-08-04T10:00:26Z","type":"usage","model":"gpt-5.5","usage":{"prompt_tokens":6000,"completion_tokens":400},"context":true,"context_estimate":6400}
`

// writeFixture writes a transcript, creating its directory.
func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// fakeHome lays out one session per agent under a temporary HOME, so the
// scanners run against known input instead of the developer's machine.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	write := func(path, content string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	write(filepath.Join(home, ".claude", "projects", "-work-coast", "aaaa-1111.jsonl"), claudeFixture)
	write(filepath.Join(home, ".pi", "agent", "sessions", "--work-mdview--", "2026-08-02T09-00-00-000Z_019f-pi.jsonl"), piFixture)
	write(filepath.Join(home, ".codex", "sessions", "2026", "08", "03", "rollout-2026-08-03T08-00-00-019f-codex.jsonl"), codexFixture)
	write(filepath.Join(home, ".codex", "session_index.jsonl"),
		`{"id":"019f-codex","thread_name":"Pricing cross-reference","updated_at":"2026-08-03T08:00:20Z"}`+"\n")

	wtDir := filepath.Join(home, ".watchtower", "sessions", "wt-20260804-100000-abc123")
	write(filepath.Join(wtDir, "manifest.json"), fmt.Sprintf(wtManifestFixture, wtDir))
	write(filepath.Join(wtDir, "run", "harness-transcript.jsonl"), wtTranscriptFixture)

	// An abandoned Claude session: cleared before anything was typed.
	write(filepath.Join(home, ".claude", "projects", "-work-coast", "empty-0000.jsonl"),
		`{"type":"user","cwd":"/work/coast","message":{"role":"user","content":"<command-name>/clear</command-name>"}}`+"\n")
	return home
}

func TestScanFindsEveryAgent(t *testing.T) {
	fakeHome(t)
	sessions := Scan()

	byAgent := map[Agent]*Session{}
	for _, s := range sessions {
		byAgent[s.Agent] = s
	}
	require.Len(t, sessions, 4, "expected exactly one session per agent, got %d", len(sessions))
	for _, agent := range Agents {
		assert.Contains(t, byAgent, agent)
	}
}

func TestScanSortsMostRecentFirst(t *testing.T) {
	fakeHome(t)
	sessions := Scan()
	require.NotEmpty(t, sessions)
	for i := 1; i < len(sessions); i++ {
		assert.False(t, sessions[i].LastUsed.After(sessions[i-1].LastUsed),
			"session %d is newer than the one before it", i)
	}
}

func TestScanDropsClearedSessions(t *testing.T) {
	fakeHome(t)
	for _, s := range Scan() {
		assert.NotContains(t, s.Path, "empty-0000", "a cleared session should not be listed")
	}
}

func TestIndexFields(t *testing.T) {
	fakeHome(t)
	byAgent := map[Agent]*Session{}
	for _, s := range Scan() {
		byAgent[s.Agent] = s
	}

	tests := []struct {
		agent       Agent
		id          string
		cwd         string
		dirLabel    string
		branch      string
		firstPrompt string
		title       string
	}{
		{
			agent: AgentClaude, id: "aaaa-1111", cwd: "/work/coast", dirLabel: "coast",
			branch: "fix/pricing", firstPrompt: "fix the flaky pricing test", title: "Fix flaky pricing test",
		},
		{
			agent: AgentPi, id: "019f-pi", cwd: "/work/mdview", dirLabel: "mdview",
			firstPrompt: "review PR 870",
		},
		{
			agent: AgentCodex, id: "019f-codex", cwd: "/work/cli", dirLabel: "cli",
			firstPrompt: "cross-reference the pricing data", title: "Pricing cross-reference",
		},
		{
			agent: AgentWatchtower, id: "wt-20260804-100000-abc123", cwd: "/work/docs", dirLabel: "docs",
			branch: "chore/bump-deps", firstPrompt: "fix the docs build", title: "docs 2026-08-04 11:00",
		},
	}
	for _, tc := range tests {
		t.Run(string(tc.agent), func(t *testing.T) {
			s := byAgent[tc.agent]
			require.NotNil(t, s)
			assert.Equal(t, tc.id, s.ID, "resume id")
			assert.Equal(t, tc.cwd, s.CWD)
			assert.Equal(t, tc.dirLabel, s.DirLabel())
			assert.Equal(t, tc.branch, s.Branch)
			assert.Equal(t, tc.firstPrompt, s.FirstPrompt)
			assert.Equal(t, tc.title, s.Title)
		})
	}
}

func TestLoadDetails(t *testing.T) {
	fakeHome(t)
	byAgent := map[Agent]*Session{}
	for _, s := range Scan() {
		byAgent[s.Agent] = s
	}

	tests := []struct {
		agent      Agent
		model      string
		prompts    []string
		tools      map[string]int
		usage      Usage
		ctxTokens  int
		ctxWindow  int
		costKnown  bool
		wantCostGT float64
	}{
		{
			agent:   AgentClaude,
			model:   "claude-opus-5",
			prompts: []string{"fix the flaky pricing test", "now run the whole suite"},
			tools:   map[string]int{"Bash": 2},
			// Usage is deduped per requestId, so the repeated req-1 lines count
			// once: 100+50 in, 200+25 out, 1000+2000 cache read, 500 cache write.
			usage:      Usage{Input: 150, Output: 225, CacheRead: 3000, CacheWrite: 500},
			ctxTokens:  2050,
			costKnown:  true,
			wantCostGT: 0,
		},
		{
			agent:   AgentPi,
			model:   "gpt-5.6-terra",
			prompts: []string{"review PR 870"},
			// One count per toolCall, not one per call plus one per result.
			tools:     map[string]int{"read": 1, "bash": 1},
			usage:     Usage{Input: 140, Output: 60, CacheRead: 1900, Reasoning: 35},
			ctxTokens: 1055,
			costKnown: true,
		},
		{
			agent:   AgentCodex,
			model:   "gpt-5.5",
			prompts: []string{"cross-reference the pricing data"},
			tools:   map[string]int{"exec_command": 2},
			// Only the final token_count matters, and cached tokens are split
			// out of the input count so the cheaper rate applies.
			usage:      Usage{Input: 800, Output: 500, CacheRead: 1200, Reasoning: 200},
			ctxTokens:  1500,
			ctxWindow:  258400,
			costKnown:  true,
			wantCostGT: 0,
		},
		{
			agent:     AgentWatchtower,
			model:     "gpt-5.5",
			prompts:   []string{"npm start crashes", "try the other provider"},
			tools:     map[string]int{"read_file": 1, "bash": 1},
			usage:     Usage{Input: 11000, Output: 700},
			ctxTokens: 6400,
			costKnown: true,
		},
	}
	for _, tc := range tests {
		t.Run(string(tc.agent), func(t *testing.T) {
			s := byAgent[tc.agent]
			require.NotNil(t, s)
			require.NoError(t, Load(s))

			assert.Equal(t, tc.model, s.Model)
			assert.Equal(t, tc.tools, s.Tools)
			assert.Equal(t, tc.ctxTokens, s.ContextTokens, "context tokens")
			assert.Equal(t, tc.ctxWindow, s.ContextWindow, "context window")
			assert.Equal(t, tc.usage.Input, s.Usage.Input, "input tokens")
			assert.Equal(t, tc.usage.Output, s.Usage.Output, "output tokens")
			assert.Equal(t, tc.usage.CacheRead, s.Usage.CacheRead, "cache read")
			assert.Equal(t, tc.usage.CacheWrite, s.Usage.CacheWrite, "cache write")
			assert.Equal(t, tc.usage.Reasoning, s.Usage.Reasoning, "reasoning")
			assert.Equal(t, tc.costKnown, s.Usage.CostKnown, "cost known")
			if tc.costKnown {
				assert.Greater(t, s.Usage.CostUSD, tc.wantCostGT, "cost")
			}

			texts := make([]string, len(s.Prompts))
			for i, p := range s.Prompts {
				texts[i] = p.Text
			}
			assert.Equal(t, tc.prompts, texts, "prompt history")
			assert.Equal(t, len(tc.prompts), s.Turns)
			assert.Equal(t, tc.prompts[len(tc.prompts)-1], s.LastPrompt())
			switch tc.agent {
			case AgentClaude:
				assert.Equal(t, map[string]int{"Bash": 1}, s.ToolFailures)
				assert.Equal(t, 1, s.Compactions)
				require.Len(t, s.TurnsDone, 1)
				assert.Equal(t, 66*time.Second, s.TurnsDone[0].Duration)
			case AgentCodex:
				assert.Equal(t, 1, s.Retries)
				assert.Equal(t, 1, s.Compactions)
				assert.Equal(t, map[string]int{"exec": 1}, s.ToolFailures)
				require.Len(t, s.TurnsDone, 2)
				assert.False(t, s.TurnsDone[0].Failed)
				assert.True(t, s.TurnsDone[1].Failed)
			}
		})
	}
}

func TestLoadIsIdempotent(t *testing.T) {
	fakeHome(t)
	var claude *Session
	for _, s := range Scan() {
		if s.Agent == AgentClaude {
			claude = s
		}
	}
	require.NotNil(t, claude)

	require.NoError(t, Load(claude))
	first := claude.Usage
	tools := len(claude.Tools)

	require.NoError(t, Load(claude))
	assert.Equal(t, first, claude.Usage, "usage should not double on a second load")
	assert.Len(t, claude.Tools, tools)
}

func TestLoadCopyLeavesTheOriginalAlone(t *testing.T) {
	fakeHome(t)
	var claude *Session
	for _, s := range Scan() {
		if s.Agent == AgentClaude {
			claude = s
		}
	}
	require.NotNil(t, claude)

	loaded, err := LoadCopy(claude)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.True(t, loaded.Loaded)
	assert.NotEmpty(t, loaded.Prompts)
	assert.False(t, claude.Loaded, "the indexed session must stay untouched")
	assert.Empty(t, claude.Prompts)
	assert.Empty(t, claude.Tools)
}

func TestMissingStoresAreNotAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	assert.Empty(t, Scan(), "an empty home should yield no sessions and no panic")
}

func TestTopToolsRanksByCountThenName(t *testing.T) {
	s := &Session{Tools: map[string]int{"Bash": 5, "Read": 5, "Edit": 9, "Write": 1}}
	got := s.TopTools(3)
	assert.Equal(t, []ToolCount{{"Edit", 9}, {"Bash", 5}, {"Read", 5}}, got)
	assert.Len(t, s.TopTools(0), 4, "0 means every tool")
}

func TestLabelFallsBackToTitle(t *testing.T) {
	assert.Equal(t, "typed", (&Session{FirstPrompt: "typed", Title: "generated"}).Label())
	assert.Equal(t, "generated", (&Session{Title: "generated"}).Label())
	assert.Equal(t, "(no prompt)", (&Session{}).Label())
}

func TestScanRereadsCodexTitles(t *testing.T) {
	// These were cached for the life of the process, so a rescan never showed a
	// renamed or newly created codex thread.
	home := fakeHome(t)
	require.NotEmpty(t, Scan())

	writeFixture(t, filepath.Join(home, ".codex", "session_index.jsonl"),
		`{"id":"019f-codex","thread_name":"Renamed thread"}`+"\n")

	var found *Session
	for _, s := range Scan() {
		if s.Agent == AgentCodex {
			found = s
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, "Renamed thread", found.Title)
}

// subagentFixture is a named agent's run: ade calls @staff-reviewer, and Claude
// Code gives it a top-level transcript of its own in the same project.
const subagentFixture = `{"type":"agent-setting","agentSetting":"staff-reviewer","sessionId":"bbbb-2222"}
{"type":"user","timestamp":"2026-08-01T11:00:00Z","cwd":"/work/coast","gitBranch":"fix/pricing","message":{"role":"user","content":"You are @staff-reviewer, called into a conversation in ade."}}
{"type":"assistant","timestamp":"2026-08-01T11:00:05Z","requestId":"req-9","message":{"id":"msg-9","role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"three findings"}],"usage":{"input_tokens":10,"output_tokens":20}}}
`

func TestANamedAgentsRunIsMarkedAsOne(t *testing.T) {
	// Every reviewer ade calls gets its own transcript beside the conversation
	// it was called from, so without this each one became a "recent session".
	home := fakeHome(t)
	require.NoError(t, os.WriteFile(
		filepath.Join(home, ".claude", "projects", "-work-coast", "bbbb-2222.jsonl"),
		[]byte(subagentFixture), 0o600))

	var run, conversation *Session
	for _, s := range Scan() {
		switch s.ID {
		case "bbbb-2222":
			run = s
		case "aaaa-1111":
			conversation = s
		}
	}
	require.NotNil(t, run, "the run is still scanned; what it spent still counts")
	assert.Equal(t, "staff-reviewer", run.Subagent)

	require.NotNil(t, conversation)
	assert.Empty(t, conversation.Subagent, "the conversation it was called from is not a subagent run")
}
