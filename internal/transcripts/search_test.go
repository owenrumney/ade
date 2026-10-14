package transcripts

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchFindsTextAnywhereInTheTranscript(t *testing.T) {
	fakeHome(t)
	sessions := Scan()

	tests := []struct {
		name      string
		query     string
		wantAgent Agent
		wantRole  Role
	}{
		// The claude fixture's opening prompt.
		{"a prompt", "flaky pricing", AgentClaude, RoleUser},
		// Mid-conversation, which the list filter cannot reach.
		{"an agent reply", "on it", AgentClaude, RoleAgent},
		{"a watchtower prompt", "other provider", AgentWatchtower, RoleUser},
		{"a codex prompt", "cross-reference", AgentCodex, RoleUser},
		{"a pi prompt", "PR 870", AgentPi, RoleUser},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hits := Search(sessions, tc.query)
			require.NotEmpty(t, hits, "expected a hit for %q", tc.query)

			var found *Hit
			for i := range hits {
				if hits[i].Session.Agent == tc.wantAgent {
					found = &hits[i]
				}
			}
			require.NotNil(t, found, "no %s hit for %q", tc.wantAgent, tc.query)
			require.NotEmpty(t, found.Excerpts)
			assert.Positive(t, found.Matches)

			roles := map[Role]bool{}
			for _, e := range found.Excerpts {
				roles[e.Role] = true
			}
			assert.True(t, roles[tc.wantRole], "expected a %s excerpt, got %v", tc.wantRole, roles)
		})
	}
}

func TestSearchIsCaseInsensitive(t *testing.T) {
	fakeHome(t)
	sessions := Scan()
	lower := Search(sessions, "flaky pricing")
	upper := Search(sessions, "FLAKY PRICING")
	require.NotEmpty(t, lower)
	assert.Len(t, upper, len(lower))
}

func TestSearchFindsToolArguments(t *testing.T) {
	home := fakeHome(t)
	writeFixture(t, filepath.Join(home, ".claude", "projects", "-work-tools", "tool-0001.jsonl"),
		`{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/tools","message":{"role":"user","content":"have a look"}}
{"type":"assistant","timestamp":"2026-08-01T10:00:01Z","requestId":"r1","message":{"id":"m1","role":"assistant","model":"claude-opus-5","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./internal/pricingtable"}}],"usage":{"input_tokens":1,"output_tokens":1}}}
`)
	hits := Search(Scan(), "pricingtable")
	require.Len(t, hits, 1, "a path only present in tool arguments should still be findable")
	require.NotEmpty(t, hits[0].Excerpts)
	assert.Equal(t, RoleTool, hits[0].Excerpts[0].Role)
}

func TestSearchRejectsMatchesOnlyInMachinery(t *testing.T) {
	home := fakeHome(t)
	// The needle appears in the raw JSON keys and the session id, never in text.
	writeFixture(t, filepath.Join(home, ".claude", "projects", "-work-noise", "cacheread-0001.jsonl"),
		`{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/noise","message":{"role":"user","content":"nothing to see"}}
{"type":"assistant","timestamp":"2026-08-01T10:00:01Z","requestId":"r1","message":{"id":"m1","role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"fine"}],"usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":5}}}
`)
	for _, hit := range Search(Scan(), "cache_read_input_tokens") {
		assert.NotContains(t, hit.Session.Path, "cacheread-0001",
			"a hit only in the raw JSON should be rejected by the confirm pass")
	}
}

func TestSearchOrdersByMatchCount(t *testing.T) {
	home := fakeHome(t)
	writeFixture(t, filepath.Join(home, ".claude", "projects", "-work-many", "many-0001.jsonl"),
		`{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/many","message":{"role":"user","content":"widget widget"}}
{"type":"assistant","timestamp":"2026-08-01T10:00:01Z","requestId":"r1","message":{"id":"m1","role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"widget"}],"usage":{"input_tokens":1,"output_tokens":1}}}
`)
	writeFixture(t, filepath.Join(home, ".claude", "projects", "-work-few", "few-0001.jsonl"),
		`{"type":"user","timestamp":"2026-08-02T10:00:00Z","cwd":"/work/few","message":{"role":"user","content":"widget"}}
`)
	hits := Search(Scan(), "widget")
	require.Len(t, hits, 2)
	assert.Contains(t, hits[0].Session.Path, "many-0001", "more matches sorts first")
	assert.Greater(t, hits[0].Matches, hits[1].Matches)
}

func TestSearchEmptyQueryFindsNothing(t *testing.T) {
	fakeHome(t)
	assert.Empty(t, Search(Scan(), ""))
	assert.Empty(t, Search(Scan(), "   "))
}

func TestSearchNoMatch(t *testing.T) {
	fakeHome(t)
	assert.Empty(t, Search(Scan(), "zzzznotpresentanywhere"))
}

func TestExcerptWindowsTheMatch(t *testing.T) {
	body := "aaaaaaaaaa needle bbbbbbbbbb"
	assert.Equal(t, body, excerpt(body, "needle", 100), "short bodies are left whole")

	long := ""
	for range 40 {
		long += "padding "
	}
	got := excerpt(long+"needle"+long, "needle", 40)
	assert.Contains(t, got, "needle")
	assert.LessOrEqual(t, len(got), 48, "should trim to a window around the match")
}

func TestScanTextsCoversEveryAgent(t *testing.T) {
	fakeHome(t)
	for _, s := range Scan() {
		t.Run(string(s.Agent), func(t *testing.T) {
			roles := map[Role]int{}
			require.NoError(t, ScanTexts(s, func(text Text) error {
				assert.NotEmpty(t, text.Body, "empty bodies should be skipped")
				roles[text.Role]++
				return nil
			}))
			assert.Positive(t, roles[RoleUser], "expected at least one prompt")
		})
	}
}

func TestScanTextsMissingTranscriptIsNotAnError(t *testing.T) {
	fakeHome(t)
	s := &Session{Agent: AgentClaude, Path: filepath.Join(t.TempDir(), "gone.jsonl")}
	calls := 0
	require.NoError(t, ScanTexts(s, func(Text) error { calls++; return nil }))
	assert.Zero(t, calls)
}
