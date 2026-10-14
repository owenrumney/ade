package transcripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWindowTakesOnlyTheRequestsInsideIt(t *testing.T) {
	fakeHome(t)
	// Between the fixture's two Claude requests: the first is outside the
	// window, the second inside, and the session spans the boundary.
	since := time.Date(2026, 8, 1, 10, 0, 30, 0, time.UTC)
	byDir := map[string]Share{}
	for _, share := range InWindow(Scan(), since) {
		byDir[share.Dir] = share
	}

	// The window runs from since to now, so later fixtures count too; what is
	// being checked is that the request before it does not.
	require.Contains(t, byDir, "coast")
	assert.Equal(t, 50+25+2000, byDir["coast"].Tokens, "the earlier request is not in this window")
	assert.Equal(t, 1, byDir["coast"].Sessions)
}

func TestAWindowSharesOutWhatSeveralCheckoutsSpent(t *testing.T) {
	fakeHome(t)
	shares := InWindow(Scan(), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))

	require.NotEmpty(t, shares)
	total := 0.0
	for _, s := range shares {
		total += s.Percent
		assert.Positivef(t, s.Tokens, "%s is listed having spent nothing", s.Dir)
	}
	assert.InDelta(t, 100, total, 0.001, "the shares are of one window")
	for i := 1; i < len(shares); i++ {
		assert.GreaterOrEqual(t, shares[i-1].Tokens, shares[i].Tokens, "biggest first")
	}
}

func TestAWindowNothingRanInIsEmpty(t *testing.T) {
	fakeHome(t)
	assert.Empty(t, InWindow(Scan(), time.Now()))
}

func TestCodexChargesAreTheDifferenceBetweenRunningTotals(t *testing.T) {
	// token_count carries the total so far, so the second event's charge is
	// what it added, not what it reported.
	fakeHome(t)
	var codex *Session
	for _, s := range Scan() {
		if s.Agent == AgentCodex {
			codex = s
		}
	}
	require.NotNil(t, codex)
	require.NoError(t, Load(codex))

	require.Len(t, codex.Charges, 2)
	assert.Equal(t, 400+300+600, codex.Charges[0].Usage.Total())
	assert.Equal(t, 400+200+600, codex.Charges[1].Usage.Total())

	// And they still add up to the total the session reports.
	sum := 0
	for _, c := range codex.Charges {
		sum += c.Usage.Total()
	}
	assert.Equal(t, codex.Usage.Total(), sum)
}

func TestClaudeChargesAreCountedOncePerRequest(t *testing.T) {
	// One API response spans several lines, each repeating the same usage.
	fakeHome(t)
	var claude *Session
	for _, s := range Scan() {
		if s.Agent == AgentClaude && s.DirLabel() == "coast" {
			claude = s
		}
	}
	require.NotNil(t, claude)
	require.NoError(t, Load(claude))

	require.Len(t, claude.Charges, 2, "the repeated line is not a second charge")
	assert.Equal(t, 100+200+1000+500, claude.Charges[0].Usage.Total())
}

func TestCostSinceIsPricedAndStopsAtTheWindow(t *testing.T) {
	fakeHome(t)
	// claude-opus-5 at $5 in, $25 out, $6.25 cache write, $0.50 cache read per million.
	first := (100*5.0 + 200*25.0 + 500*6.25 + 1000*0.5) / 1e6
	second := (50*5.0 + 25*25.0 + 2000*0.5) / 1e6

	all, known := CostSince(Scan(), AgentClaude, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	require.True(t, known)
	assert.InDelta(t, first+second, all, 1e-12, "only Claude's requests, each at its rate")
	later, _ := CostSince(Scan(), AgentClaude, time.Date(2026, 8, 1, 10, 0, 30, 0, time.UTC))
	assert.InDelta(t, second, later, 1e-12, "the request before the window is not in it")
	none, _ := CostSince(Scan(), AgentClaude, time.Now())
	assert.Zero(t, none)
}

const mixedModels = `{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/mixed","message":{"role":"user","content":"go"}}
{"type":"assistant","timestamp":"2026-08-01T10:00:05Z","requestId":"a","message":{"id":"a","role":"assistant","model":"claude-opus-5","content":[],"usage":{"input_tokens":1000000,"output_tokens":0}}}
{"type":"assistant","timestamp":"2026-08-01T10:00:06Z","requestId":"b","message":{"id":"b","role":"assistant","model":"claude-haiku-4-5","content":[],"usage":{"input_tokens":1000000,"output_tokens":0}}}
`

func TestCostSincePricesEachRequestAtItsOwnModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".claude", "projects", "-work-mixed", "m.jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(mixedModels), 0o600))

	got, known := CostSince(Scan(), AgentClaude, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	require.True(t, known)
	assert.InDelta(t, 5.0+1.0, got, 1e-9, "opus then haiku, not two of the last")

	unpriced := strings.Replace(mixedModels, "claude-haiku-4-5", "no-such-model", 1)
	require.NoError(t, os.WriteFile(path, []byte(unpriced), 0o600))
	_, known = CostSince(Scan(), AgentClaude, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	assert.False(t, known)
}
