package transcripts

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummariseGroupsAndTotals(t *testing.T) {
	fakeHome(t)
	sessions := Scan()
	spend := Summarise(sessions)

	assert.Equal(t, len(sessions), spend.Total.Sessions, "every session should be accounted for")
	assert.Zero(t, spend.Failed)
	assert.Positive(t, spend.Total.Cost)
	assert.Positive(t, spend.Total.Tokens)

	// One session per agent in the fixture, so one group each.
	assert.Len(t, spend.ByAgent, len(Agents))
	byAgent := map[string]Group{}
	for _, group := range spend.ByAgent {
		byAgent[group.Key] = group
	}
	for _, agent := range Agents {
		require.Contains(t, byAgent, string(agent))
		assert.Equal(t, 1, byAgent[string(agent)].Sessions)
	}

	// The group totals must add back up to the overall total.
	var dirCost, agentCost float64
	for _, group := range spend.ByDir {
		dirCost += group.Cost
	}
	for _, group := range spend.ByAgent {
		agentCost += group.Cost
	}
	assert.InDelta(t, spend.Total.Cost, dirCost, 0.0001)
	assert.InDelta(t, spend.Total.Cost, agentCost, 0.0001)
}

func TestSummariseSortsDirsByCostAndMonthsByDate(t *testing.T) {
	fakeHome(t)
	spend := Summarise(Scan())

	for i := 1; i < len(spend.ByDir); i++ {
		assert.GreaterOrEqual(t, spend.ByDir[i-1].Cost, spend.ByDir[i].Cost)
	}
	for i := 1; i < len(spend.ByMonth); i++ {
		assert.Less(t, spend.ByMonth[i-1].Key, spend.ByMonth[i].Key, "months read chronologically")
	}
}

func TestSummariseCountsUnpricedModels(t *testing.T) {
	home := fakeHome(t)
	writeFixture(t, filepath.Join(home, ".claude", "projects", "-work-odd", "odd-0001.jsonl"),
		`{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/odd","message":{"role":"user","content":"hello"}}
{"type":"assistant","timestamp":"2026-08-01T10:00:01Z","requestId":"r1","message":{"id":"m1","role":"assistant","model":"some-unknown-model","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":100,"output_tokens":50}}}
`)
	spend := Summarise(Scan())
	assert.Equal(t, 1, spend.Unpriced, "an unpriced model must be reported, not read as free")

	var odd Group
	for _, group := range spend.ByDir {
		if group.Key == "odd" {
			odd = group
		}
	}
	assert.Equal(t, 1, odd.Unpriced)
	assert.Zero(t, odd.Cost)
	assert.Equal(t, 150, odd.Tokens, "tokens still count even when the price is unknown")
}

func TestSummariseSurvivesAMissingTranscript(t *testing.T) {
	fakeHome(t)
	sessions := append(Scan(), &Session{
		Agent:    AgentClaude,
		Path:     filepath.Join(t.TempDir(), "gone.jsonl"),
		CWD:      "/work/gone",
		LastUsed: mustParseMonth(t, "2026-08"),
	})
	spend := Summarise(sessions)
	assert.Zero(t, spend.Failed, "a session that never ran is empty, not a failure")
	assert.Equal(t, len(sessions), spend.Total.Sessions)
}

func TestSummariseEmptyInput(t *testing.T) {
	spend := Summarise(nil)
	assert.Zero(t, spend.Total.Sessions)
	assert.Empty(t, spend.ByDir)
	assert.Empty(t, spend.ByMonth)
}

func TestStatisticsGroupsModelValueByRequestDay(t *testing.T) {
	home := fakeHome(t)
	path := filepath.Join(home, ".claude", "projects", "-work-daily", "daily.jsonl")
	writeFixture(t, path, `{"type":"user","timestamp":"2026-10-03T10:00:00Z","cwd":"/work/daily","message":{"role":"user","content":"first"}}
{"type":"assistant","timestamp":"2026-10-03T10:01:00Z","requestId":"a","message":{"id":"a","role":"assistant","model":"claude-opus-5","content":[],"usage":{"input_tokens":1000,"output_tokens":100}}}
{"type":"assistant","timestamp":"2026-10-04T09:01:00Z","requestId":"b","message":{"id":"b","role":"assistant","model":"claude-sonnet-5","content":[],"usage":{"input_tokens":2000,"output_tokens":200}}}
`)

	session := ClaudeSession(path)
	require.NotNil(t, session)
	loaded, err := LoadCopy(session)
	require.NoError(t, err)
	require.Len(t, loaded.Charges, 2)

	stats := Statistics([]*Session{session})
	require.Len(t, stats.ByDayModel["2026-10-03"], 1)
	require.Len(t, stats.ByDayModel["2026-10-04"], 1)
	assert.Equal(t, "claude-opus-5", stats.ByDayModel["2026-10-03"][0].Key)
	assert.Equal(t, "claude-sonnet-5", stats.ByDayModel["2026-10-04"][0].Key)
	assert.Positive(t, stats.ByDayModel["2026-10-03"][0].Cost)
	assert.Positive(t, stats.ByDayModel["2026-10-04"][0].Cost)
}

func mustParseMonth(t *testing.T, month string) time.Time {
	t.Helper()
	at, err := time.Parse("2006-01", month)
	require.NoError(t, err)
	return at
}
