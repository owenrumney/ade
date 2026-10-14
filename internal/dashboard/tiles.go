package dashboard

import (
	"fmt"
	"sort"
	"time"

	"github.com/owenrumney/ade/internal/limits"
	"github.com/owenrumney/ade/internal/transcripts"
)

// Accounts names the subscription each login belongs to. Empty is a provider
// this machine does not report.
type Accounts struct {
	Claude string
	Codex  string
}

// topTools is how many of the day's tools the list tile carries.
const topTools = 5

// SparkDays is how much of the daily series a tick carries.
const SparkDays = 30

// Subscriptions is one tile per provider this machine has a label for.
func Subscriptions(state limits.State, who Accounts, host string) []Tile {
	var out []Tile
	for _, each := range []struct {
		label  string
		limits *limits.Limits
		title  string
		order  int
	}{
		{who.Claude, state.Claude, "Claude", 10},
		{who.Codex, state.Codex, "Codex", 20},
	} {
		if each.label == "" || each.limits == nil || len(each.limits.Windows) == 0 {
			continue
		}
		out = append(out, Tile{
			// Qualified by provider: one account label for both is the
			// ordinary case, and a bare label makes them one tile.
			Source: each.limits.Provider + "." + each.label,
			Type:   TypeSubscription,
			Title:  each.title + " · " + each.label,
			Group:  "subscriptions",
			Order:  each.order,
			Reduce: Latest,
			Host:   host,
			// When ade read the number, not when it is being sent.
			At: each.limits.CheckedAt,
			Data: map[string]any{
				"provider": each.limits.Provider,
				"windows":  windows(each.limits.Windows),
			},
		})
	}
	return out
}

