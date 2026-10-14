package dashboard

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/limits"
	"github.com/owenrumney/ade/internal/transcripts"
)

var now = time.Date(2026, 10, 4, 15, 46, 12, 0, time.UTC)

// read is when ade took the reading, which is earlier than when it sends it.
var read = now.Add(-90 * time.Second)

func state() limits.State {
	return limits.State{
		Claude: &limits.Limits{
			Provider:  "claude",
			CheckedAt: read,
			Windows: []limits.Window{
				{Name: "5h", Percent: 68.2, ResetsAt: now.Add(2 * time.Hour)},
				{Name: "7d", Percent: 24, ResetsAt: now.Add(72 * time.Hour)},
			},
		},
		Codex: &limits.Limits{
			Provider:  "codex",
			CheckedAt: read,
			// Codex's second window is derived and is not always 7d, which is
			// why the windows go through as they are.
			Windows: []limits.Window{{Name: "5h", Percent: 12}, {Name: "30d", Percent: 4}},
		},
	}
}

func TestASubscriptionTileCarriesTheWindowsAsTheyAre(t *testing.T) {
	tiles := Subscriptions(state(), Accounts{Claude: "personal", Codex: "work"}, "mbp")
	require.Len(t, tiles, 2)

	claude := tiles[0]
	assert.Equal(t, "claude.personal", claude.Source, "the provider qualifies the label")
	assert.Equal(t, TypeSubscription, claude.Type)
	assert.Equal(t, "Claude · personal", claude.Title)
	assert.Equal(t, "subscriptions", claude.Group)
	assert.Equal(t, Latest, claude.Reduce, "the window belongs to the account")
	assert.Equal(t, "mbp", claude.Host)
	assert.Equal(t, read, claude.At, "observed_at is the read time, not the send time")
	assert.Equal(t, "claude", claude.Data["provider"])
	// Every window, not a fixed 5h/7d pair — and resets_at, which is what
	// the screen reads. limits.Window marshals it as resetsAt.
	assert.Equal(t, []map[string]any{
		{"name": "5h", "percent": 68.2, "resets_at": "2026-10-04T17:46:12Z"},
		{"name": "7d", "percent": 24.0, "resets_at": "2026-10-07T15:46:12Z"},
	}, claude.Data["windows"])

	assert.Equal(t, "codex.work", tiles[1].Source)
	// Codex's second window is derived and is not always 7d, and a window
	// with no reset time says nothing rather than 1970.
	assert.Equal(t, []map[string]any{
		{"name": "5h", "percent": 12.0},
		{"name": "30d", "percent": 4.0},
	}, tiles[1].Data["windows"])
}