// Usage is what this machine's transcripts say. days is how much of the daily
// series to send; SparkDays is a tick's worth. See the design for the rest.
func Usage(stats transcripts.Stats, host string, now time.Time, days int) []Tile {
	today := day(stats.ByDay, now)
	todayKey := now.Format(time.DateOnly)
	if days < 1 {
		days = SparkDays
	}
	return []Tile{
		{
			Source: "value.today", Type: TypeMoney, Title: "API value today",
			Group: "today", Order: 10, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{
				"value":  today.Cost,
				"unit":   "USD",
				"detail": notional(today.Unpriced),
			},
		},
		{
			Source: "value.by_model", Type: TypeList, Title: "API value today, by model",
			Group: "breakdown", Order: 10, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"items": byModel(stats.ByDayModel[now.Format(time.DateOnly)])},
		},
		{
			Source: "value.by_machine", Type: TypeList, Title: "API value today, by machine",
			Group: "breakdown", Order: 20, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"items": []map[string]any{{"label": host, "value": today.Cost}}},
		},
		{
			// tools.today is summed across hosts before export, so the
			// machine is lost. A list labelled by host keeps it, which is the
			// only shape an account can be joined onto.
			Source: "tools.by_machine", Type: TypeList, Title: "Tool calls, by machine",
			Group: "breakdown", Order: 25, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"items": []map[string]any{{"label": host, "value": stats.Tools}}},
		},
		{
			Source: "tokens.cacheread", Type: TypeCounter, Title: "Cached input tokens",
			Group: "breakdown", Order: 30, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"value": stats.Usage.CacheRead},
		},
		{
			Source: "tokens.total", Type: TypeCounter, Title: "Total tokens",
			Group: "breakdown", Order: 40, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"value": stats.Usage.Total()},
		},
		{
			Source: "turn.duration", Type: TypeList, Title: "Turn duration buckets",
			Group: "performance", Order: 10, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"items": durationHistogram(stats.TurnDurations, []float64{5, 30, 120, 600})},
		},
		{
			Source: "turns.failed", Type: TypeCounter, Title: "Failed turns, all time",
			Group: "performance", Order: 20, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"value": stats.Failed},
		},
		{
			Source: "turns.retried", Type: TypeCounter, Title: "Retried turns, all time",
			Group: "performance", Order: 30, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"value": stats.Retries},
		},
		{
			Source: "turns.completed", Type: TypeCounter, Title: "Completed turns today",
			Group: "performance", Order: 40, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"value": countGroups(stats.ByDayModelTurns[todayKey])},
		},
		{
			Source: "turns.by_model", Type: TypeList, Title: "Completed turns today, by model",
			Group: "performance", Order: 50, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"items": byCount(stats.ByDayModelTurns[todayKey])},
		},
		{
			Source: "context.headroom", Type: TypeList, Title: "Context headroom by session",
			Group: "performance", Order: 60, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"items": contextHeadroom(stats.Contexts, host)},
		},
		{
			Source: "tools.calls", Type: TypeList, Title: "Tool calls, all time",
			Group: "performance", Order: 70, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"items": byCount(stats.ByTool)},
		},
		{
			Source: "tools.failures", Type: TypeList, Title: "Tool failures, all time",
			Group: "performance", Order: 80, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"items": byCount(stats.ByToolFailure)},
		},
		{
			Source: "compactions", Type: TypeCounter, Title: "Compactions, all time",
			Group: "performance", Order: 90, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"value": stats.Compactions},
		},
		{
			Source: "session.duration", Type: TypeList, Title: "Session duration buckets",
			Group: "performance", Order: 100, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"items": durationHistogram(stats.SessionDurations, []float64{300, 1800, 7200, 28800})},
		},
		{
			Source: "human.interventions", Type: TypeCounter, Title: "Human follow-up turns, all time",
			Group: "performance", Order: 110, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"value": stats.Interventions},
		},
		{
			// Stats.Tools is every transcript on the machine, not today's:
			// nothing crosses the day groups with the tool ones.
			Source: "tools.today", Type: TypeCounter, Title: "Tool calls, all time",
			Group: "today", Order: 30, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"value": stats.Tools},
		},
		{
			Source: "tools.top", Type: TypeList, Title: "Busiest tools",
			Group: "today", Order: 40, Reduce: Sum, Host: host, At: now,
			Data: map[string]any{"items": byTool(stats.ByTool)},
		},
		{
			Source: "value.30d", Type: TypeSparkline, Title: "API value, by day",
			Group: "history", Order: 10, Reduce: Merge, Host: host, At: now,
			Data: map[string]any{"points": series(stats.ByDay, now, days), "unit": "USD"},
		},
	}
}

// Operations is the fresh machine state that does not come from transcripts.
func Operations(live, queued int, wait time.Duration, additions, deletions int, host string, now time.Time) []Tile {
	return []Tile{
		{Source: "sessions.live", Type: TypeCounter, Title: "Live sessions", Group: "breakdown", Order: 50, Reduce: Sum, Host: host, At: now, Data: map[string]any{"value": live}},
		{Source: "queue.depth", Type: TypeCounter, Title: "Queued turns", Group: "performance", Order: 120, Reduce: Sum, Host: host, At: now, Data: map[string]any{"value": queued}},
		{Source: "queue.wait", Type: TypeList, Title: "Oldest queue wait", Group: "performance", Order: 130, Reduce: Sum, Host: host, At: now, Data: map[string]any{"items": []map[string]any{{"label": host, "value": wait.Seconds()}}}},
		{Source: "lines.added", Type: TypeCounter, Title: "Branch lines added", Group: "output", Order: 10, Reduce: Sum, Host: host, At: now, Data: map[string]any{"value": additions}},
		{Source: "lines.removed", Type: TypeCounter, Title: "Branch lines removed", Group: "output", Order: 20, Reduce: Sum, Host: host, At: now, Data: map[string]any{"value": deletions}},
	}
}