func TestAProviderWithNoLabelIsNotReportedFromThisMachine(t *testing.T) {
	tests := []struct {
		name string
		who  Accounts
		want []string
	}{
		{"neither", Accounts{}, nil},
		{"claude only", Accounts{Claude: "personal"}, []string{"claude.personal"}},
		{"codex only", Accounts{Codex: "work"}, []string{"codex.work"}},
		{"both", Accounts{Claude: "personal", Codex: "work"}, []string{"claude.personal", "codex.work"}},
		// One label for both is the ordinary case: two subscriptions, one
		// person. A bare label made them one tile that overwrote itself.
		{"one label", Accounts{Claude: "personal", Codex: "personal"}, []string{"claude.personal", "codex.personal"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, tile := range Subscriptions(state(), tt.who, "mbp") {
				got = append(got, tile.Source)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAProviderWithNoReadingIsNotReportedEither(t *testing.T) {
	who := Accounts{Claude: "personal", Codex: "work"}
	assert.Empty(t, Subscriptions(limits.State{}, who, "mbp"), "no reading is nothing to say")

	empty := limits.State{Claude: &limits.Limits{Provider: "claude"}}
	assert.Empty(t, Subscriptions(empty, who, "mbp"), "a reading with no windows is not one")
}

func stats() transcripts.Stats {
	return transcripts.Stats{
		Sessions: 40,
		Tools:    318,
		Usage: transcripts.Usage{
			Input: 1_000, Output: 200, CacheRead: 8_000, CacheWrite: 800,
		},
		ByDay: []transcripts.Group{
			{Key: "2026-10-01", Cost: 1.5},
			{Key: "2026-10-04", Cost: 4.25, Unpriced: 3},
		},
		ByDayModel: map[string][]transcripts.Group{
			"2026-10-04": {
				{Key: "claude-opus-5", Cost: 3.5},
				{Key: "gpt-5.6-terra", Cost: 0.75},
			},
		},
		ByDayModelTurns: map[string][]transcripts.Group{
			"2026-10-04": {{Key: "claude-opus-5", Sessions: 2}, {Key: "gpt-5.6-terra", Sessions: 1}},
		},
		ByToolFailure: []transcripts.Group{{Key: "Bash", Sessions: 2}},
		Failed:        2, Retries: 1, Compactions: 3, Interventions: 4,
		TurnDurations: []float64{3, 20, 200}, SessionDurations: []float64{20, 700},
		Contexts: []transcripts.ContextStat{{ID: "session-1", Headroom: 25}},
		// Sessions, not Requests: addCount totals a tool's calls into
		// Sessions, and reading Requests drew five zeros in tool order.
		ByTool: []transcripts.Group{
			{Key: "Read", Sessions: 90},
			{Key: "Bash", Sessions: 120},
			{Key: "Edit", Sessions: 70},
			{Key: "Grep", Sessions: 40},
			{Key: "Write", Sessions: 30},
			{Key: "Glob", Sessions: 10},
		},
	}
}

func TestTheUsageTilesAreSummedAcrossMachines(t *testing.T) {
	tiles := Usage(stats(), "mbp", now, SparkDays)
	require.Len(t, tiles, 20)

	by := map[string]Tile{}
	for _, tile := range tiles {
		by[tile.Source] = tile
		assert.Equal(t, "mbp", tile.Host)
		assert.Equal(t, now, tile.At)
	}

	// Each machine counts its own transcripts and the sets are disjoint.
	assert.Equal(t, Sum, by["value.today"].Reduce)
	assert.Equal(t, Sum, by["value.by_model"].Reduce)
	assert.Equal(t, Sum, by["value.by_machine"].Reduce)
	assert.Equal(t, Sum, by["tokens.cacheread"].Reduce)
	assert.Equal(t, Sum, by["tokens.total"].Reduce)
	assert.Equal(t, Sum, by["turn.duration"].Reduce)
	assert.Equal(t, Sum, by["turns.completed"].Reduce)
	assert.Equal(t, Sum, by["tools.today"].Reduce)
	assert.Equal(t, Sum, by["tools.by_machine"].Reduce)
	assert.Equal(t, Sum, by["tools.top"].Reduce)
	assert.Equal(t, Merge, by["value.30d"].Reduce, "a series is added bucket by bucket")

	// Not "spend": it is priced against the API card on a subscription.
	assert.Equal(t, "API value today", by["value.today"].Title)
	assert.Equal(t, TypeMoney, by["value.today"].Type)
	// value and unit, which is what the screen reads: amount and currency
	// drew a zero for an afternoon.
	assert.InDelta(t, 4.25, by["value.today"].Data["value"], 0.001)
	assert.Equal(t, "USD", by["value.today"].Data["unit"])
	// Always said, not only when something is unpriced: a bare number on a
	// wall reads as money somebody was charged.
	assert.Equal(t, "rate-card value, not billed · 3 unpriced", by["value.today"].Data["detail"])
	assert.Equal(t, []map[string]any{
		{"label": "claude-opus-5", "value": 3.5},
		{"label": "gpt-5.6-terra", "value": 0.75},
	}, by["value.by_model"].Data["items"])
	assert.Equal(t, []map[string]any{{"label": "mbp", "value": 4.25}}, by["value.by_machine"].Data["items"])
	// The machine is the label, not the host field: a dashboard that sums the
	// hosts keeps a list item's label and drops the host.
	assert.Equal(t, []map[string]any{{"label": "mbp", "value": 318}}, by["tools.by_machine"].Data["items"])
	assert.Equal(t, 8_000, by["tokens.cacheread"].Data["value"])
	assert.Equal(t, 10_000, by["tokens.total"].Data["value"])
	assert.Equal(t, 3, by["turns.completed"].Data["value"])
	assert.Equal(t, []map[string]any{{"label": "mbp/session-1", "value": float64(25)}}, by["context.headroom"].Data["items"])

	assert.Equal(t, 318, by["tools.today"].Data["value"])
}

func TestTheToolListIsTheBusiestFive(t *testing.T) {
	tiles := Usage(stats(), "mbp", now, SparkDays)
	var list Tile
	for _, tile := range tiles {
		if tile.Source == "tools.top" {
			list = tile
		}
	}
	assert.Equal(t, []map[string]any{
		{"label": "Bash", "value": 120},
		{"label": "Read", "value": 90},
		{"label": "Edit", "value": 70},
		{"label": "Grep", "value": 40},
		{"label": "Write", "value": 30},
	}, list.Data["items"], "most-used first, and Glob does not make the cut")
}

func TestTheSeriesHasADayForEveryDayIncludingTheQuietOnes(t *testing.T) {
	tiles := Usage(stats(), "mbp", now, 5)
	var spark Tile
	for _, tile := range tiles {
		if tile.Source == "value.30d" {
			spark = tile
		}
	}
	// Oldest first, with the empty days in it: the server adds the machines
	// bucket by bucket, so the buckets have to line up.
	// key, not day: the axis labels and the bar tooltips read points[].key.
	assert.Equal(t, []map[string]any{
		{"key": "2026-09-30", "value": 0.0},
		{"key": "2026-10-01", "value": 1.5},
		{"key": "2026-10-02", "value": 0.0},
		{"key": "2026-10-03", "value": 0.0},
		{"key": "2026-10-04", "value": 4.25},
	}, spark.Data["points"])
	assert.Equal(t, "USD", spark.Data["unit"], "or the axis reads 158 rather than $158.00")
}

func TestTheSeriesLengthIsWhatTheBackfillAsksFor(t *testing.T) {
	for _, days := range []int{1, 7, 365} {
		tiles := Usage(stats(), "mbp", now, days)
		for _, tile := range tiles {
			if tile.Source == "value.30d" {
				assert.Len(t, tile.Data["points"], days)
			}
		}
	}
	// Zero is a tick's worth rather than an empty chart.
	for _, tile := range Usage(stats(), "mbp", now, 0) {
		if tile.Source == "value.30d" {
			assert.Len(t, tile.Data["points"], SparkDays)
		}
	}
}

func TestADayWithNothingOnItReportsZeroRatherThanNothing(t *testing.T) {
	quiet := now.AddDate(0, 0, 1)
	tiles := Usage(stats(), "mbp", quiet, SparkDays)
	for _, tile := range tiles {
		if tile.Source == "value.today" {
			assert.InDelta(t, 0.0, tile.Data["value"], 0.001)
			assert.Equal(t, "rate-card value, not billed", tile.Data["detail"],
				"what the number is does not depend on there being a gap in it")
		}
	}
}

// Nothing here may attribute cost to a subscription: a transcript does not
// record which account produced it.
func TestNoUsageTileNamesAnAccount(t *testing.T) {
	for _, tile := range Usage(stats(), "mbp", now, SparkDays) {
		assert.NotContains(t, tile.Data, "provider")
		assert.NotContains(t, tile.Data, "account")
		assert.NotEqual(t, Latest, tile.Reduce)
	}
}

// The brief gave a worked example for a subscription and said only that the
// other types were "validated server-side". Inventing their field names drew
// a screen of zeros, so every shape is pinned here: the renderer reads
// data.value, data.items[].value and data.points[].key, and nothing else.
func TestTheWireShapeOfEveryTileType(t *testing.T) {
	tiles := Subscriptions(state(), Accounts{Claude: "personal"}, "mbp")
	tiles = append(tiles, Usage(stats(), "mbp", now, 2)...)

	want := map[string]string{
		"claude.personal": `{
			"type":"subscription","title":"Claude · personal","group":"subscriptions",
			"order":10,"reduce":"latest","host":"mbp","observed_at":"2026-10-04T15:44:42Z",
			"data":{"provider":"claude","windows":[
				{"name":"5h","percent":68.2,"resets_at":"2026-10-04T17:46:12Z"},
				{"name":"7d","percent":24,"resets_at":"2026-10-07T15:46:12Z"}]}}`,
		"value.today": `{
			"type":"money","title":"API value today","group":"today",
			"order":10,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"value":4.25,"unit":"USD","detail":"rate-card value, not billed · 3 unpriced"}}`,
		"value.by_model": `{
			"type":"list","title":"API value today, by model","group":"breakdown",
			"order":10,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"items":[
				{"label":"claude-opus-5","value":3.5},
				{"label":"gpt-5.6-terra","value":0.75}]}}`,
		"value.by_machine": `{
			"type":"list","title":"API value today, by machine","group":"breakdown",
			"order":20,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"items":[{"label":"mbp","value":4.25}]}}`,
		"tools.by_machine": `{
			"type":"list","title":"Tool calls, by machine","group":"breakdown",
			"order":25,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"items":[{"label":"mbp","value":318}]}}`,
		"tokens.cacheread": `{
			"type":"counter","title":"Cached input tokens","group":"breakdown",
			"order":30,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"value":8000}}`,
		"tokens.total": `{
			"type":"counter","title":"Total tokens","group":"breakdown",
			"order":40,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"value":10000}}`,
		"turn.duration": `{
			"type":"list","title":"Turn duration buckets","group":"performance",
			"order":10,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"items":[{"label":"le_5","value":1},{"label":"le_30","value":2},{"label":"le_120","value":2},{"label":"le_600","value":3},{"label":"le_inf","value":3}]}}`,
		"turns.failed": `{
			"type":"counter","title":"Failed turns, all time","group":"performance",
			"order":20,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z","data":{"value":2}}`,
		"turns.retried": `{
			"type":"counter","title":"Retried turns, all time","group":"performance",
			"order":30,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z","data":{"value":1}}`,
		"turns.completed": `{
			"type":"counter","title":"Completed turns today","group":"performance",
			"order":40,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z","data":{"value":3}}`,
		"turns.by_model": `{
			"type":"list","title":"Completed turns today, by model","group":"performance",
			"order":50,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"items":[{"label":"claude-opus-5","value":2},{"label":"gpt-5.6-terra","value":1}]}}`,
		"context.headroom": `{
			"type":"list","title":"Context headroom by session","group":"performance",
			"order":60,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"items":[{"label":"mbp/session-1","value":25}]}}`,
		"tools.calls": `{
			"type":"list","title":"Tool calls, all time","group":"performance",
			"order":70,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"items":[{"label":"Read","value":90},{"label":"Bash","value":120},{"label":"Edit","value":70},{"label":"Grep","value":40},{"label":"Write","value":30},{"label":"Glob","value":10}]}}`,
		"tools.failures": `{
			"type":"list","title":"Tool failures, all time","group":"performance",
			"order":80,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"items":[{"label":"Bash","value":2}]}}`,
		"compactions": `{
			"type":"counter","title":"Compactions, all time","group":"performance",
			"order":90,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z","data":{"value":3}}`,
		"session.duration": `{
			"type":"list","title":"Session duration buckets","group":"performance",
			"order":100,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"items":[{"label":"le_300","value":1},{"label":"le_1800","value":2},{"label":"le_7200","value":2},{"label":"le_28800","value":2},{"label":"le_inf","value":2}]}}`,
		"human.interventions": `{
			"type":"counter","title":"Human follow-up turns, all time","group":"performance",
			"order":110,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z","data":{"value":4}}`,
		"tools.today": `{
			"type":"counter","title":"Tool calls, all time","group":"today",
			"order":30,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"value":318}}`,
		"tools.top": `{
			"type":"list","title":"Busiest tools","group":"today",
			"order":40,"reduce":"sum","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"items":[
				{"label":"Bash","value":120},{"label":"Read","value":90},
				{"label":"Edit","value":70},{"label":"Grep","value":40},
				{"label":"Write","value":30}]}}`,
		"value.30d": `{
			"type":"sparkline","title":"API value, by day","group":"history",
			"order":10,"reduce":"merge","host":"mbp","observed_at":"2026-10-04T15:46:12Z",
			"data":{"unit":"USD","points":[
				{"key":"2026-10-03","value":0},{"key":"2026-10-04","value":4.25}]}}`,
	}
	require.Len(t, tiles, len(want))

	for _, tile := range tiles {
		t.Run(tile.Source, func(t *testing.T) {
			shape, ok := want[tile.Source]
			require.True(t, ok, "a new tile source needs its shape pinned here")
			sent, err := json.Marshal(tile)
			require.NoError(t, err)
			assert.JSONEq(t, shape, string(sent))
		})
	}
}

func TestLiveSessionsIsASummedMachineCount(t *testing.T) {
	tile := LiveSessions(3, "mbp", now)
	assert.Equal(t, "sessions.live", tile.Source)
	assert.Equal(t, TypeCounter, tile.Type)
	assert.Equal(t, Sum, tile.Reduce)
	assert.Equal(t, "mbp", tile.Host)
	assert.Equal(t, map[string]any{"value": 3}, tile.Data)
}

func TestOperationalTilesCarryFreshMachineState(t *testing.T) {
	tiles := Operations(3, 2, 45*time.Second, 120, 40, "mbp", now)
	require.Len(t, tiles, 5)
	by := map[string]Tile{}
	for _, tile := range tiles {
		by[tile.Source] = tile
	}
	assert.Equal(t, 3, by["sessions.live"].Data["value"])
	assert.Equal(t, 2, by["queue.depth"].Data["value"])
	assert.Equal(t, []map[string]any{{"label": "mbp", "value": float64(45)}}, by["queue.wait"].Data["items"])
	assert.Equal(t, 120, by["lines.added"].Data["value"])
	assert.Equal(t, 40, by["lines.removed"].Data["value"])
}