// LiveSessions is this machine's current running-agent count.
func LiveSessions(count int, host string, now time.Time) Tile {
	return Tile{
		Source: "sessions.live", Type: TypeCounter, Title: "Live sessions",
		Group: "breakdown", Order: 50, Reduce: Sum, Host: host, At: now,
		Data: map[string]any{"value": count},
	}
}

// day is a date's row, or an empty one: nothing ran is zero, not silence.
func day(byDay []transcripts.Group, now time.Time) transcripts.Group {
	want := now.Format(time.DateOnly)
	for _, group := range byDay {
		if group.Key == want {
			return group
		}
	}
	return transcripts.Group{Key: want}
}

// byTool is the busiest tools, most-used first. addCount totals a tool's
// calls into Sessions, so that and not Requests is the number.
func byTool(groups []transcripts.Group) []map[string]any {
	sorted := append([]transcripts.Group(nil), groups...)
	sort.SliceStable(sorted, func(a, b int) bool {
		return sorted[a].Sessions > sorted[b].Sessions
	})
	out := make([]map[string]any, 0, topTools)
	for _, group := range sorted[:min(topTools, len(sorted))] {
		out = append(out, map[string]any{"label": group.Key, "value": group.Sessions})
	}
	return out
}

func byModel(groups []transcripts.Group) []map[string]any {
	out := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		out = append(out, map[string]any{"label": group.Key, "value": group.Cost})
	}
	return out
}

func byCount(groups []transcripts.Group) []map[string]any {
	out := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		out = append(out, map[string]any{"label": group.Key, "value": group.Sessions})
	}
	return out
}

func countGroups(groups []transcripts.Group) int {
	total := 0
	for _, group := range groups {
		total += group.Sessions
	}
	return total
}

func durationHistogram(values, bounds []float64) []map[string]any {
	out := make([]map[string]any, 0, len(bounds)+1)
	for _, bound := range bounds {
		count := 0
		for _, value := range values {
			if value <= bound {
				count++
			}
		}
		out = append(out, map[string]any{"label": fmt.Sprintf("le_%g", bound), "value": count})
	}
	out = append(out, map[string]any{"label": "le_inf", "value": len(values)})
	return out
}

func contextHeadroom(contexts []transcripts.ContextStat, host string) []map[string]any {
	sorted := append([]transcripts.ContextStat(nil), contexts...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Headroom < sorted[j].Headroom })
	if len(sorted) > 10 {
		sorted = sorted[:10]
	}
	out := make([]map[string]any, 0, len(sorted))
	for _, context := range sorted {
		out = append(out, map[string]any{"label": host + "/" + context.ID, "value": context.Headroom})
	}
	return out
}

// windows is the reading as the screen reads it: snake_case, because
// limits.Window marshals resetsAt and the renderer wants resets_at.
func windows(from []limits.Window) []map[string]any {
	out := make([]map[string]any, 0, len(from))
	for _, w := range from {
		one := map[string]any{"name": w.Name, "percent": w.Percent}
		if !w.ResetsAt.IsZero() {
			one["resets_at"] = w.ResetsAt.Format(time.RFC3339)
		}
		out = append(out, one)
	}
	return out
}

// notional is the line under the number: always what the number is, and what
// it could not price, so a gap reads as a gap rather than as zero.
func notional(unpriced int) string {
	const what = "rate-card value, not billed"
	if unpriced == 0 {
		return what
	}
	return fmt.Sprintf("%s · %d unpriced", what, unpriced)
}

// series is the last n days, oldest first, with the quiet days in it: the
// server adds the machines bucket by bucket, so the buckets have to line up.
func series(byDay []transcripts.Group, now time.Time, n int) []map[string]any {
	have := map[string]transcripts.Group{}
	for _, group := range byDay {
		have[group.Key] = group
	}
	out := make([]map[string]any, 0, n)
	for i := n - 1; i >= 0; i-- {
		key := now.AddDate(0, 0, -i).Format(time.DateOnly)
		out = append(out, map[string]any{"key": key, "value": have[key].Cost})
	}
	return out
}
